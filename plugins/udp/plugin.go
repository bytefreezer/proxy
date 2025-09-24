package udp

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/mitchellh/mapstructure"
	"github.com/n0needt0/bytefreezer-proxy/plugins"
	"github.com/n0needt0/go-goodies/log"
)

// Plugin implements the UDP input plugin
type Plugin struct {
	config  Config
	conn    *net.UDPConn
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	health  plugins.PluginHealth
	mu      sync.RWMutex
	output  chan<- *plugins.DataMessage
	metrics PluginMetrics
}

// Config represents UDP plugin configuration
type Config struct {
	Host              string `mapstructure:"host"`
	Port              int    `mapstructure:"port"`
	TenantID          string `mapstructure:"tenant_id"`
	DatasetID         string `mapstructure:"dataset_id"`
	BearerToken       string `mapstructure:"bearer_token,omitempty"`
	Protocol          string `mapstructure:"protocol,omitempty"`    // "udp", "syslog"
	SyslogMode        string `mapstructure:"syslog_mode,omitempty"` // "rfc3164", "rfc5424"
	DataHint          string `mapstructure:"data_hint,omitempty"` // Data format hint for downstream processing (defaults to "raw")
	ReadBufferSize    int    `mapstructure:"read_buffer_size,omitempty"`
	WorkerCount       int    `mapstructure:"worker_count,omitempty"`
	ChannelBufferSize int    `mapstructure:"channel_buffer_size,omitempty"`
}

// PluginMetrics tracks UDP plugin metrics
type PluginMetrics struct {
	PacketsReceived uint64
	BytesReceived   uint64
	PacketsDropped  uint64
	LastPacketTime  time.Time
	StartTime       time.Time
}

// NewPlugin creates a new UDP plugin instance
func NewPlugin() plugins.InputPlugin {
	return &Plugin{
		health: plugins.PluginHealth{
			Status:      plugins.HealthStatusStopped,
			Message:     "Plugin created but not started",
			LastUpdated: time.Now(),
		},
		metrics: PluginMetrics{
			StartTime: time.Now(),
		},
	}
}

// Name returns the plugin name
func (p *Plugin) Name() string {
	return "udp"
}

// Configure initializes the plugin with configuration
func (p *Plugin) Configure(config map[string]interface{}) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Decode configuration
	if err := mapstructure.Decode(config, &p.config); err != nil {
		return fmt.Errorf("failed to decode UDP plugin config: %w", err)
	}

	// Set defaults
	if p.config.Host == "" {
		p.config.Host = "0.0.0.0"
	}
	if p.config.Port == 0 {
		return fmt.Errorf("port is required for UDP plugin")
	}
	if p.config.TenantID == "" {
		return fmt.Errorf("tenant_id is required for UDP plugin")
	}
	if p.config.DatasetID == "" {
		return fmt.Errorf("dataset_id is required for UDP plugin")
	}
	if p.config.Protocol == "" {
		p.config.Protocol = "udp"
	}
	if p.config.ReadBufferSize == 0 {
		p.config.ReadBufferSize = 65536 // 64KB default
	}
	if p.config.WorkerCount == 0 {
		p.config.WorkerCount = 4
	}
	if p.config.ChannelBufferSize == 0 {
		p.config.ChannelBufferSize = 10000
	}

	// Validate protocol
	validProtocols := map[string]bool{
		"udp": true, "syslog": true,
	}
	if !validProtocols[p.config.Protocol] {
		return fmt.Errorf("invalid protocol %s (supported: udp, syslog)", p.config.Protocol)
	}

	p.updateHealth(plugins.HealthStatusStopped, "Plugin configured successfully", "")
	log.Infof("UDP plugin configured: %s:%d -> %s/%s (%s)",
		p.config.Host, p.config.Port, p.config.TenantID, p.config.DatasetID, p.config.Protocol)

	return nil
}

// Start begins consuming UDP data
func (p *Plugin) Start(ctx context.Context, output chan<- *plugins.DataMessage) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.ctx != nil {
		return fmt.Errorf("plugin is already started")
	}

	// Set default data hint if not specified
	if p.config.DataHint == "" {
		p.config.DataHint = "raw"
	}

	p.ctx, p.cancel = context.WithCancel(ctx)
	p.output = output
	p.metrics.StartTime = time.Now()

	// Create UDP listener
	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", p.config.Host, p.config.Port))
	if err != nil {
		p.updateHealth(plugins.HealthStatusUnhealthy, "Failed to resolve UDP address", err.Error())
		return fmt.Errorf("failed to resolve UDP address: %w", err)
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		p.updateHealth(plugins.HealthStatusUnhealthy, "Failed to start UDP listener", err.Error())
		return fmt.Errorf("failed to listen on UDP: %w", err)
	}

	p.conn = conn

	// Set read buffer size
	if err := conn.SetReadBuffer(p.config.ReadBufferSize); err != nil {
		log.Warnf("Failed to set UDP read buffer size to %d: %v", p.config.ReadBufferSize, err)
	}

	p.updateHealth(plugins.HealthStatusStarting, "Starting UDP listener workers", "")

	// Start worker goroutines
	messageChannel := make(chan *udpMessage, p.config.ChannelBufferSize)

	// Start packet receiver
	p.wg.Add(1)
	go p.packetReceiver(messageChannel)

	// Start message processors
	for i := 0; i < p.config.WorkerCount; i++ {
		p.wg.Add(1)
		go p.messageProcessor(messageChannel)
	}

	p.updateHealth(plugins.HealthStatusHealthy, fmt.Sprintf("UDP listener active on %s:%d", p.config.Host, p.config.Port), "")
	log.Infof("UDP plugin started on %s:%d with %d workers", p.config.Host, p.config.Port, p.config.WorkerCount)

	return nil
}

// Stop gracefully shuts down the UDP plugin
func (p *Plugin) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cancel == nil {
		return nil // Already stopped
	}

	p.updateHealth(plugins.HealthStatusStopping, "Shutting down UDP listener", "")

	// Cancel context
	p.cancel()

	// Close UDP connection
	if p.conn != nil {
		p.conn.Close()
	}

	// Wait for all goroutines to finish
	p.wg.Wait()

	p.ctx = nil
	p.cancel = nil
	p.conn = nil

	p.updateHealth(plugins.HealthStatusStopped, "UDP listener stopped", "")
	log.Infof("UDP plugin stopped")

	return nil
}

// Health returns the current plugin health status
func (p *Plugin) Health() plugins.PluginHealth {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.health
}

// updateHealth updates the plugin health status
func (p *Plugin) updateHealth(status plugins.HealthStatus, message, lastError string) {
	p.health = plugins.PluginHealth{
		Status:      status,
		Message:     message,
		LastError:   lastError,
		LastUpdated: time.Now(),
	}
}

// udpMessage represents a received UDP packet with metadata
type udpMessage struct {
	Data []byte
	From *net.UDPAddr
}

// packetReceiver reads UDP packets and forwards them to processing channel
func (p *Plugin) packetReceiver(messageChannel chan<- *udpMessage) {
	defer p.wg.Done()
	defer close(messageChannel)

	buffer := make([]byte, 65536) // 64KB buffer for UDP packets

	for {
		select {
		case <-p.ctx.Done():
			return
		default:
			// Set read deadline to prevent blocking indefinitely
			p.conn.SetReadDeadline(time.Now().Add(1 * time.Second))

			n, addr, err := p.conn.ReadFromUDP(buffer)
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					continue // Timeout is expected, continue listening
				}
				if strings.Contains(err.Error(), "use of closed network connection") {
					return // Connection closed, normal shutdown
				}
				log.Errorf("UDP read error: %v", err)
				p.metrics.PacketsDropped++
				continue
			}

			// Update metrics
			p.metrics.PacketsReceived++
			if n > 0 {
				p.metrics.BytesReceived += uint64(n) // #nosec G115 - n is always positive from ReadFromUDP
			}
			p.metrics.LastPacketTime = time.Now()

			// Copy data to avoid buffer reuse issues
			data := make([]byte, n)
			copy(data, buffer[:n])

			// Send to processing channel
			select {
			case messageChannel <- &udpMessage{Data: data, From: addr}:
			case <-p.ctx.Done():
				return
			default:
				// Channel is full, drop packet
				p.metrics.PacketsDropped++
				log.Warnf("UDP message channel full, dropping packet from %s", addr)
			}
		}
	}
}

// messageProcessor processes UDP messages and sends them to output
func (p *Plugin) messageProcessor(messageChannel <-chan *udpMessage) {
	defer p.wg.Done()

	for {
		select {
		case <-p.ctx.Done():
			return
		case msg, ok := <-messageChannel:
			if !ok {
				return // Channel closed
			}
			p.processMessage(msg)
		}
	}
}

// processMessage processes a single UDP message
func (p *Plugin) processMessage(msg *udpMessage) {
	// Minimal cleanup - only remove null bytes
	payload := bytes.Trim(msg.Data, "\x00")
	if len(payload) == 0 {
		return
	}

	// Create data message for output
	dataMsg := &plugins.DataMessage{
		Data:          payload,
		TenantID:      p.config.TenantID,
		DatasetID:     p.config.DatasetID,
		DataHint:      p.config.DataHint,
		Timestamp:     time.Now(),
		SourceIP:      msg.From.IP.String(),
		Metadata: map[string]string{
			"source_port": fmt.Sprintf("%d", msg.From.Port),
			"protocol":    p.config.Protocol,
			"plugin":      "udp",
		},
	}

	// Add bearer token if configured
	if p.config.BearerToken != "" {
		dataMsg.Metadata["bearer_token"] = p.config.BearerToken
	}

	// Add syslog mode if applicable
	if p.config.Protocol == "syslog" && p.config.SyslogMode != "" {
		dataMsg.Metadata["syslog_mode"] = p.config.SyslogMode
	}

	// Send to output channel
	select {
	case p.output <- dataMsg:
	case <-p.ctx.Done():
		return
	default:
		log.Warnf("Output channel full, dropping message from %s", msg.From)
	}
}

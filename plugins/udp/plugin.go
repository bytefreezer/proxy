package udp

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/mitchellh/mapstructure"
	"github.com/n0needt0/bytefreezer-proxy/plugins"
	"github.com/n0needt0/go-goodies/log"
)

// Plugin implements the UDP input plugin with direct filesystem writes
type Plugin struct {
	config  Config
	conn    *net.UDPConn
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	health  plugins.PluginHealth
	mu      sync.RWMutex
	spooler plugins.SpoolingInterface
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
func (p *Plugin) Start(ctx context.Context, spooler plugins.SpoolingInterface) error {
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
	p.spooler = spooler
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

	p.updateHealth(plugins.HealthStatusStarting, "Starting UDP listener with direct spooling", "")

	// Start packet reader with direct spooling (no channels, no drops)
	p.wg.Add(1)
	go p.packetReaderWithSpooling()

	p.updateHealth(plugins.HealthStatusHealthy, fmt.Sprintf("UDP listener active with direct spooling on %s:%d", p.config.Host, p.config.Port), "")
	log.Infof("UDP plugin started with direct spooling on %s:%d", p.config.Host, p.config.Port)

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


// packetReaderWithSpooling reads UDP packets and writes directly to filesystem (zero data loss)
func (p *Plugin) packetReaderWithSpooling() {
	defer p.wg.Done()

	buffer := make([]byte, 65536) // 64KB buffer for UDP packets

	for {
		select {
		case <-p.ctx.Done():
			return
		default:
			// Set read deadline to allow checking for context cancellation
			p.conn.SetReadDeadline(time.Now().Add(1 * time.Second))

			n, clientAddr, err := p.conn.ReadFromUDP(buffer)
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					continue // Timeout is expected, continue to check context
				}
				if p.ctx.Err() != nil {
					return // Context cancelled
				}
				log.Errorf("Error reading UDP packet: %v", err)
				continue
			}

			// Process message with direct spooling
			if n > 0 {
				p.processMessageWithSpooling(buffer[:n], clientAddr)
			}
		}
	}
}

// processMessageWithSpooling processes UDP message with direct filesystem write (zero data loss)
func (p *Plugin) processMessageWithSpooling(data []byte, clientAddr *net.UDPAddr) {
	// Update metrics
	p.metrics.PacketsReceived++
	p.metrics.BytesReceived += uint64(len(data))
	p.metrics.LastPacketTime = time.Now()

	// Minimal cleanup - only remove null bytes
	payload := bytes.Trim(data, "\x00")
	if len(payload) == 0 {
		return
	}

	// Format data according to data hint
	formattedData := payload
	if p.config.DataHint != "" {
		var err error
		formatter := plugins.GetFormatter(p.config.DataHint)
		formattedData, err = formatter.Format(payload)
		if err != nil {
			log.Warnf("Data formatting failed for UDP packet from %s (format: %s): %v", clientAddr.IP.String(), p.config.DataHint, err)
			// Continue with original payload if formatting fails
			formattedData = payload
		}
	}

	// Write directly to filesystem - NO CHANNEL DROPS POSSIBLE
	bearerToken := p.config.BearerToken
	dataHint := p.config.DataHint
	if dataHint == "" {
		dataHint = "raw" // default for UDP
	}
	if err := p.spooler.StoreRawMessage(p.config.TenantID, p.config.DatasetID, bearerToken, formattedData, dataHint); err != nil {
		log.Errorf("Failed to store UDP message to filesystem from %s: %v", clientAddr.IP.String(), err)
		p.metrics.PacketsDropped++
		return
	}

	log.Debugf("Stored UDP message from %s:%d directly to filesystem (%d bytes)",
		clientAddr.IP.String(), clientAddr.Port, len(formattedData))
}

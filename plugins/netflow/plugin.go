// Licensed under Elastic License 2.0
// See LICENSE.txt for details

package netflow

import (
	"bytes"
	"context"
	"fmt"
	"github.com/bytedance/sonic"
	"net"
	"sync"
	"time"

	"github.com/bytefreezer/goodies/log"
	"github.com/bytefreezer/proxy/plugins"
	"github.com/mitchellh/mapstructure"
	netflowdec "github.com/netsampler/goflow2/v2/decoders/netflow"
)

// Plugin implements the NetFlow input plugin with direct filesystem writes
type Plugin struct {
	config    Config
	conn      *net.UDPConn
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	health    plugins.PluginHealth
	mu        sync.RWMutex
	spooler   plugins.SpoolingInterface
	metrics   PluginMetrics
	templates netflowdec.NetFlowTemplateSystem
}

// Config represents NetFlow plugin configuration
type Config struct {
	Host           string `mapstructure:"host"`
	Port           int    `mapstructure:"port"`
	TenantID       string `mapstructure:"tenant_id"`
	DatasetID      string `mapstructure:"dataset_id"`
	BearerToken    string `mapstructure:"bearer_token,omitempty"`
	Protocol       string `mapstructure:"protocol,omitempty"`  // Always "netflow" for this plugin
	DataHint       string `mapstructure:"data_hint,omitempty"` // Always "ndjson" for flow data
	ReadBufferSize int    `mapstructure:"read_buffer_size,omitempty"`
	WorkerCount    int    `mapstructure:"worker_count,omitempty"` // Number of processing workers
}

// PluginMetrics tracks NetFlow plugin metrics
type PluginMetrics struct {
	PacketsReceived uint64
	BytesReceived   uint64
	FlowsDecoded    uint64
	PacketsDropped  uint64
	LastPacketTime  time.Time
	StartTime       time.Time
}

// NewPlugin creates a new NetFlow plugin instance
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
	return "netflow"
}

// Configure initializes the plugin with configuration
func (p *Plugin) Configure(config map[string]interface{}) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Decode configuration
	if err := mapstructure.Decode(config, &p.config); err != nil {
		return fmt.Errorf("failed to decode NetFlow plugin config: %w", err)
	}

	// Set defaults
	if p.config.Host == "" {
		p.config.Host = "0.0.0.0"
	}
	if p.config.Port == 0 {
		return fmt.Errorf("port is required for NetFlow plugin")
	}
	if p.config.TenantID == "" {
		return fmt.Errorf("tenant_id is required for NetFlow plugin")
	}
	if p.config.DatasetID == "" {
		return fmt.Errorf("dataset_id is required for NetFlow plugin")
	}
	if p.config.Protocol == "" {
		p.config.Protocol = "netflow"
	}
	if p.config.DataHint == "" {
		p.config.DataHint = "ndjson" // NetFlow data is converted to NDJSON
	}
	if p.config.ReadBufferSize == 0 {
		p.config.ReadBufferSize = 8388608 // 8MB default for burst handling
	}
	if p.config.WorkerCount == 0 {
		p.config.WorkerCount = 4 // Default 4 workers
	}

	// Initialize NetFlow template system
	p.templates = netflowdec.CreateTemplateSystem()

	p.updateHealth(plugins.HealthStatusStopped, "Plugin configured successfully", "")
	log.Infof("NetFlow plugin configured: %s:%d -> %s/%s (data_hint: %s)",
		p.config.Host, p.config.Port, p.config.TenantID, p.config.DatasetID, p.config.DataHint)

	return nil
}

// Start begins consuming NetFlow data
func (p *Plugin) Start(ctx context.Context, spooler plugins.SpoolingInterface) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.ctx != nil {
		return fmt.Errorf("plugin is already started")
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

	// Set read buffer size with verification and report if limited
	bufferResult := plugins.SetUDPReadBufferWithCheck(conn, p.config.ReadBufferSize)
	if bufferResult.Limited && bufferResult.Warning != "" {
		spooler.ReportWarning(p.config.TenantID, p.config.DatasetID, "udp_buffer_limited", bufferResult.Warning)
	}

	p.updateHealth(plugins.HealthStatusStarting, "Starting NetFlow listener with direct spooling", "")

	// Start packet reader with direct spooling
	p.wg.Add(1)
	go p.packetReaderWithSpooling()

	p.updateHealth(plugins.HealthStatusHealthy, fmt.Sprintf("NetFlow listener active with direct spooling on %s:%d", p.config.Host, p.config.Port), "")
	log.Infof("NetFlow plugin started with direct spooling on %s:%d", p.config.Host, p.config.Port)

	return nil
}

// Stop gracefully shuts down the NetFlow plugin
func (p *Plugin) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cancel == nil {
		return nil // Already stopped
	}

	p.updateHealth(plugins.HealthStatusStopping, "Shutting down NetFlow listener", "")

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

	p.updateHealth(plugins.HealthStatusStopped, "NetFlow listener stopped", "")
	log.Infof("NetFlow plugin stopped")

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

// packetReaderWithSpooling reads NetFlow packets and writes directly to filesystem (zero data loss)
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
				log.Errorf("Error reading NetFlow packet: %v", err)
				continue
			}

			// Process message with direct spooling
			if n > 0 {
				p.processMessageWithSpooling(buffer[:n], clientAddr)
			}
		}
	}
}

// processMessageWithSpooling processes NetFlow message with direct filesystem write (zero data loss)
func (p *Plugin) processMessageWithSpooling(data []byte, clientAddr *net.UDPAddr) {
	// Update metrics
	p.metrics.PacketsReceived++
	p.metrics.BytesReceived += uint64(len(data))
	p.metrics.LastPacketTime = time.Now()

	// Decode NetFlow packet using goflow2
	payload := bytes.NewBuffer(data)
	packet := &netflowdec.NFv9Packet{}

	err := netflowdec.DecodeMessageVersion(payload, p.templates, packet, nil)
	if err != nil {
		log.Warnf("Failed to decode NetFlow packet from %s: %v", clientAddr.IP.String(), err)
		p.metrics.PacketsDropped++
		return
	}

	p.metrics.FlowsDecoded++

	// Convert decoded packet to JSON
	jsonData, err := sonic.Marshal(packet)
	if err != nil {
		log.Warnf("Failed to marshal NetFlow message to JSON from %s: %v", clientAddr.IP.String(), err)
		p.metrics.PacketsDropped++
		return
	}

	// Add newline for NDJSON format
	ndjsonData := append(jsonData, '\n')

	// Write directly to filesystem - NO CHANNEL DROPS POSSIBLE
	bearerToken := p.config.BearerToken
	dataHint := p.config.DataHint
	if err := p.spooler.StoreRawMessage(p.config.TenantID, p.config.DatasetID, bearerToken, ndjsonData, dataHint); err != nil {
		log.Errorf("Failed to store NetFlow message to filesystem from %s: %v", clientAddr.IP.String(), err)
		p.metrics.PacketsDropped++
		return
	}

	log.Debugf("Stored NetFlow message from %s:%d as NDJSON directly to filesystem (%d bytes)",
		clientAddr.IP.String(), clientAddr.Port, len(ndjsonData))
}

// Schema returns the NetFlow plugin configuration schema
func (p *Plugin) Schema() plugins.PluginSchema {
	return plugins.PluginSchema{
		Name:        "netflow",
		DisplayName: "NetFlow v5/v9",
		Description: "NetFlow network flow collector. Decodes NetFlow v5 and v9 packets and outputs structured NDJSON with flow metadata.",
		Category:    "UDP-based (Network Flow)",
		Transport:   "UDP",
		DefaultPort: 2055,
		Fields: []plugins.PluginFieldSchema{
			{
				Name:        "host",
				Type:        "string",
				Required:    false,
				Default:     "0.0.0.0",
				Description: "Host address to bind to",
				Placeholder: "0.0.0.0",
				Group:       "Network",
			},
			{
				Name:        "port",
				Type:        "int",
				Required:    false,
				Default:     2055,
				Description: "UDP port to listen on (standard NetFlow port: 2055)",
				Validation:  "1-65535",
				Placeholder: "2055",
				Group:       "Network",
			},
			{
				Name:        "read_buffer_size",
				Type:        "int",
				Required:    false,
				Default:     65536,
				Description: "UDP read buffer size in bytes (64KB default)",
				Validation:  "min:1024,max:1048576",
				Placeholder: "65536",
				Group:       "Performance",
			},
			{
				Name:        "worker_count",
				Type:        "int",
				Required:    false,
				Default:     4,
				Description: "Number of processing workers",
				Validation:  "min:1,max:32",
				Placeholder: "4",
				Group:       "Performance",
			},
		},
	}
}

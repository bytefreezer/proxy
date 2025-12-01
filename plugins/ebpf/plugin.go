package ebpf

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
)

// Plugin implements the eBPF input plugin with direct filesystem writes
// Receives NDJSON data from eBPF sources and passes through as-is
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

// Config represents eBPF plugin configuration
type Config struct {
	Host           string `mapstructure:"host"`
	Port           int    `mapstructure:"port"`
	TenantID       string `mapstructure:"tenant_id"`
	DatasetID      string `mapstructure:"dataset_id"`
	BearerToken    string `mapstructure:"bearer_token,omitempty"`
	ReadBufferSize int    `mapstructure:"read_buffer_size,omitempty"`
	WorkerCount    int    `mapstructure:"worker_count,omitempty"`
}

// PluginMetrics tracks eBPF plugin metrics
type PluginMetrics struct {
	MessagesReceived uint64
	BytesReceived    uint64
	MessagesDropped  uint64
	LastMessageTime  time.Time
	StartTime        time.Time
}

// NewPlugin creates a new eBPF plugin instance
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
	return "ebpf"
}

// Configure initializes the plugin with configuration
func (p *Plugin) Configure(config map[string]interface{}) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Decode configuration
	if err := mapstructure.Decode(config, &p.config); err != nil {
		return fmt.Errorf("failed to decode eBPF plugin config: %w", err)
	}

	// Set defaults
	if p.config.Host == "" {
		p.config.Host = "0.0.0.0"
	}
	if p.config.Port == 0 {
		return fmt.Errorf("port is required for eBPF plugin")
	}
	if p.config.TenantID == "" {
		return fmt.Errorf("tenant_id is required for eBPF plugin")
	}
	if p.config.DatasetID == "" {
		return fmt.Errorf("dataset_id is required for eBPF plugin")
	}
	if p.config.ReadBufferSize == 0 {
		p.config.ReadBufferSize = 65536 // 64KB default
	}
	if p.config.WorkerCount == 0 {
		p.config.WorkerCount = 4 // Default 4 workers
	}

	p.updateHealth(plugins.HealthStatusStopped, "Plugin configured successfully", "")
	log.Infof("eBPF plugin configured: %s:%d -> %s/%s",
		p.config.Host, p.config.Port, p.config.TenantID, p.config.DatasetID)

	return nil
}

// Start begins consuming eBPF data
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

	// Set read buffer size
	if err := conn.SetReadBuffer(p.config.ReadBufferSize); err != nil {
		log.Warnf("Failed to set UDP read buffer size to %d: %v", p.config.ReadBufferSize, err)
	}

	p.updateHealth(plugins.HealthStatusStarting, "Starting eBPF listener with direct spooling", "")

	// Start packet reader with direct spooling
	p.wg.Add(1)
	go p.packetReaderWithSpooling()

	p.updateHealth(plugins.HealthStatusHealthy, fmt.Sprintf("eBPF listener active on %s:%d", p.config.Host, p.config.Port), "")
	log.Infof("eBPF plugin started on %s:%d", p.config.Host, p.config.Port)

	return nil
}

// Stop gracefully shuts down the eBPF plugin
func (p *Plugin) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cancel == nil {
		return nil // Already stopped
	}

	p.updateHealth(plugins.HealthStatusStopping, "Shutting down eBPF listener", "")

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

	p.updateHealth(plugins.HealthStatusStopped, "eBPF listener stopped", "")
	log.Infof("eBPF plugin stopped")

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

// packetReaderWithSpooling reads UDP packets and writes directly to filesystem
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
					continue // Timeout is expected
				}
				if p.ctx.Err() != nil {
					return // Context cancelled
				}
				log.Errorf("Error reading eBPF packet: %v", err)
				continue
			}

			// Process message with direct spooling
			if n > 0 {
				p.processMessageWithSpooling(buffer[:n], clientAddr)
			}
		}
	}
}

// processMessageWithSpooling processes eBPF message and flattens to NDJSON
func (p *Plugin) processMessageWithSpooling(data []byte, clientAddr *net.UDPAddr) {
	// Update metrics
	p.metrics.MessagesReceived++
	p.metrics.BytesReceived += uint64(len(data))
	p.metrics.LastMessageTime = time.Now()

	// Trim whitespace and null bytes
	payload := bytes.TrimSpace(bytes.Trim(data, "\x00"))
	if len(payload) == 0 {
		return
	}

	// Parse JSON (handles both compact and pretty-printed formats)
	var message map[string]interface{}
	if err := sonic.Unmarshal(payload, &message); err != nil {
		log.Warnf("Failed to parse eBPF JSON from %s: %v", clientAddr.IP.String(), err)
		p.metrics.MessagesDropped++
		return
	}

	// Re-marshal to compact JSON (flattens pretty-printed JSON)
	jsonData, err := sonic.Marshal(message)
	if err != nil {
		log.Warnf("Failed to marshal eBPF message to JSON from %s: %v", clientAddr.IP.String(), err)
		p.metrics.MessagesDropped++
		return
	}

	// Add newline for NDJSON format
	ndjsonData := append(jsonData, '\n')

	// Store to filesystem
	bearerToken := p.config.BearerToken
	if err := p.spooler.StoreRawMessage(p.config.TenantID, p.config.DatasetID, bearerToken, ndjsonData, "ndjson"); err != nil {
		log.Errorf("Failed to store eBPF message to filesystem from %s: %v", clientAddr.IP.String(), err)
		p.metrics.MessagesDropped++
		return
	}

	log.Debugf("Stored eBPF NDJSON message from %s:%d (%d bytes flattened to %d bytes)",
		clientAddr.IP.String(), clientAddr.Port, len(payload), len(ndjsonData))
}

// Schema returns the eBPF plugin configuration schema
func (p *Plugin) Schema() plugins.PluginSchema {
	return plugins.PluginSchema{
		Name:        "ebpf",
		DisplayName: "eBPF",
		Description: "eBPF data receiver for kernel-level telemetry. Receives pre-formatted NDJSON data from eBPF sources and passes through as-is.",
		Category:    "Kernel Telemetry",
		Transport:   "UDP",
		DefaultPort: 2056,
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
				Required:    true,
				Default:     2056,
				Description: "UDP port to listen on",
				Validation:  "1-65535",
				Placeholder: "2056",
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
				Description: "Number of worker goroutines for processing",
				Validation:  "min:1,max:32",
				Placeholder: "4",
				Group:       "Performance",
			},
		},
	}
}

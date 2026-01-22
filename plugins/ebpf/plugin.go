// Licensed under Elastic License 2.0
// See LICENSE.txt for details

package ebpf

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bytedance/sonic"
	"github.com/bytefreezer/goodies/log"
	"github.com/bytefreezer/proxy/plugins"
	"github.com/mitchellh/mapstructure"
)

// Plugin implements the eBPF input plugin with direct filesystem writes
// Receives NDJSON data from eBPF sources and passes through as-is
type Plugin struct {
	config   Config
	conn     *net.UDPConn
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	health   plugins.PluginHealth
	mu       sync.RWMutex
	spooler  plugins.SpoolingInterface
	metrics  PluginMetrics
	workChan chan workItem // Channel for worker pool
}

// workItem represents a UDP packet to be processed
type workItem struct {
	data       []byte
	clientAddr *net.UDPAddr
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
		p.config.ReadBufferSize = 8388608 // 8MB default for burst handling
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

	// Set read buffer size with verification and report if limited
	bufferResult := plugins.SetUDPReadBufferWithCheck(conn, p.config.ReadBufferSize)
	if bufferResult.Limited && bufferResult.Warning != "" {
		metadata := map[string]interface{}{
			"requested_size": bufferResult.RequestedSize,
			"actual_size":    bufferResult.ActualSize,
			"sysctl_cmd":     bufferResult.SysctlCmd,
		}
		spooler.ReportWarningWithMetadata(p.config.TenantID, p.config.DatasetID, "udp_buffer_limited", bufferResult.Warning, metadata)
	} else {
		// Buffer is OK - resolve any previous warnings
		spooler.ResolveWarning(p.config.DatasetID, "udp_buffer_limited")
	}

	p.updateHealth(plugins.HealthStatusStarting, "Starting eBPF listener with direct spooling", "")

	// Create work channel with buffer to absorb bursts
	p.workChan = make(chan workItem, 10000)

	// Start worker pool
	for i := 0; i < p.config.WorkerCount; i++ {
		p.wg.Add(1)
		go p.worker(i)
	}

	// Start packet reader that feeds workers
	p.wg.Add(1)
	go p.packetReader()

	p.updateHealth(plugins.HealthStatusHealthy, fmt.Sprintf("eBPF listener active on %s:%d with %d workers", p.config.Host, p.config.Port, p.config.WorkerCount), "")
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

	// Close UDP connection first to stop reader
	if p.conn != nil {
		p.conn.Close()
	}

	// Close work channel to signal workers to stop
	if p.workChan != nil {
		close(p.workChan)
	}

	// Wait for all goroutines to finish
	p.wg.Wait()

	p.ctx = nil
	p.cancel = nil
	p.conn = nil
	p.workChan = nil

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

// packetReader reads UDP packets and sends to worker pool
func (p *Plugin) packetReader() {
	defer p.wg.Done()

	buffer := make([]byte, 65536) // 64KB buffer for UDP packets

	for {
		// Set read deadline to allow checking for context cancellation
		p.conn.SetReadDeadline(time.Now().Add(1 * time.Second))

		n, clientAddr, err := p.conn.ReadFromUDP(buffer)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				// Check if we should stop
				select {
				case <-p.ctx.Done():
					return
				default:
					continue // Timeout is expected, keep reading
				}
			}
			if p.ctx.Err() != nil {
				return // Context cancelled
			}
			log.Errorf("Error reading eBPF packet: %v", err)
			continue
		}

		// Copy data and send to worker pool
		if n > 0 {
			// Must copy buffer as it will be reused
			dataCopy := make([]byte, n)
			copy(dataCopy, buffer[:n])

			select {
			case p.workChan <- workItem{data: dataCopy, clientAddr: clientAddr}:
				// Sent to worker
			default:
				// Channel full - drop packet and count it
				atomic.AddUint64(&p.metrics.MessagesDropped, 1)
				log.Warnf("Worker queue full, dropping eBPF packet from %s", clientAddr.IP.String())
			}
		}
	}
}

// worker processes packets from the work channel
func (p *Plugin) worker(id int) {
	defer p.wg.Done()

	for item := range p.workChan {
		p.processMessage(item.data, item.clientAddr)
	}
}

// processMessage processes eBPF message and writes to spooler
// Optimized: skips JSON parse/re-marshal if data is already compact
func (p *Plugin) processMessage(data []byte, clientAddr *net.UDPAddr) {
	// Update metrics using atomic operations (lock-free)
	atomic.AddUint64(&p.metrics.MessagesReceived, 1)
	atomic.AddUint64(&p.metrics.BytesReceived, uint64(len(data)))

	// Trim whitespace and null bytes
	payload := bytes.TrimSpace(bytes.Trim(data, "\x00"))
	if len(payload) == 0 {
		return
	}

	var ndjsonData []byte

	// Optimization: if payload has no internal newlines, it's already compact JSON
	// Just validate it's valid JSON and pass through without parse/re-marshal
	if !bytes.Contains(payload, []byte("\n")) {
		// Fast path: validate JSON structure without full parse
		if len(payload) > 0 && payload[0] == '{' && payload[len(payload)-1] == '}' {
			// Looks like compact JSON object, pass through
			ndjsonData = append(payload, '\n')
		} else {
			// Not a JSON object, try to parse
			var message map[string]interface{}
			if err := sonic.Unmarshal(payload, &message); err != nil {
				log.Warnf("Failed to parse eBPF JSON from %s: %v", clientAddr.IP.String(), err)
				atomic.AddUint64(&p.metrics.MessagesDropped, 1)
				return
			}
			jsonData, err := sonic.Marshal(message)
			if err != nil {
				log.Warnf("Failed to marshal eBPF message to JSON from %s: %v", clientAddr.IP.String(), err)
				atomic.AddUint64(&p.metrics.MessagesDropped, 1)
				return
			}
			ndjsonData = append(jsonData, '\n')
		}
	} else {
		// Pretty-printed JSON - strip whitespace using fast byte scan
		// eBPF JSON won't have literal newlines in string values, so this is safe
		ndjsonData = compactJSONFast(payload)
	}

	// Store to filesystem
	bearerToken := p.config.BearerToken
	if err := p.spooler.StoreRawMessage(p.config.TenantID, p.config.DatasetID, bearerToken, ndjsonData, "ndjson"); err != nil {
		log.Errorf("Failed to store eBPF message to filesystem from %s: %v", clientAddr.IP.String(), err)
		atomic.AddUint64(&p.metrics.MessagesDropped, 1)
		return
	}
}

// compactJSONFast strips whitespace from JSON without parsing
// This is faster than json.Compact() as it doesn't validate structure
// Safe for eBPF JSON which won't have literal newlines in string values
func compactJSONFast(src []byte) []byte {
	dst := make([]byte, 0, len(src))
	inString := false
	escape := false

	for _, b := range src {
		if escape {
			dst = append(dst, b)
			escape = false
			continue
		}
		if b == '\\' && inString {
			dst = append(dst, b)
			escape = true
			continue
		}
		if b == '"' {
			inString = !inString
		}
		if !inString && (b == ' ' || b == '\t' || b == '\n' || b == '\r') {
			continue
		}
		dst = append(dst, b)
	}
	return append(dst, '\n')
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
				Default:     8388608,
				Description: "UDP socket read buffer size in bytes (8MB default). Requires kernel sysctl: net.core.rmem_max=16777216",
				Validation:  "min:65536,max:16777216",
				Placeholder: "8388608",
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

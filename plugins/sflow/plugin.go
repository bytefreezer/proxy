package sflow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/mitchellh/mapstructure"
	"github.com/n0needt0/bytefreezer-proxy/plugins"
	"github.com/n0needt0/go-goodies/log"
	sflowdec "github.com/netsampler/goflow2/v2/decoders/sflow"
)

// Plugin implements the sFlow input plugin with direct filesystem writes
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

// Config represents sFlow plugin configuration
type Config struct {
	Host           string `mapstructure:"host"`
	Port           int    `mapstructure:"port"`
	TenantID       string `mapstructure:"tenant_id"`
	DatasetID      string `mapstructure:"dataset_id"`
	BearerToken    string `mapstructure:"bearer_token,omitempty"`
	Protocol       string `mapstructure:"protocol,omitempty"` // Always "sflow" for this plugin
	DataHint       string `mapstructure:"data_hint,omitempty"` // Always "ndjson" for flow data
	ReadBufferSize int    `mapstructure:"read_buffer_size,omitempty"`
	WorkerCount    int    `mapstructure:"worker_count,omitempty"` // Number of processing workers
}

// PluginMetrics tracks sFlow plugin metrics
type PluginMetrics struct {
	PacketsReceived uint64
	BytesReceived   uint64
	FlowsDecoded    uint64
	PacketsDropped  uint64
	LastPacketTime  time.Time
	StartTime       time.Time
}

// NewPlugin creates a new sFlow plugin instance
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
	return "sflow"
}

// Configure initializes the plugin with configuration
func (p *Plugin) Configure(config map[string]interface{}) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Decode configuration
	if err := mapstructure.Decode(config, &p.config); err != nil {
		return fmt.Errorf("failed to decode sFlow plugin config: %w", err)
	}

	// Set defaults
	if p.config.Host == "" {
		p.config.Host = "0.0.0.0"
	}
	if p.config.Port == 0 {
		return fmt.Errorf("port is required for sFlow plugin")
	}
	if p.config.TenantID == "" {
		return fmt.Errorf("tenant_id is required for sFlow plugin")
	}
	if p.config.DatasetID == "" {
		return fmt.Errorf("dataset_id is required for sFlow plugin")
	}
	if p.config.Protocol == "" {
		p.config.Protocol = "sflow"
	}
	if p.config.DataHint == "" {
		p.config.DataHint = "ndjson" // sFlow data is converted to NDJSON
	}
	if p.config.ReadBufferSize == 0 {
		p.config.ReadBufferSize = 65536 // 64KB default
	}
	if p.config.WorkerCount == 0 {
		p.config.WorkerCount = 4 // Default 4 workers
	}

	p.updateHealth(plugins.HealthStatusStopped, "Plugin configured successfully", "")
	log.Infof("sFlow plugin configured: %s:%d -> %s/%s (data_hint: %s)",
		p.config.Host, p.config.Port, p.config.TenantID, p.config.DatasetID, p.config.DataHint)

	return nil
}

// Start begins consuming sFlow data
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

	p.updateHealth(plugins.HealthStatusStarting, "Starting sFlow listener with direct spooling", "")

	// Start packet reader with direct spooling
	p.wg.Add(1)
	go p.packetReaderWithSpooling()

	p.updateHealth(plugins.HealthStatusHealthy, fmt.Sprintf("sFlow listener active with direct spooling on %s:%d", p.config.Host, p.config.Port), "")
	log.Infof("sFlow plugin started with direct spooling on %s:%d", p.config.Host, p.config.Port)

	return nil
}

// Stop gracefully shuts down the sFlow plugin
func (p *Plugin) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cancel == nil {
		return nil // Already stopped
	}

	p.updateHealth(plugins.HealthStatusStopping, "Shutting down sFlow listener", "")

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

	p.updateHealth(plugins.HealthStatusStopped, "sFlow listener stopped", "")
	log.Infof("sFlow plugin stopped")

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

// packetReaderWithSpooling reads sFlow packets and writes directly to filesystem (zero data loss)
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
				log.Errorf("Error reading sFlow packet: %v", err)
				continue
			}

			// Process message with direct spooling
			if n > 0 {
				p.processMessageWithSpooling(buffer[:n], clientAddr)
			}
		}
	}
}

// processMessageWithSpooling processes sFlow message with direct filesystem write (zero data loss)
func (p *Plugin) processMessageWithSpooling(data []byte, clientAddr *net.UDPAddr) {
	// Update metrics
	p.metrics.PacketsReceived++
	p.metrics.BytesReceived += uint64(len(data))
	p.metrics.LastPacketTime = time.Now()

	// Decode sFlow packet using goflow2
	payload := bytes.NewBuffer(data)
	packet := &sflowdec.Packet{}

	// Use DecodeMessageVersion which handles version detection
	err := sflowdec.DecodeMessageVersion(payload, packet)
	if err != nil {
		log.Warnf("Failed to decode sFlow packet from %s: %v", clientAddr.IP.String(), err)
		p.metrics.PacketsDropped++
		return
	}

	p.metrics.FlowsDecoded++

	// Convert decoded packet to JSON
	jsonData, err := json.Marshal(packet)
	if err != nil {
		log.Warnf("Failed to marshal sFlow message to JSON from %s: %v", clientAddr.IP.String(), err)
		p.metrics.PacketsDropped++
		return
	}

	// Add newline for NDJSON format
	ndjsonData := append(jsonData, '\n')

	// Write directly to filesystem - NO CHANNEL DROPS POSSIBLE
	bearerToken := p.config.BearerToken
	dataHint := p.config.DataHint
	if err := p.spooler.StoreRawMessage(p.config.TenantID, p.config.DatasetID, bearerToken, ndjsonData, dataHint); err != nil {
		log.Errorf("Failed to store sFlow message to filesystem from %s: %v", clientAddr.IP.String(), err)
		p.metrics.PacketsDropped++
		return
	}

	log.Debugf("Stored sFlow message from %s:%d as NDJSON directly to filesystem (%d bytes)",
		clientAddr.IP.String(), clientAddr.Port, len(ndjsonData))
}

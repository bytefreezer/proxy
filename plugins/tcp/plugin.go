// Licensed under Elastic License 2.0
// See LICENSE.txt for details

package tcp

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bytefreezer/goodies/log"
	"github.com/bytefreezer/proxy/plugins"
	"github.com/mitchellh/mapstructure"
)

// Plugin implements the TCP input plugin with direct filesystem writes
type Plugin struct {
	config        Config
	listener      net.Listener
	ctx           context.Context
	cancel        context.CancelFunc
	wg            sync.WaitGroup
	health        plugins.PluginHealth
	mu            sync.RWMutex
	spooler       plugins.SpoolingInterface
	metrics       PluginMetrics
	textProcessor *plugins.TextProcessor
	activeConns   int64 // atomic counter for active connections
}

// Config represents TCP plugin configuration
type Config struct {
	Host           string `mapstructure:"host"`
	Port           int    `mapstructure:"port"`
	TenantID       string `mapstructure:"tenant_id"`
	DatasetID      string `mapstructure:"dataset_id"`
	BearerToken    string `mapstructure:"bearer_token,omitempty"`
	DataHint       string `mapstructure:"data_hint,omitempty"`   // Data format hint for downstream processing (defaults to "raw")
	DataFormat     string `mapstructure:"data_format,omitempty"` // data format mode: "ndjson", "text", "auto" (default)
	MaxConnections int    `mapstructure:"max_connections,omitempty"`
	ReadTimeout    int    `mapstructure:"read_timeout,omitempty"`  // seconds
	MaxLineSize    int    `mapstructure:"max_line_size,omitempty"` // bytes
	Delimiter      string `mapstructure:"delimiter,omitempty"`     // line delimiter (default: newline)
}

// PluginMetrics tracks TCP plugin metrics
type PluginMetrics struct {
	ConnectionsTotal  uint64
	ConnectionsActive uint64
	MessagesReceived  uint64
	BytesReceived     uint64
	MessagesDropped   uint64
	LastMessageTime   time.Time
	StartTime         time.Time
}

// NewPlugin creates a new TCP plugin instance
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
		textProcessor: plugins.NewTextProcessor(),
	}
}

// Name returns the plugin name
func (p *Plugin) Name() string {
	return "tcp"
}

// Configure initializes the plugin with configuration
func (p *Plugin) Configure(config map[string]interface{}) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Decode configuration
	if err := mapstructure.Decode(config, &p.config); err != nil {
		return fmt.Errorf("failed to decode TCP plugin config: %w", err)
	}

	// Set defaults
	if p.config.Host == "" {
		p.config.Host = "0.0.0.0"
	}
	if p.config.Port == 0 {
		return fmt.Errorf("port is required for TCP plugin")
	}
	if p.config.TenantID == "" {
		return fmt.Errorf("tenant_id is required for TCP plugin")
	}
	if p.config.DatasetID == "" {
		return fmt.Errorf("dataset_id is required for TCP plugin")
	}
	if p.config.MaxConnections == 0 {
		p.config.MaxConnections = 1000 // Default max connections
	}
	if p.config.ReadTimeout == 0 {
		p.config.ReadTimeout = 300 // 5 minutes default
	}
	if p.config.MaxLineSize == 0 {
		p.config.MaxLineSize = 1048576 // 1MB default max line size
	}
	if p.config.DataFormat == "" {
		p.config.DataFormat = plugins.DataFormatAuto // default to auto-detect
	}
	if p.config.Delimiter == "" {
		p.config.Delimiter = "\n"
	}

	p.updateHealth(plugins.HealthStatusStopped, "Plugin configured successfully", "")
	log.Infof("TCP plugin configured: %s:%d -> %s/%s",
		p.config.Host, p.config.Port, p.config.TenantID, p.config.DatasetID)

	return nil
}

// Start begins accepting TCP connections
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

	// Create TCP listener
	addr := fmt.Sprintf("%s:%d", p.config.Host, p.config.Port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		p.updateHealth(plugins.HealthStatusUnhealthy, "Failed to start TCP listener", err.Error())
		return fmt.Errorf("failed to listen on TCP: %w", err)
	}

	p.listener = listener
	p.updateHealth(plugins.HealthStatusStarting, "Starting TCP listener with direct spooling", "")

	// Start connection acceptor
	p.wg.Add(1)
	go p.acceptConnections()

	p.updateHealth(plugins.HealthStatusHealthy, fmt.Sprintf("TCP listener active on %s:%d", p.config.Host, p.config.Port), "")
	log.Infof("TCP plugin started on %s:%d", p.config.Host, p.config.Port)

	return nil
}

// Stop gracefully shuts down the TCP plugin
func (p *Plugin) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cancel == nil {
		return nil // Already stopped
	}

	p.updateHealth(plugins.HealthStatusStopping, "Shutting down TCP listener", "")

	// Cancel context
	p.cancel()

	// Close listener to stop accepting new connections
	if p.listener != nil {
		p.listener.Close()
	}

	// Wait for all goroutines to finish
	p.wg.Wait()

	p.ctx = nil
	p.cancel = nil
	p.listener = nil

	p.updateHealth(plugins.HealthStatusStopped, "TCP listener stopped", "")
	log.Infof("TCP plugin stopped")

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

// acceptConnections accepts incoming TCP connections
func (p *Plugin) acceptConnections() {
	defer p.wg.Done()

	for {
		select {
		case <-p.ctx.Done():
			return
		default:
			// Set accept deadline to allow checking for context cancellation
			if tcpListener, ok := p.listener.(*net.TCPListener); ok {
				tcpListener.SetDeadline(time.Now().Add(1 * time.Second))
			}

			conn, err := p.listener.Accept()
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					continue // Timeout is expected, continue to check context
				}
				if p.ctx.Err() != nil {
					return // Context cancelled
				}
				log.Errorf("Error accepting TCP connection: %v", err)
				continue
			}

			// Check connection limit
			activeConns := atomic.LoadInt64(&p.activeConns)
			if activeConns >= int64(p.config.MaxConnections) {
				log.Warnf("Max connections reached (%d), rejecting connection from %s",
					p.config.MaxConnections, conn.RemoteAddr().String())
				conn.Close()
				continue
			}

			// Handle connection in a new goroutine
			atomic.AddInt64(&p.activeConns, 1)
			p.metrics.ConnectionsTotal++
			p.wg.Add(1)
			go p.handleConnection(conn)
		}
	}
}

// handleConnection handles a single TCP connection
func (p *Plugin) handleConnection(conn net.Conn) {
	defer p.wg.Done()
	defer conn.Close()
	defer atomic.AddInt64(&p.activeConns, -1)

	clientAddr := conn.RemoteAddr().String()
	log.Debugf("TCP connection accepted from %s", clientAddr)

	// Create buffered reader with max line size
	reader := bufio.NewReaderSize(conn, p.config.MaxLineSize)

	for {
		select {
		case <-p.ctx.Done():
			return
		default:
			// Set read deadline
			conn.SetReadDeadline(time.Now().Add(time.Duration(p.config.ReadTimeout) * time.Second))

			// Read until delimiter
			line, err := reader.ReadBytes(p.config.Delimiter[0])
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					log.Debugf("TCP connection from %s timed out", clientAddr)
					return
				}
				if err.Error() == "EOF" {
					log.Debugf("TCP connection from %s closed", clientAddr)
					return
				}
				if p.ctx.Err() != nil {
					return // Context cancelled
				}
				log.Warnf("Error reading from TCP connection %s: %v", clientAddr, err)
				return
			}

			// Process the line
			if len(line) > 0 {
				p.processLine(line, clientAddr)
			}
		}
	}
}

// processLine processes a single line from TCP connection
func (p *Plugin) processLine(data []byte, clientAddr string) {
	// Update metrics
	p.metrics.MessagesReceived++
	p.metrics.BytesReceived += uint64(len(data))
	p.metrics.LastMessageTime = time.Now()

	// Minimal cleanup - remove delimiter and null bytes
	payload := bytes.TrimRight(data, p.config.Delimiter+"\x00")
	payload = bytes.TrimSpace(payload)
	if len(payload) == 0 {
		return
	}

	// Process through text processor (line-by-line detection and wrapping)
	processed, wrapped, err := p.textProcessor.ProcessLine(payload, p.config.TenantID, p.config.DatasetID, p.config.DataFormat)
	if err != nil {
		log.Warnf("Failed to process line for %s/%s from %s: %v",
			p.config.TenantID, p.config.DatasetID, clientAddr, err)
		p.metrics.MessagesDropped++
		return
	}

	// Ensure newline at end
	if len(processed) > 0 && processed[len(processed)-1] != '\n' {
		processed = append(processed, '\n')
	}

	if wrapped {
		log.Debugf("TCP: Wrapped text line from %s (data_format: %s)", clientAddr, p.config.DataFormat)
	}

	// Write directly to filesystem - NO CHANNEL DROPS POSSIBLE
	bearerToken := p.config.BearerToken
	dataHint := p.config.DataHint
	if dataHint == "" {
		dataHint = "raw"
	}
	if err := p.spooler.StoreRawMessage(p.config.TenantID, p.config.DatasetID, bearerToken, processed, dataHint); err != nil {
		log.Errorf("Failed to store TCP message to filesystem from %s: %v", clientAddr, err)
		p.metrics.MessagesDropped++
		return
	}

	log.Debugf("Stored TCP message from %s directly to filesystem (%d bytes)", clientAddr, len(processed))
}

// Schema returns the TCP plugin configuration schema
func (p *Plugin) Schema() plugins.PluginSchema {
	return plugins.PluginSchema{
		Name:        "tcp",
		DisplayName: "TCP Generic",
		Description: "Generic TCP data receiver for line-delimited data. Each line is processed individually. Outputs structured NDJSON with TCP metadata.",
		Category:    "TCP-based (Generic)",
		Transport:   "TCP",
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
				Description: "TCP port to listen on",
				Validation:  "1-65535",
				Placeholder: "5000",
				Group:       "Network",
			},
			{
				Name:        "max_connections",
				Type:        "int",
				Required:    false,
				Default:     1000,
				Description: "Maximum concurrent connections",
				Validation:  "min:1,max:100000",
				Placeholder: "1000",
				Group:       "Performance",
			},
			{
				Name:        "read_timeout",
				Type:        "int",
				Required:    false,
				Default:     300,
				Description: "Connection read timeout in seconds",
				Validation:  "min:1,max:3600",
				Placeholder: "300",
				Group:       "Performance",
			},
			{
				Name:        "max_line_size",
				Type:        "int",
				Required:    false,
				Default:     1048576,
				Description: "Maximum line size in bytes (1MB default)",
				Validation:  "min:1024,max:16777216",
				Placeholder: "1048576",
				Group:       "Performance",
			},
			{
				Name:        "delimiter",
				Type:        "string",
				Required:    false,
				Default:     "\n",
				Description: "Line delimiter character",
				Placeholder: "\\n",
				Group:       "Parsing",
			},
		},
	}
}

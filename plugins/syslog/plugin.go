// Licensed under Elastic License 2.0
// See LICENSE.txt for details

package syslog

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"regexp"
	"sync"
	"time"

	"github.com/bytedance/sonic"
	"github.com/bytefreezer/goodies/log"
	"github.com/bytefreezer/proxy/plugins"
	"github.com/mitchellh/mapstructure"
)

// Plugin implements the Syslog input plugin with direct filesystem writes
// Parses syslog messages (RFC3164/RFC5424) and converts to NDJSON format
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

// Config represents Syslog plugin configuration
type Config struct {
	Host           string `mapstructure:"host"`
	Port           int    `mapstructure:"port"`
	TenantID       string `mapstructure:"tenant_id"`
	DatasetID      string `mapstructure:"dataset_id"`
	BearerToken    string `mapstructure:"bearer_token,omitempty"`
	SyslogMode     string `mapstructure:"syslog_mode,omitempty"` // "rfc3164", "rfc5424", "auto"
	ReadBufferSize int    `mapstructure:"read_buffer_size,omitempty"`
}

// PluginMetrics tracks Syslog plugin metrics
type PluginMetrics struct {
	MessagesReceived uint64
	BytesReceived    uint64
	MessagesDropped  uint64
	ParseErrors      uint64
	LastMessageTime  time.Time
	StartTime        time.Time
}

// RFC3164 syslog format: <PRI>TIMESTAMP HOSTNAME TAG: MESSAGE
var rfc3164Pattern = regexp.MustCompile(`^<(\d+)>(\w{3}\s+\d+\s+\d+:\d+:\d+)\s+(\S+)\s+(\S+?):\s*(.*)$`)

// RFC5424 syslog format: <PRI>VERSION TIMESTAMP HOSTNAME APP-NAME PROCID MSGID [STRUCTURED-DATA] MSG
var rfc5424Pattern = regexp.MustCompile(`^<(\d+)>(\d+)\s+(\S+)\s+(\S+)\s+(\S+)\s+(\S+)\s+(\S+)\s+(?:\[.*?\]\s+)?(.*)$`)

// NewPlugin creates a new Syslog plugin instance
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
	return "syslog"
}

// Configure initializes the plugin with configuration
func (p *Plugin) Configure(config map[string]interface{}) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Decode configuration
	if err := mapstructure.Decode(config, &p.config); err != nil {
		return fmt.Errorf("failed to decode Syslog plugin config: %w", err)
	}

	// Set defaults
	if p.config.Host == "" {
		p.config.Host = "0.0.0.0"
	}
	if p.config.Port == 0 {
		return fmt.Errorf("port is required for Syslog plugin")
	}
	if p.config.TenantID == "" {
		return fmt.Errorf("tenant_id is required for Syslog plugin")
	}
	if p.config.DatasetID == "" {
		return fmt.Errorf("dataset_id is required for Syslog plugin")
	}
	if p.config.SyslogMode == "" {
		p.config.SyslogMode = "auto" // auto-detect RFC3164 vs RFC5424
	}
	if p.config.ReadBufferSize == 0 {
		p.config.ReadBufferSize = 8388608 // 8MB default for burst handling
	}

	// Validate syslog mode
	validModes := map[string]bool{
		"auto": true, "rfc3164": true, "rfc5424": true,
	}
	if !validModes[p.config.SyslogMode] {
		return fmt.Errorf("invalid syslog_mode %s (supported: auto, rfc3164, rfc5424)", p.config.SyslogMode)
	}

	p.updateHealth(plugins.HealthStatusStopped, "Plugin configured successfully", "")
	log.Infof("Syslog plugin configured: %s:%d -> %s/%s (mode: %s)",
		p.config.Host, p.config.Port, p.config.TenantID, p.config.DatasetID, p.config.SyslogMode)

	return nil
}

// Start begins consuming Syslog data
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

	p.updateHealth(plugins.HealthStatusStarting, "Starting Syslog listener with direct spooling", "")

	// Start packet reader with direct spooling
	p.wg.Add(1)
	go p.packetReaderWithSpooling()

	p.updateHealth(plugins.HealthStatusHealthy, fmt.Sprintf("Syslog listener active on %s:%d", p.config.Host, p.config.Port), "")
	log.Infof("Syslog plugin started on %s:%d", p.config.Host, p.config.Port)

	return nil
}

// Stop gracefully shuts down the Syslog plugin
func (p *Plugin) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cancel == nil {
		return nil // Already stopped
	}

	p.updateHealth(plugins.HealthStatusStopping, "Shutting down Syslog listener", "")

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

	p.updateHealth(plugins.HealthStatusStopped, "Syslog listener stopped", "")
	log.Infof("Syslog plugin stopped")

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
				log.Errorf("Error reading syslog packet: %v", err)
				continue
			}

			// Process message with direct spooling
			if n > 0 {
				p.processMessageWithSpooling(buffer[:n], clientAddr)
			}
		}
	}
}

// processMessageWithSpooling processes syslog message and converts to NDJSON
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

	// Parse syslog message into NDJSON
	ndjsonData, err := p.parseSyslogToNDJSON(payload, clientAddr)
	if err != nil {
		log.Warnf("Failed to parse syslog message from %s: %v", clientAddr.IP.String(), err)
		p.metrics.ParseErrors++
		return
	}

	// Write directly to filesystem - outputs NDJSON format
	bearerToken := p.config.BearerToken
	if err := p.spooler.StoreRawMessage(p.config.TenantID, p.config.DatasetID, bearerToken, ndjsonData, "ndjson"); err != nil {
		log.Errorf("Failed to store syslog message to filesystem from %s: %v", clientAddr.IP.String(), err)
		p.metrics.MessagesDropped++
		return
	}

	log.Debugf("Stored syslog message from %s:%d as NDJSON (%d bytes)",
		clientAddr.IP.String(), clientAddr.Port, len(ndjsonData))
}

// parseSyslogToNDJSON parses a syslog message and converts it to NDJSON format
func (p *Plugin) parseSyslogToNDJSON(data []byte, clientAddr *net.UDPAddr) ([]byte, error) {
	syslogMsg := make(map[string]interface{})
	syslogMsg["source_ip"] = clientAddr.IP.String()
	syslogMsg["timestamp"] = time.Now().UTC().Format(time.RFC3339)

	var message string
	var parsed bool

	// Try RFC5424 first (if mode is auto or rfc5424)
	if p.config.SyslogMode == "auto" || p.config.SyslogMode == "rfc5424" {
		if matches := rfc5424Pattern.FindSubmatch(data); matches != nil {
			syslogMsg["priority"] = string(matches[1])
			syslogMsg["version"] = string(matches[2])
			syslogMsg["syslog_timestamp"] = string(matches[3])
			syslogMsg["hostname"] = string(matches[4])
			syslogMsg["app_name"] = string(matches[5])
			syslogMsg["proc_id"] = string(matches[6])
			syslogMsg["msg_id"] = string(matches[7])
			message = string(matches[8])
			syslogMsg["rfc"] = "5424"
			parsed = true
		}
	}

	// Try RFC3164 (if not yet parsed and mode is auto or rfc3164)
	if !parsed && (p.config.SyslogMode == "auto" || p.config.SyslogMode == "rfc3164") {
		if matches := rfc3164Pattern.FindSubmatch(data); matches != nil {
			syslogMsg["priority"] = string(matches[1])
			syslogMsg["syslog_timestamp"] = string(matches[2])
			syslogMsg["hostname"] = string(matches[3])
			syslogMsg["tag"] = string(matches[4])
			message = string(matches[5])
			syslogMsg["rfc"] = "3164"
			parsed = true
		}
	}

	// If still not parsed, treat as raw message
	if !parsed {
		message = string(data)
		syslogMsg["rfc"] = "raw"
	}

	// Store the actual syslog message in the "message" field
	syslogMsg["message"] = message

	// Marshal to compact NDJSON
	ndjsonData, err := sonic.Marshal(syslogMsg)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal syslog to JSON: %w", err)
	}

	return ndjsonData, nil
}

// Schema returns the Syslog plugin configuration schema
func (p *Plugin) Schema() plugins.PluginSchema {
	return plugins.PluginSchema{
		Name:        "syslog",
		DisplayName: "Syslog",
		Description: "Syslog receiver supporting RFC3164 and RFC5424 formats. Parses syslog messages line-by-line and converts to structured NDJSON with message field.",
		Category:    "System Logs",
		Transport:   "UDP",
		DefaultPort: 514,
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
				Default:     514,
				Description: "UDP port to listen on (514 is standard syslog port)",
				Validation:  "1-65535",
				Placeholder: "514",
				Group:       "Network",
			},
			{
				Name:        "syslog_mode",
				Type:        "string",
				Required:    false,
				Default:     "auto",
				Description: "Syslog parsing mode",
				Options:     []string{"auto", "rfc3164", "rfc5424"},
				Placeholder: "auto",
				Group:       "Parsing",
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
		},
	}
}

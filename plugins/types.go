// Licensed under Elastic License 2.0
// See LICENSE.txt for details

package plugins

import (
	"context"
	"fmt"
	"net"
	"syscall"
	"time"

	"github.com/bytefreezer/goodies/log"
)

// InputPlugin defines the interface that all input plugins must implement
// All plugins now write directly to filesystem first (zero data loss architecture)
type InputPlugin interface {
	// Name returns the plugin name (e.g., "udp", "kafka", "nats")
	Name() string

	// Configure initializes the plugin with config
	Configure(config map[string]interface{}) error

	// Start begins consuming data and writes directly to filesystem
	// This method bypasses channels entirely for guaranteed data persistence
	Start(ctx context.Context, spooler SpoolingInterface) error

	// Stop gracefully shuts down the plugin
	Stop() error

	// Health returns plugin health status
	Health() PluginHealth

	// Schema returns the plugin's configuration schema for dynamic UI generation
	Schema() PluginSchema
}

// SpoolingInterface defines the interface plugins use for direct filesystem writes
type SpoolingInterface interface {
	StoreRawMessage(tenantID, datasetID, bearerToken string, data []byte, dataHint string) error
	// ReportWarning reports a warning to the control service (optional - may be nil)
	ReportWarning(tenantID, datasetID, warningType, message string)
	// ResolveWarning resolves a previously reported warning (called when issue is fixed)
	ResolveWarning(datasetID, warningType string)
}

// DataMessage represents raw data from any input source
type DataMessage struct {
	Data      []byte            `json:"data"`
	TenantID  string            `json:"tenant_id"`
	DatasetID string            `json:"dataset_id"`
	DataHint  string            `json:"data_hint"` // Data format hint for downstream processing (defaults to "raw")
	Metadata  map[string]string `json:"metadata"`  // Source-specific metadata
	Timestamp time.Time         `json:"timestamp"`
	SourceIP  string            `json:"source_ip,omitempty"` // For UDP/network sources
}

// PluginHealth represents the health status of a plugin
type PluginHealth struct {
	Status      HealthStatus `json:"status"`
	Message     string       `json:"message"`
	LastError   string       `json:"last_error,omitempty"`
	LastUpdated time.Time    `json:"last_updated"`
}

// HealthStatus represents plugin health states
type HealthStatus string

const (
	HealthStatusHealthy   HealthStatus = "healthy"
	HealthStatusUnhealthy HealthStatus = "unhealthy"
	HealthStatusStarting  HealthStatus = "starting"
	HealthStatusStopping  HealthStatus = "stopping"
	HealthStatusStopped   HealthStatus = "stopped"
)

// InputPluginFactory creates new instances of input plugins
type InputPluginFactory func() InputPlugin

// Data Format Constants
// These constants define how input data should be detected and processed.
const (
	DataFormatNDJSON = "ndjson" // Explicit JSON - pass through as-is, no detection
	DataFormatText   = "text"   // Explicit text - always wrap lines in JSON envelope
	DataFormatAuto   = "auto"   // Auto-detect - try JSON first, cache mode per dataset
)

// Data Hint Constants
// These constants define the supported data format hints for downstream processing.
// When data_hint is not specified, it defaults to "raw".
const (
	// Text-based formats
	DataHintNDJSON   = "ndjson"   // Newline-delimited JSON
	DataHintCSV      = "csv"      // Comma-separated values
	DataHintTSV      = "tsv"      // Tab-separated values
	DataHintApache   = "apache"   // Apache access/error logs
	DataHintNginx    = "nginx"    // Nginx access/error logs
	DataHintIIS      = "iis"      // Windows IIS web server logs
	DataHintSquid    = "squid"    // Squid proxy cache logs
	DataHintInflux   = "influx"   // InfluxDB Line Protocol - time-series data
	DataHintProm     = "prom"     // Prometheus Text Format - metrics exposition
	DataHintStatsD   = "statsd"   // StatsD - metrics protocol
	DataHintGraphite = "graphite" // Graphite Plaintext Protocol - metrics format
	DataHintSyslog   = "syslog"   // Syslog RFC5424 - structured system logs
	DataHintCEF      = "cef"      // Common Event Format by ArcSight
	DataHintGELF     = "gelf"     // Graylog Extended Log Format
	DataHintLEEF     = "leef"     // Log Event Extended Format by IBM
	DataHintCLF      = "log"      // CLF/NCSA Combined - Common/Combined Log Format
	DataHintFIX      = "fix"      // FIX Protocol - Financial Information eXchange
	DataHintHL7      = "hl7"      // HL7 v2 - Healthcare messaging standard

	// Default format
	DataHintRaw = "raw" // Raw data - no specific format processing
)

// UDPPluginTypes defines plugin types that use UDP transport
var UDPPluginTypes = map[string]bool{
	"udp":     true,
	"syslog":  true,
	"netflow": true,
	"sflow":   true,
	"ipfix":   true,
	"ebpf":    true,
}

// PluginSchema defines the configuration schema for a plugin
// This allows the UI to dynamically generate forms based on plugin capabilities
type PluginSchema struct {
	Name        string              `json:"name"`                   // Plugin name (e.g., "http", "kafka")
	DisplayName string              `json:"display_name"`           // Human-readable name (e.g., "HTTP Webhook")
	Description string              `json:"description"`            // Plugin description
	Category    string              `json:"category"`               // Category (e.g., "HTTP-based", "Message Queue")
	Transport   string              `json:"transport"`              // Transport protocol (e.g., "TCP", "UDP")
	DefaultPort int                 `json:"default_port,omitempty"` // Default port if applicable
	Fields      []PluginFieldSchema `json:"fields"`                 // Configuration fields
}

// PluginFieldSchema defines a single configuration field
type PluginFieldSchema struct {
	Name        string      `json:"name"`                  // Field name (e.g., "port", "host")
	Type        string      `json:"type"`                  // Field type: "string", "int", "bool", "[]string"
	Required    bool        `json:"required"`              // Whether field is required
	Default     interface{} `json:"default,omitempty"`     // Default value
	Description string      `json:"description"`           // Field description
	Validation  string      `json:"validation,omitempty"`  // Validation rule (e.g., "1-65535", "min:1,max:10")
	Placeholder string      `json:"placeholder,omitempty"` // Placeholder text for UI
	Options     []string    `json:"options,omitempty"`     // Valid options for enum-like fields
	Group       string      `json:"group,omitempty"`       // Field group for UI organization
}

// BufferCheckResult contains the result of setting UDP buffer size
type BufferCheckResult struct {
	RequestedSize int
	ActualSize    int
	Limited       bool
	Warning       string // Warning message if kernel limited the buffer
	SysctlCmd     string // The sysctl command to fix the issue
}

// SetUDPReadBufferWithCheck sets the UDP socket read buffer and verifies it was applied.
// If the kernel limits the buffer to a smaller size, it logs a warning with the
// required sysctl command to increase the limit.
// Returns a BufferCheckResult with details about what was set and any warnings.
func SetUDPReadBufferWithCheck(conn *net.UDPConn, requestedSize int) BufferCheckResult {
	result := BufferCheckResult{
		RequestedSize: requestedSize,
	}

	// Try to set the requested buffer size
	if err := conn.SetReadBuffer(requestedSize); err != nil {
		log.Warnf("Failed to set UDP read buffer size to %d: %v", requestedSize, err)
		result.Warning = fmt.Sprintf("Failed to set buffer: %v", err)
		return result
	}

	// Read back the actual buffer size using syscall
	rawConn, err := conn.SyscallConn()
	if err != nil {
		log.Warnf("Could not get syscall conn to verify buffer size: %v", err)
		result.ActualSize = requestedSize // Assume it worked
		return result
	}

	err = rawConn.Control(func(fd uintptr) {
		// Get the actual receive buffer size
		// Note: Kernel doubles the value for overhead, so we read what was actually set
		val, err := syscall.GetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_RCVBUF)
		if err != nil {
			log.Warnf("Could not read socket buffer size: %v", err)
			return
		}
		// Kernel doubles the requested value, so divide by 2 to get effective size
		result.ActualSize = val / 2
	})

	if err != nil {
		log.Warnf("Failed to verify UDP buffer size: %v", err)
		result.ActualSize = requestedSize
		return result
	}

	// Check if we got what we asked for
	if result.ActualSize < requestedSize {
		result.Limited = true
		// Calculate required rmem_max (needs to be at least 2x the desired size due to kernel doubling)
		requiredMax := requestedSize * 2
		result.SysctlCmd = fmt.Sprintf("sudo sysctl -w net.core.rmem_max=%d", requiredMax)
		result.Warning = fmt.Sprintf("UDP buffer limited by kernel: requested %s, got %s. Run: %s",
			formatBytes(requestedSize), formatBytes(result.ActualSize), result.SysctlCmd)

		log.Warnf("UDP buffer size limited by kernel: requested %s, got %s",
			formatBytes(requestedSize), formatBytes(result.ActualSize))
		log.Warnf("To fix, run: %s", result.SysctlCmd)
		log.Warnf("To make permanent, add to /etc/sysctl.conf: net.core.rmem_max=%d", requiredMax)
	} else {
		log.Infof("UDP read buffer set to %s", formatBytes(result.ActualSize))
	}

	return result
}

// formatBytes formats bytes into human-readable format
func formatBytes(bytes int) string {
	const (
		KB = 1024
		MB = KB * 1024
	)
	switch {
	case bytes >= MB:
		return fmt.Sprintf("%dMB", bytes/MB)
	case bytes >= KB:
		return fmt.Sprintf("%dKB", bytes/KB)
	default:
		return fmt.Sprintf("%d bytes", bytes)
	}
}

package plugins

import (
	"context"
	"time"
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
}

// SpoolingInterface defines the interface plugins use for direct filesystem writes
type SpoolingInterface interface {
	StoreRawMessage(tenantID, datasetID, bearerToken string, data []byte, dataHint string) error
}

// DataMessage represents raw data from any input source
type DataMessage struct {
	Data      []byte            `json:"data"`
	TenantID  string            `json:"tenant_id"`
	DatasetID string            `json:"dataset_id"`
	DataHint  string            `json:"data_hint"`           // Data format hint for downstream processing (defaults to "raw")
	Metadata  map[string]string `json:"metadata"`            // Source-specific metadata
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

// Data Hint Constants
// These constants define the supported data format hints for downstream processing.
// When data_hint is not specified, it defaults to "raw".
const (
	// Text-based formats
	DataHintNDJSON    = "ndjson"    // Newline-delimited JSON
	DataHintCSV       = "csv"       // Comma-separated values
	DataHintTSV       = "tsv"       // Tab-separated values
	DataHintApache    = "apache"    // Apache access/error logs
	DataHintNginx     = "nginx"     // Nginx access/error logs
	DataHintIIS       = "iis"       // Windows IIS web server logs
	DataHintSquid     = "squid"     // Squid proxy cache logs
	DataHintInflux    = "influx"    // InfluxDB Line Protocol - time-series data
	DataHintProm      = "prom"      // Prometheus Text Format - metrics exposition
	DataHintStatsD    = "statsd"    // StatsD - metrics protocol
	DataHintGraphite  = "graphite"  // Graphite Plaintext Protocol - metrics format
	DataHintSyslog    = "syslog"    // Syslog RFC5424 - structured system logs
	DataHintCEF       = "cef"       // Common Event Format by ArcSight
	DataHintGELF      = "gelf"      // Graylog Extended Log Format
	DataHintLEEF      = "leef"      // Log Event Extended Format by IBM
	DataHintCLF       = "log"       // CLF/NCSA Combined - Common/Combined Log Format
	DataHintFIX       = "fix"       // FIX Protocol - Financial Information eXchange
	DataHintHL7       = "hl7"       // HL7 v2 - Healthcare messaging standard

	// Binary formats
	DataHintSFlow     = "sflow"     // sFlow - Sampled network packet data (BINARY)
	DataHintNetFlow   = "netflow"   // NetFlow v5/v9 - Cisco flow data (BINARY)
	DataHintIPFIX     = "ipfix"     // IPFIX - IP Flow Information Export (BINARY)

	// Default format
	DataHintRaw       = "raw"       // Raw data - no specific format processing
)

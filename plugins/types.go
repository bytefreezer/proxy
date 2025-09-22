package plugins

import (
	"context"
	"time"
)

// InputPlugin defines the interface that all input plugins must implement
type InputPlugin interface {
	// Name returns the plugin name (e.g., "udp", "kafka", "nats")
	Name() string

	// Configure initializes the plugin with config
	Configure(config map[string]interface{}) error

	// Start begins consuming data and sends to output channel
	Start(ctx context.Context, output chan<- *DataMessage) error

	// Stop gracefully shuts down the plugin
	Stop() error

	// Health returns plugin health status
	Health() PluginHealth
}

// DataMessage represents raw data from any input source
type DataMessage struct {
	Data          []byte            `json:"data"`
	TenantID      string            `json:"tenant_id"`
	DatasetID     string            `json:"dataset_id"`
	FileExtension string            `json:"file_extension"`      // Plugin-defined file extension
	Metadata      map[string]string `json:"metadata"`            // Source-specific metadata
	Timestamp     time.Time         `json:"timestamp"`
	SourceIP      string            `json:"source_ip,omitempty"` // For UDP/network sources
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

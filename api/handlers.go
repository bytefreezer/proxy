package api

import (
	"context"
	"time"

	"github.com/n0needt0/bytefreezer-proxy/config"
	"github.com/n0needt0/bytefreezer-proxy/services"
	"github.com/n0needt0/go-goodies/log"
	"github.com/swaggest/usecase"
)

// HealthResponse represents the health check response
type HealthResponse struct {
	Status      string               `json:"status"`
	Version     string               `json:"version"`
	ServiceName string               `json:"service_name"`
	Timestamp   string               `json:"timestamp"`
	UDP         UDPHealthStatus      `json:"udp"`
	Receiver    ReceiverHealthStatus `json:"receiver"`
	Stats       ProxyStatsResponse   `json:"stats"`
}

type UDPHealthStatus struct {
	Enabled   bool          `json:"enabled"`
	Host      string        `json:"host"`
	Listeners []UDPListener `json:"listeners"`
	Status    string        `json:"status"`
}

type UDPListener struct {
	Port      int    `json:"port"`
	DatasetID string `json:"dataset_id"`
	TenantID  string `json:"tenant_id,omitempty"`
}

type ReceiverHealthStatus struct {
	BaseURL   string `json:"base_url"`
	TenantID  string `json:"tenant_id"`
	DatasetID string `json:"dataset_id"`
	Status    string `json:"status"`
}

type ProxyStatsResponse struct {
	UDPMessagesReceived int64  `json:"udp_messages_received"`
	UDPMessageErrors    int64  `json:"udp_message_errors"`
	BatchesCreated      int64  `json:"batches_created"`
	BatchesForwarded    int64  `json:"batches_forwarded"`
	ForwardingErrors    int64  `json:"forwarding_errors"`
	BytesReceived       int64  `json:"bytes_received"`
	BytesForwarded      int64  `json:"bytes_forwarded"`
	LastActivity        string `json:"last_activity"`
	UptimeSeconds       int64  `json:"uptime_seconds"`
}

// ConfigResponse represents the current system configuration
type ConfigResponse struct {
	App          AppConfig            `json:"app"`
	Server       ServerConfig         `json:"server"`
	UDP          UDPConfig            `json:"udp"`
	Receiver     ReceiverConfigMasked `json:"receiver"`
	SOC          SOCConfig            `json:"soc"`
	Otel         OtelConfig           `json:"otel"`
	Housekeeping HousekeepingConfig   `json:"housekeeping"`
	Dev          bool                 `json:"dev"`
}

type AppConfig struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type ServerConfig struct {
	ApiPort int `json:"api_port"`
}

type UDPConfig struct {
	Enabled             bool          `json:"enabled"`
	Host                string        `json:"host"`
	Listeners           []UDPListener `json:"listeners"`
	ReadBufferSizeBytes int           `json:"read_buffer_size_bytes"`
	MaxBatchLines       int           `json:"max_batch_lines"`
	MaxBatchBytes       int64         `json:"max_batch_bytes"`
	BatchTimeoutSeconds int           `json:"batch_timeout_seconds"`
	CompressionLevel    int           `json:"compression_level"`
	EnableCompression   bool          `json:"enable_compression"`
}

type ReceiverConfigMasked struct {
	BaseURL       string `json:"base_url"`
	TenantID      string `json:"tenant_id"` // This will be masked
	DatasetID     string `json:"dataset_id"`
	TimeoutSec    int    `json:"timeout_seconds"`
	RetryCount    int    `json:"retry_count"`
	RetryDelaySec int    `json:"retry_delay_seconds"`
}

type SOCConfig struct {
	Enabled  bool   `json:"enabled"`
	Endpoint string `json:"endpoint"`
	Timeout  int    `json:"timeout"`
}

type OtelConfig struct {
	Enabled               bool   `json:"enabled"`
	Endpoint              string `json:"endpoint"`
	ServiceName           string `json:"service_name"`
	ScrapeIntervalSeconds int    `json:"scrape_interval_seconds"`
}

type HousekeepingConfig struct {
	Enabled         bool `json:"enabled"`
	IntervalSeconds int  `json:"interval_seconds"`
}

// API holds the API configuration and services
type API struct {
	Services *services.Services
	Config   *config.Config
}

// NewAPI creates a new API instance
func NewAPI(services *services.Services, conf *config.Config) *API {
	return &API{
		Services: services,
		Config:   conf,
	}
}

// maskSensitiveValue masks sensitive configuration values
func maskSensitiveValue(value string) string {
	if value == "" {
		return ""
	}
	if len(value) <= 8 {
		return "***"
	}
	return value[:4] + "***" + value[len(value)-4:]
}

// HealthCheck returns a health check handler
func (api *API) HealthCheck() usecase.Interactor {
	u := usecase.NewInteractor(func(ctx context.Context, input struct{}, output *HealthResponse) error {
		cfg := api.Config
		stats := api.Services.GetStats()

		// Determine overall status
		overallStatus := "healthy"
		if !api.Services.IsHealthy() {
			overallStatus = "degraded"
		}

		output.Status = overallStatus
		output.Version = cfg.App.Version
		output.ServiceName = cfg.App.Name
		output.Timestamp = time.Now().UTC().Format(time.RFC3339)

		// UDP status
		udpStatus := "disabled"
		if cfg.UDP.Enabled {
			udpStatus = "enabled"
			// TODO: Add actual UDP listener health check
		}

		output.UDP = UDPHealthStatus{
			Enabled:   cfg.UDP.Enabled,
			Host:      cfg.UDP.Host,
			Listeners: convertListeners(cfg.UDP.Listeners),
			Status:    udpStatus,
		}

		// Receiver status
		receiverStatus := "unknown"
		if cfg.Receiver.BaseURL != "" {
			receiverStatus = "configured"
			// TODO: Add actual receiver connectivity check
		}

		output.Receiver = ReceiverHealthStatus{
			BaseURL:   cfg.Receiver.BaseURL,
			TenantID:  maskSensitiveValue(cfg.Receiver.TenantID),
			DatasetID: cfg.Receiver.DatasetID,
			Status:    receiverStatus,
		}

		// Stats
		output.Stats = ProxyStatsResponse{
			UDPMessagesReceived: stats.UDPMessagesReceived,
			UDPMessageErrors:    stats.UDPMessageErrors,
			BatchesCreated:      stats.BatchesCreated,
			BatchesForwarded:    stats.BatchesForwarded,
			ForwardingErrors:    stats.ForwardingErrors,
			BytesReceived:       stats.BytesReceived,
			BytesForwarded:      stats.BytesForwarded,
			LastActivity:        stats.LastActivity.Format(time.RFC3339),
			UptimeSeconds:       stats.UptimeSeconds,
		}

		log.Debugf("Health check completed: status=%s", overallStatus)
		return nil
	})

	u.SetTitle("Health Check")
	u.SetDescription("Check the health status of the ByteFreezer Proxy service")
	u.SetTags("Health")

	return u
}

// GetConfig returns a handler for getting current system configuration
func (api *API) GetConfig() usecase.Interactor {
	u := usecase.NewInteractor(func(ctx context.Context, input struct{}, output *ConfigResponse) error {
		cfg := api.Config

		// Basic app configuration
		output.App = AppConfig{
			Name:    cfg.App.Name,
			Version: cfg.App.Version,
		}

		// Server configuration
		output.Server = ServerConfig{
			ApiPort: cfg.Server.ApiPort,
		}

		// UDP configuration
		output.UDP = UDPConfig{
			Enabled:             cfg.UDP.Enabled,
			Host:                cfg.UDP.Host,
			Listeners:           convertListeners(cfg.UDP.Listeners),
			ReadBufferSizeBytes: cfg.UDP.ReadBufferSizeBytes,
			MaxBatchLines:       cfg.UDP.MaxBatchLines,
			MaxBatchBytes:       cfg.UDP.MaxBatchBytes,
			BatchTimeoutSeconds: cfg.UDP.BatchTimeoutSeconds,
			CompressionLevel:    cfg.UDP.CompressionLevel,
			EnableCompression:   cfg.UDP.EnableCompression,
		}

		// Receiver configuration (with masked ID)
		output.Receiver = ReceiverConfigMasked{
			BaseURL:       cfg.Receiver.BaseURL,
			TenantID:      maskSensitiveValue(cfg.Receiver.TenantID),
			DatasetID:     cfg.Receiver.DatasetID,
			TimeoutSec:    cfg.Receiver.TimeoutSec,
			RetryCount:    cfg.Receiver.RetryCount,
			RetryDelaySec: cfg.Receiver.RetryDelaySec,
		}

		// SOC configuration
		output.SOC = SOCConfig{
			Enabled:  cfg.SOC.Enabled,
			Endpoint: cfg.SOC.Endpoint,
			Timeout:  cfg.SOC.Timeout,
		}

		// OTEL configuration
		output.Otel = OtelConfig{
			Enabled:               cfg.Otel.Enabled,
			Endpoint:              cfg.Otel.Endpoint,
			ServiceName:           cfg.Otel.ServiceName,
			ScrapeIntervalSeconds: cfg.Otel.ScrapeIntervalSeconds,
		}

		// Housekeeping configuration
		output.Housekeeping = HousekeepingConfig{
			Enabled:         cfg.Housekeeping.Enabled,
			IntervalSeconds: cfg.Housekeeping.IntervalSeconds,
		}

		// Dev flag
		output.Dev = cfg.Dev

		log.Debugf("Retrieved system configuration")
		return nil
	})

	u.SetTitle("Get System Configuration")
	u.SetDescription("Retrieve the current system configuration (sensitive values are masked)")
	u.SetTags("Configuration")

	return u
}

// convertListeners converts config UDP listeners to API response format
func convertListeners(configListeners []config.UDPListener) []UDPListener {
	listeners := make([]UDPListener, len(configListeners))
	for i, l := range configListeners {
		listeners[i] = UDPListener{
			Port:      l.Port,
			DatasetID: l.DatasetID,
			TenantID:  l.TenantID,
		}
	}
	return listeners
}

package api

import (
	"context"
	"fmt"
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
	BaseURL string `json:"base_url"`
	Status  string `json:"status"`
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
	TenantID     string               `json:"tenant_id"`    // This will be masked
	BearerToken  string               `json:"bearer_token"` // This will be masked
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
	// Compression is always enabled for raw data
}

type ReceiverConfigMasked struct {
	BaseURL       string `json:"base_url"`
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

// DLQStatsResponse represents DLQ and spooling statistics
type DLQStatsResponse struct {
	SpoolingEnabled   bool                       `json:"spooling_enabled"`
	TotalFilesInQueue int                        `json:"total_files_in_queue"`
	TotalFilesInDLQ   int                        `json:"total_files_in_dlq"`
	TotalBytesInQueue int64                      `json:"total_bytes_in_queue"`
	TotalBytesInDLQ   int64                      `json:"total_bytes_in_dlq"`
	TenantStats       map[string]*TenantDLQStats `json:"tenant_stats"`
	OldestQueueFile   *FileInfo                  `json:"oldest_queue_file,omitempty"`
	OldestDLQFile     *FileInfo                  `json:"oldest_dlq_file,omitempty"`
	SpoolDirectory    string                     `json:"spool_directory"`
}

// TenantDLQStats represents per-tenant DLQ statistics
type TenantDLQStats struct {
	QueueFiles   int                         `json:"queue_files"`
	DLQFiles     int                         `json:"dlq_files"`
	QueueBytes   int64                       `json:"queue_bytes"`
	DLQBytes     int64                       `json:"dlq_bytes"`
	DatasetStats map[string]*DatasetDLQStats `json:"dataset_stats"`
}

// DatasetDLQStats represents per-dataset DLQ statistics
type DatasetDLQStats struct {
	QueueFiles int   `json:"queue_files"`
	DLQFiles   int   `json:"dlq_files"`
	QueueBytes int64 `json:"queue_bytes"`
	DLQBytes   int64 `json:"dlq_bytes"`
}

// FileInfo represents basic file information
type FileInfo struct {
	ID            string    `json:"id"`
	TenantID      string    `json:"tenant_id"`
	DatasetID     string    `json:"dataset_id"`
	Size          int64     `json:"size"`
	CreatedAt     time.Time `json:"created_at"`
	RetryCount    int       `json:"retry_count"`
	FailureReason string    `json:"failure_reason,omitempty"`
}

// DLQRetryRequest represents a request to retry DLQ files
type DLQRetryRequest struct {
	TenantID  string `json:"tenant_id,omitempty"`  // Optional: specific tenant
	DatasetID string `json:"dataset_id,omitempty"` // Optional: specific dataset
}

// DLQRetryResponse represents the response from DLQ retry operation
type DLQRetryResponse struct {
	Success      bool             `json:"success"`
	Message      string           `json:"message"`
	FilesRetried int              `json:"files_retried"`
	Details      []DLQRetryDetail `json:"details,omitempty"`
}

// DLQRetryDetail represents details of a single file retry operation
type DLQRetryDetail struct {
	FileID    string `json:"file_id"`
	TenantID  string `json:"tenant_id"`
	DatasetID string `json:"dataset_id"`
	Success   bool   `json:"success"`
	Error     string `json:"error,omitempty"`
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
			BaseURL: cfg.Receiver.BaseURL,
			Status:  receiverStatus,
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
			// Compression is always enabled for raw data
		}

		// Receiver configuration
		output.Receiver = ReceiverConfigMasked{
			BaseURL:       cfg.Receiver.BaseURL,
			TimeoutSec:    cfg.Receiver.TimeoutSec,
			RetryCount:    cfg.Receiver.RetryCount,
			RetryDelaySec: cfg.Receiver.RetryDelaySec,
		}

		// Global tenant ID and bearer token (masked)
		output.TenantID = maskSensitiveValue(cfg.TenantID)
		output.BearerToken = maskSensitiveValue(cfg.BearerToken)

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

// GetDLQStats returns DLQ and spooling statistics
func (api *API) GetDLQStats() usecase.Interactor {
	u := usecase.NewInteractor(func(ctx context.Context, input struct{}, output *DLQStatsResponse) error {
		if !api.Config.Spooling.Enabled {
			output.SpoolingEnabled = false
			output.SpoolDirectory = api.Config.Spooling.Directory
			output.TenantStats = make(map[string]*TenantDLQStats)
			return nil
		}

		output.SpoolingEnabled = true
		output.SpoolDirectory = api.Config.Spooling.Directory

		// Get DLQ statistics from spooling service
		dlqStats, err := api.Services.SpoolingService.GetDLQStats()
		if err != nil {
			log.Errorf("Failed to get DLQ statistics: %v", err)
			return err
		}

		// Populate response
		output.TotalFilesInQueue = dlqStats.TotalFilesInQueue
		output.TotalFilesInDLQ = dlqStats.TotalFilesInDLQ
		output.TotalBytesInQueue = dlqStats.TotalBytesInQueue
		output.TotalBytesInDLQ = dlqStats.TotalBytesInDLQ
		output.TenantStats = convertTenantDLQStats(dlqStats.TenantStats)

		// Convert oldest file info
		if dlqStats.OldestQueueFile != nil {
			output.OldestQueueFile = convertToFileInfo(dlqStats.OldestQueueFile)
		}
		if dlqStats.OldestDLQFile != nil {
			output.OldestDLQFile = convertToFileInfo(dlqStats.OldestDLQFile)
		}

		log.Debugf("Retrieved DLQ statistics: queue=%d dlq=%d", output.TotalFilesInQueue, output.TotalFilesInDLQ)
		return nil
	})

	u.SetTitle("Get DLQ Statistics")
	u.SetDescription("Retrieve statistics about queued files and dead letter queue")
	u.SetTags("DLQ")

	return u
}

// RetryDLQFiles moves DLQ files back to the processing queue
func (api *API) RetryDLQFiles() usecase.Interactor {
	u := usecase.NewInteractor(func(ctx context.Context, input *DLQRetryRequest, output *DLQRetryResponse) error {
		if !api.Config.Spooling.Enabled {
			output.Success = false
			output.Message = "Spooling is disabled"
			output.FilesRetried = 0
			return nil
		}

		// Perform DLQ retry operation
		retryResult, err := api.Services.SpoolingService.RetryDLQFiles(input.TenantID, input.DatasetID)
		if err != nil {
			output.Success = false
			output.Message = err.Error()
			output.FilesRetried = 0
			log.Errorf("Failed to retry DLQ files: %v", err)
			return nil // Don't return error, just populate failed response
		}

		output.Success = true
		output.FilesRetried = retryResult.FilesRetried
		output.Details = convertRetryDetails(retryResult.Details)

		if input.TenantID != "" && input.DatasetID != "" {
			output.Message = fmt.Sprintf("Successfully retried %d files for tenant=%s dataset=%s",
				retryResult.FilesRetried, input.TenantID, input.DatasetID)
		} else if input.TenantID != "" {
			output.Message = fmt.Sprintf("Successfully retried %d files for tenant=%s",
				retryResult.FilesRetried, input.TenantID)
		} else {
			output.Message = fmt.Sprintf("Successfully retried %d files from DLQ", retryResult.FilesRetried)
		}

		log.Infof("DLQ retry completed: %s", output.Message)
		return nil
	})

	u.SetTitle("Retry DLQ Files")
	u.SetDescription("Move files from Dead Letter Queue back to processing queue")
	u.SetTags("DLQ")

	return u
}

// convertTenantDLQStats converts service tenant stats to API format
func convertTenantDLQStats(serviceStats map[string]*services.TenantDLQStats) map[string]*TenantDLQStats {
	apiStats := make(map[string]*TenantDLQStats)
	for tenantID, stats := range serviceStats {
		apiStats[tenantID] = &TenantDLQStats{
			QueueFiles:   stats.QueueFiles,
			DLQFiles:     stats.DLQFiles,
			QueueBytes:   stats.QueueBytes,
			DLQBytes:     stats.DLQBytes,
			DatasetStats: convertDatasetDLQStats(stats.DatasetStats),
		}
	}
	return apiStats
}

// convertDatasetDLQStats converts service dataset stats to API format
func convertDatasetDLQStats(serviceStats map[string]*services.DatasetDLQStats) map[string]*DatasetDLQStats {
	apiStats := make(map[string]*DatasetDLQStats)
	for datasetID, stats := range serviceStats {
		apiStats[datasetID] = &DatasetDLQStats{
			QueueFiles: stats.QueueFiles,
			DLQFiles:   stats.DLQFiles,
			QueueBytes: stats.QueueBytes,
			DLQBytes:   stats.DLQBytes,
		}
	}
	return apiStats
}

// convertToFileInfo converts service SpooledFile to API FileInfo
func convertToFileInfo(spooledFile *services.SpooledFile) *FileInfo {
	if spooledFile == nil {
		return nil
	}
	return &FileInfo{
		ID:            spooledFile.ID,
		TenantID:      spooledFile.TenantID,
		DatasetID:     spooledFile.DatasetID,
		Size:          spooledFile.Size,
		CreatedAt:     spooledFile.CreatedAt,
		RetryCount:    spooledFile.RetryCount,
		FailureReason: spooledFile.FailureReason,
	}
}

// convertRetryDetails converts service retry details to API format
func convertRetryDetails(serviceDetails []services.DLQRetryDetail) []DLQRetryDetail {
	apiDetails := make([]DLQRetryDetail, len(serviceDetails))
	for i, detail := range serviceDetails {
		apiDetails[i] = DLQRetryDetail{
			FileID:    detail.FileID,
			TenantID:  detail.TenantID,
			DatasetID: detail.DatasetID,
			Success:   detail.Success,
			Error:     detail.Error,
		}
	}
	return apiDetails
}

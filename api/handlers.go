package api

import (
	"context"
	"fmt"
	"path/filepath"
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
	Plugins     PluginsHealthStatus  `json:"plugins"`
	Receiver    ReceiverHealthStatus `json:"receiver"`
	Stats       ProxyStatsResponse   `json:"stats"`
}

type PluginsHealthStatus struct {
	TotalPlugins  int                  `json:"total_plugins"`
	ActivePlugins int                  `json:"active_plugins"`
	PluginsByType map[string]int       `json:"plugins_by_type"`
	PluginDetails []PluginHealthDetail `json:"plugin_details"`
	Status        string               `json:"status"`
}

type PluginHealthDetail struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	Status    string `json:"status"`
	Port      int    `json:"port,omitempty"`
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
	Plugins      PluginsConfig        `json:"plugins"`
	Receiver     ReceiverConfigMasked `json:"receiver"`
	TenantID     string               `json:"tenant_id"`    // This will be masked
	BearerToken  string               `json:"bearer_token"` // This will be masked
	SOC          SOCConfig            `json:"soc"`
	Otel         OtelConfig           `json:"otel"`
	Housekeeping HousekeepingConfig   `json:"housekeeping"`
	Dev          bool                 `json:"dev"`
}

type AppConfig struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	GitCommit string `json:"git_commit"`
}

type ServerConfig struct {
	ApiPort int `json:"api_port"`
}

type PluginsConfig struct {
	TotalPlugins  int                  `json:"total_plugins"`
	PluginDetails []PluginConfigDetail `json:"plugin_details"`
}

type PluginConfigDetail struct {
	Name   string                 `json:"name"`
	Type   string                 `json:"type"`
	Config map[string]interface{} `json:"config"`
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
	ID               string    `json:"id"`
	TenantID         string    `json:"tenant_id"`
	DatasetID        string    `json:"dataset_id"`
	CompressedSize   int64     `json:"compressed_size"`
	UncompressedSize int64     `json:"uncompressed_size"`
	CreatedAt        time.Time `json:"created_at"`
	RetryCount       int       `json:"retry_count"`
	FailureReason    string    `json:"failure_reason,omitempty"`
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

// DLQListRequest represents a request to list DLQ files
type DLQListRequest struct {
	TenantID  string `query:"tenant_id" description:"Filter by tenant ID (optional)"`
	DatasetID string `query:"dataset_id" description:"Filter by dataset ID (optional)"`
}

// DLQListResponse represents the response for listing DLQ files
type DLQListResponse struct {
	SpoolingEnabled bool          `json:"spooling_enabled"`
	Files           []DLQFileInfo `json:"files"`
	Message         string        `json:"message"`
}

// DLQFileInfo represents information about a file in the DLQ
type DLQFileInfo struct {
	ID               string    `json:"id"`
	TenantID         string    `json:"tenant_id"`
	DatasetID        string    `json:"dataset_id"`
	Filename         string    `json:"filename"`
	CompressedSize   int64     `json:"compressed_size"`
	UncompressedSize int64     `json:"uncompressed_size"`
	LineCount        int       `json:"line_count"`
	CreatedAt        time.Time `json:"created_at"`
	LastRetry        time.Time `json:"last_retry"`
	RetryCount       int       `json:"retry_count"`
	Status           string    `json:"status"`
	FailureReason    string    `json:"failure_reason,omitempty"`
	TriggerReason string    `json:"trigger_reason,omitempty"`
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

		// Plugin status
		pluginStatus := "no_plugins"
		pluginsByType := make(map[string]int)
		var pluginDetails []PluginHealthDetail

		totalPlugins := len(cfg.Inputs)
		activePlugins := 0

		for _, input := range cfg.Inputs {
			pluginsByType[input.Type]++
			activePlugins++ // For now, assume all configured plugins are active

			// Extract port from config if available
			port := 0
			if portVal, ok := input.Config["port"]; ok {
				if portInt, ok := portVal.(int); ok {
					port = portInt
				}
			}

			// Extract dataset_id and tenant_id from config
			datasetID := ""
			if datasetVal, ok := input.Config["dataset_id"]; ok {
				if datasetStr, ok := datasetVal.(string); ok {
					datasetID = datasetStr
				}
			}

			tenantID := ""
			if tenantVal, ok := input.Config["tenant_id"]; ok {
				if tenantStr, ok := tenantVal.(string); ok {
					tenantID = tenantStr
				}
			}
			// Use global tenant as fallback if plugin doesn't specify its own
			if tenantID == "" {
				tenantID = cfg.TenantID
			}

			pluginDetails = append(pluginDetails, PluginHealthDetail{
				Name:      input.Name,
				Type:      input.Type,
				Status:    "active", // TODO: Get actual plugin health status
				Port:      port,
				DatasetID: datasetID,
				TenantID:  tenantID,
			})
		}

		if totalPlugins > 0 {
			pluginStatus = "active"
		}

		output.Plugins = PluginsHealthStatus{
			TotalPlugins:  totalPlugins,
			ActivePlugins: activePlugins,
			PluginsByType: pluginsByType,
			PluginDetails: pluginDetails,
			Status:        pluginStatus,
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
			Name:      cfg.App.Name,
			Version:   cfg.App.Version,
			GitCommit: cfg.App.GitCommit,
		}

		// Server configuration
		output.Server = ServerConfig{
			ApiPort: cfg.Server.ApiPort,
		}

		// Plugin configuration
		var pluginDetails []PluginConfigDetail
		for _, input := range cfg.Inputs {
			// Create a sanitized copy of the config (mask sensitive values)
			sanitizedConfig := make(map[string]interface{})
			for key, value := range input.Config {
				// Mask sensitive fields
				if key == "bearer_token" || key == "password" || key == "secret" || key == "token" {
					sanitizedConfig[key] = "***MASKED***"
				} else {
					sanitizedConfig[key] = value
				}
			}

			pluginDetails = append(pluginDetails, PluginConfigDetail{
				Name:   input.Name,
				Type:   input.Type,
				Config: sanitizedConfig,
			})
		}

		output.Plugins = PluginsConfig{
			TotalPlugins:  len(cfg.Inputs),
			PluginDetails: pluginDetails,
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

// ListDLQFiles returns a list of files in the DLQ
func (api *API) ListDLQFiles() usecase.Interactor {
	u := usecase.NewInteractor(func(ctx context.Context, input *DLQListRequest, output *DLQListResponse) error {
		log.Info("Listing DLQ files")

		if !api.Config.Spooling.Enabled {
			output.SpoolingEnabled = false
			output.Files = []DLQFileInfo{}
			output.Message = "Spooling is disabled"
			return nil
		}

		output.SpoolingEnabled = true

		// Get DLQ file list from spooling service
		files, err := api.Services.SpoolingService.ListDLQFiles(input.TenantID, input.DatasetID)
		if err != nil {
			log.Errorf("Failed to list DLQ files: %v", err)
			return fmt.Errorf("failed to list DLQ files: %w", err)
		}

		// Convert to API response format
		output.Files = make([]DLQFileInfo, len(files))
		for i, file := range files {
			output.Files[i] = DLQFileInfo{
				ID:               file.ID,
				TenantID:         file.TenantID,
				DatasetID:        file.DatasetID,
				Filename:         filepath.Base(file.Filename),
				CompressedSize:   file.CompressedSize,
				UncompressedSize: file.UncompressedSize,
				LineCount:        file.LineCount,
				CreatedAt:        file.CreatedAt,
				LastRetry:        file.LastRetry,
				RetryCount:       file.RetryCount,
				Status:           file.Status,
				FailureReason:    file.FailureReason,
				TriggerReason:    file.TriggerReason,
			}
		}

		// Set response message
		if len(files) == 0 {
			if input.TenantID != "" && input.DatasetID != "" {
				output.Message = fmt.Sprintf("No files in DLQ for tenant=%s dataset=%s", input.TenantID, input.DatasetID)
			} else if input.TenantID != "" {
				output.Message = fmt.Sprintf("No files in DLQ for tenant=%s", input.TenantID)
			} else {
				output.Message = "No files in DLQ"
			}
		} else {
			// Check if we hit the limit (indicates there may be more files)
			const maxFiles = 100 // Must match limit in spooling service
			fileCountStr := fmt.Sprintf("%d", len(files))
			if len(files) == maxFiles {
				fileCountStr = fmt.Sprintf("%d+", maxFiles)
			}

			if input.TenantID != "" && input.DatasetID != "" {
				output.Message = fmt.Sprintf("Found %s files in DLQ for tenant=%s dataset=%s", fileCountStr, input.TenantID, input.DatasetID)
			} else if input.TenantID != "" {
				output.Message = fmt.Sprintf("Found %s files in DLQ for tenant=%s", fileCountStr, input.TenantID)
			} else {
				output.Message = fmt.Sprintf("Found %s files in DLQ", fileCountStr)
			}
		}

		log.Infof("DLQ file listing completed: %s", output.Message)
		return nil
	})

	u.SetTitle("List DLQ Files")
	u.SetDescription("List all files in the Dead Letter Queue, optionally filtered by tenant and dataset")
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
		ID:               spooledFile.ID,
		TenantID:         spooledFile.TenantID,
		DatasetID:        spooledFile.DatasetID,
		CompressedSize:   spooledFile.CompressedSize,
		UncompressedSize: spooledFile.UncompressedSize,
		CreatedAt:        spooledFile.CreatedAt,
		RetryCount:       spooledFile.RetryCount,
		FailureReason:    spooledFile.FailureReason,
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

// ConnectivityTestRequest represents a request to test connectivity
type ConnectivityTestRequest struct {
	// No fields needed - auto-discovers all configurations
}

// ConnectivityTestResponse represents the response from connectivity tests
type ConnectivityTestResponse struct {
	Message string                         `json:"message"`
	Results []ConnectivityTestResultAPI   `json:"results"`
	Summary ConnectivityTestSummary       `json:"summary"`
}

// ConnectivityTestResultAPI represents a single connectivity test result
type ConnectivityTestResultAPI struct {
	TenantID     string `json:"tenant_id"`
	DatasetID    string `json:"dataset_id"`
	PluginName   string `json:"plugin_name"`
	PluginType   string `json:"plugin_type"`
	BearerToken  string `json:"bearer_token,omitempty"`
	ReceiverURL  string `json:"receiver_url"`
	Status       string `json:"status"`
	StatusCode   int    `json:"status_code,omitempty"`
	ResponseTime string `json:"response_time"`
	ErrorMessage string `json:"error_message,omitempty"`
	ResponseBody string `json:"response_body,omitempty"`
}

// ConnectivityTestSummary represents a summary of connectivity test results
type ConnectivityTestSummary struct {
	TotalTests    int `json:"total_tests"`
	SuccessCount  int `json:"success_count"`
	FailureCount  int `json:"failure_count"`
	ErrorCount    int `json:"error_count"`
	SuccessRate   int `json:"success_rate_percent"`
}

// TestConnectivity tests connectivity to receiver for all configured plugins/tenants
func (api *API) TestConnectivity() usecase.Interactor {
	u := usecase.NewInteractor(func(ctx context.Context, input *ConnectivityTestRequest, output *ConnectivityTestResponse) error {
		log.Info("Starting automatic connectivity tests for all configured plugins/tenants/datasets")

		// Create connectivity test service
		connectivityService := services.NewConnectivityTestService(api.Config)

		// Test all configured connections automatically
		results, err := connectivityService.TestAllConnections()
		if err != nil {
			log.Errorf("Failed to test connectivity: %v", err)
			return fmt.Errorf("failed to test connectivity: %w", err)
		}

		// Convert results to API format
		output.Results = make([]ConnectivityTestResultAPI, len(results))
		for i, result := range results {
			output.Results[i] = ConnectivityTestResultAPI{
				TenantID:     result.TenantID,
				DatasetID:    result.DatasetID,
				PluginName:   result.PluginName,
				PluginType:   result.PluginType,
				BearerToken:  result.BearerToken,
				ReceiverURL:  result.ReceiverURL,
				Status:       result.Status,
				StatusCode:   result.StatusCode,
				ResponseTime: result.ResponseTime,
				ErrorMessage: result.ErrorMessage,
				ResponseBody: result.ResponseBody,
			}
		}

		// Calculate summary
		summary := ConnectivityTestSummary{
			TotalTests: len(results),
		}
		for _, result := range results {
			switch result.Status {
			case "success":
				summary.SuccessCount++
			case "failed":
				summary.FailureCount++
			case "error":
				summary.ErrorCount++
			}
		}
		if summary.TotalTests > 0 {
			summary.SuccessRate = (summary.SuccessCount * 100) / summary.TotalTests
		}
		output.Summary = summary

		// Generate message
		output.Message = fmt.Sprintf("Connectivity tests completed for all %d configured plugins/tenants/datasets", summary.TotalTests)

		log.Infof("Connectivity tests completed: %d total, %d success, %d failed, %d errors (%d%% success rate)",
			summary.TotalTests, summary.SuccessCount, summary.FailureCount, summary.ErrorCount, summary.SuccessRate)

		return nil
	})

	u.SetTitle("Test Connectivity to Receiver")
	u.SetDescription("Automatically tests connectivity to bytefreezer-receiver for all configured plugins/tenants/datasets")
	u.SetTags("Connectivity")

	return u
}

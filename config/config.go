package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
	"github.com/n0needt0/go-goodies/log"
	pkgerrors "github.com/pkg/errors"

	"github.com/n0needt0/bytefreezer-proxy/alerts"
	"github.com/n0needt0/bytefreezer-proxy/errors"
	"github.com/n0needt0/bytefreezer-proxy/plugins"
)

var k = koanf.New(".")

type Config struct {
	App              App                    `mapstructure:"app"`
	Logging          LoggingConfig          `mapstructure:"logging"`
	Server           Server                 `mapstructure:"server"`
	Inputs           []plugins.PluginConfig `mapstructure:"inputs"` // Plugin-based input system
	Batching         Batching               `mapstructure:"batching"`
	Receiver         Receiver               `mapstructure:"receiver"`
	AccountID        string                 `mapstructure:"account_id"` // Account ID for multi-tenant polling
	TenantID         string                 `mapstructure:"tenant_id"`  // Global tenant ID (fallback for plugins without tenant_id)
	BearerToken      string                 `mapstructure:"bearer_token"` // Account-specific API key for authentication
	ControlURL       string                 `mapstructure:"control_url"`  // Centralized control service URL
	ConfigMode       string                 `mapstructure:"config_mode"`  // local-only | control-only
	ConfigPolling    ConfigPollingConfig    `mapstructure:"config_polling"`
	SOC              SOCAlert               `mapstructure:"soc"`
	Otel             Otel                   `mapstructure:"otel"`
	Spooling         Spooling               `mapstructure:"spooling"`
	HealthReporting  HealthReportingConfig  `mapstructure:"health_reporting"`
	ErrorTracking    ErrorTrackingConfig    `mapstructure:"error_tracking"`
	TenantValidation TenantValidationConfig `mapstructure:"tenant_validation"`
	Dev              bool                   `mapstructure:"dev"`

	// Runtime components
	SOCAlertClient       *alerts.SOCAlertClient    `mapstructure:"-"`
	ErrorReporter        *errors.ErrorReporter     `mapstructure:"-"`
	DatasetMetricsClient interface{}               `mapstructure:"-"` // services.DatasetMetricsClient (interface to avoid circular import)
}

// ErrorTrackingConfig represents error tracking configuration
type ErrorTrackingConfig struct {
	Enabled bool `mapstructure:"enabled"`
}

type App struct {
	Name      string `mapstructure:"name"`
	Version   string `mapstructure:"version"`
	GitCommit string `mapstructure:"-"` // Not loaded from config, set at runtime
}

type LoggingConfig struct {
	Level    string `mapstructure:"level"`
	Encoding string `mapstructure:"encoding"`
}

type Server struct {
	ApiPort int `mapstructure:"api_port"`
}

type Batching struct {
	Enabled            bool  `mapstructure:"enabled"`
	MaxLines           int   `mapstructure:"max_lines"`
	MaxBytes           int64 `mapstructure:"max_bytes"`
	TimeoutSeconds     int   `mapstructure:"timeout_seconds"`
	CompressionEnabled bool  `mapstructure:"compression_enabled"`
	CompressionLevel   int   `mapstructure:"compression_level"`
}

type Receiver struct {
	BaseURL            string `mapstructure:"base_url"`
	TimeoutSec         int    `mapstructure:"timeout_seconds"`
	UploadWorkerCount  int    `mapstructure:"upload_worker_count"`  // Number of upload workers (aligned with receiver)
	MaxIdleConns       int    `mapstructure:"max_idle_conns"`       // HTTP connection pool size
	MaxConnsPerHost    int    `mapstructure:"max_conns_per_host"`   // Max connections per host

	// Dedicated retry HTTP client configuration (same defaults as upload client)
	RetryMaxIdleConns    int `mapstructure:"retry_max_idle_conns"`    // HTTP connection pool size for retries
	RetryMaxConnsPerHost int `mapstructure:"retry_max_conns_per_host"` // Max connections per host for retries
}

type SOCAlert struct {
	Enabled  bool   `mapstructure:"enabled"`
	Endpoint string `mapstructure:"endpoint"`
	Timeout  int    `mapstructure:"timeout"`
}

type Otel struct {
	Enabled               bool   `mapstructure:"enabled"`
	Endpoint              string `mapstructure:"endpoint"`
	ServiceName           string `mapstructure:"service_name"`
	ScrapeIntervalSeconds int    `mapstructure:"scrapeIntervalseconds"`
	PrometheusMode        bool   `mapstructure:"prometheus_mode"`
	MetricsPort           int    `mapstructure:"metrics_port"`
	MetricsHost           string `mapstructure:"metrics_host"`
}


type ConfigPollingConfig struct {
	Enabled         bool   `mapstructure:"enabled"`
	IntervalSeconds int    `mapstructure:"interval_seconds"`
	TimeoutSeconds  int    `mapstructure:"timeout_seconds"`
	CacheDirectory  string `mapstructure:"cache_directory"` // Directory to cache config from control
	RetryOnError    bool   `mapstructure:"retry_on_error"`  // Keep trying if control unreachable
}

type Spooling struct {
	Enabled            bool   `mapstructure:"enabled"`
	Directory          string `mapstructure:"directory"`
	MaxSizeBytes       int64  `mapstructure:"max_size_bytes"`
	RetryAttempts      int    `mapstructure:"retry_attempts"`
	RetryIntervalSec   int    `mapstructure:"retry_interval_seconds"`
	CleanupIntervalSec int    `mapstructure:"cleanup_interval_seconds"`
	KeepSrc                      bool `mapstructure:"keep_src"`
	QueueProcessingIntervalSec   int  `mapstructure:"queue_processing_interval_seconds"`

	// Organization settings
	Organization       string `mapstructure:"organization"`          // "flat", "tenant_dataset", "date_tenant", "protocol_tenant"
	PerTenantLimits    bool   `mapstructure:"per_tenant_limits"`     // Apply size limits per tenant instead of globally
	MaxFilesPerDataset int    `mapstructure:"max_files_per_dataset"` // Max files per dataset (0 = unlimited)
	MaxAgeDays         int    `mapstructure:"max_age_days"`          // Max age in days before cleanup (0 = unlimited)
}

type HealthReportingConfig struct {
	Enabled            bool   `mapstructure:"enabled"`
	ReportInterval     int    `mapstructure:"report_interval"`
	TimeoutSeconds     int    `mapstructure:"timeout_seconds"`
	RegisterOnStartup  bool   `mapstructure:"register_on_startup"`
}

type TenantValidationConfig struct {
	Enabled            bool   `mapstructure:"enabled"`
	CacheTTLSec        int    `mapstructure:"cache_ttl_seconds"`         // TTL for active tenants
	InactiveCacheTTLSec int    `mapstructure:"inactive_cache_ttl_seconds"` // TTL for inactive tenants (longer)
	TimeoutSeconds     int    `mapstructure:"timeout_seconds"`
}

func LoadConfig(cfgFile, envPrefix string, cfg *Config) error {
	if cfgFile == "" {
		cfgFile = "config.yaml"
	}

	err := k.Load(file.Provider(cfgFile), yaml.Parser())
	if err != nil {
		return pkgerrors.Wrapf(err, "failed to parse %s", cfgFile)
	}

	if err := k.Load(env.Provider(envPrefix, ".", func(s string) string {
		return strings.Replace(strings.ToLower(strings.TrimPrefix(s, envPrefix)), "_", ".", -1)
	}), nil); err != nil {
		return pkgerrors.Wrapf(err, "error loading config from env")
	}

	err = k.UnmarshalWithConf("", &cfg, koanf.UnmarshalConf{Tag: "mapstructure"})
	if err != nil {
		return pkgerrors.Wrapf(err, "failed to unmarshal %s", cfgFile)
	}

	// Validate plugin inputs and identifiers
	if err := validateIdentifiers(cfg); err != nil {
		return pkgerrors.Wrapf(err, "identifier validation failed")
	}

	// Batching defaults
	if cfg.Batching.TimeoutSeconds == 0 {
		cfg.Batching.TimeoutSeconds = 30
	}
	if cfg.Batching.MaxBytes == 0 {
		cfg.Batching.MaxBytes = 1048576 // 1MB
	}
	if !cfg.Batching.CompressionEnabled {
		cfg.Batching.CompressionEnabled = true
	}
	if cfg.Batching.CompressionLevel == 0 {
		cfg.Batching.CompressionLevel = 6
	}

	if cfg.Receiver.TimeoutSec == 0 {
		cfg.Receiver.TimeoutSec = 30
	}
	// RetryCount and RetryDelaySec are deprecated - file-level retry is used instead
	// Keep RetryCount = 0 for single HTTP attempts only

	// Spooling defaults
	if cfg.Spooling.Directory == "" {
		cfg.Spooling.Directory = "/var/spool/bytefreezer-proxy"
	}
	if cfg.Spooling.MaxSizeBytes == 0 {
		cfg.Spooling.MaxSizeBytes = 1073741824 // 1GB default
	}
	if cfg.Spooling.RetryAttempts == 0 {
		cfg.Spooling.RetryAttempts = 5
	}
	if cfg.Spooling.RetryIntervalSec == 0 {
		cfg.Spooling.RetryIntervalSec = 60 // 1 minute
	}
	if cfg.Spooling.CleanupIntervalSec == 0 {
		cfg.Spooling.CleanupIntervalSec = 300 // 5 minutes
	}

	// Config mode defaults
	if cfg.ConfigMode == "" {
		cfg.ConfigMode = "control-only" // Default to control-only mode with local fallback
	}

	// Validate config mode
	validModes := map[string]bool{
		"local-only":   true,
		"control-only": true,
	}
	if !validModes[cfg.ConfigMode] {
		return fmt.Errorf("invalid config_mode '%s': must be 'local-only' or 'control-only'", cfg.ConfigMode)
	}

	// Config polling defaults
	if cfg.ConfigPolling.IntervalSeconds == 0 {
		cfg.ConfigPolling.IntervalSeconds = 300 // 5 minutes default
	}
	if cfg.ConfigPolling.TimeoutSeconds == 0 {
		cfg.ConfigPolling.TimeoutSeconds = 30 // 30 seconds default
	}
	if cfg.ConfigPolling.CacheDirectory == "" {
		cfg.ConfigPolling.CacheDirectory = "/var/cache/bytefreezer-proxy"
	}

	// Enable polling by default for control-only mode
	if cfg.ConfigMode == "control-only" && cfg.ControlURL != "" {
		if !cfg.ConfigPolling.Enabled {
			cfg.ConfigPolling.Enabled = true // Auto-enable for control-based mode
		}
	}

	return nil
}

func (cfg *Config) InitializeComponents() error {
	// Initialize error reporter if configured
	if cfg.ErrorTracking.Enabled && cfg.ControlURL != "" {
		cfg.ErrorReporter = errors.NewErrorReporter(
			cfg.ControlURL,
			cfg.AccountID,
			cfg.BearerToken,
			"proxy",
			true,
		)
		log.Infof("Error reporter initialized - reporting to control service at %s", cfg.ControlURL)
	} else {
		log.Infof("Error reporting disabled (enabled: %v, control_url: %s)", cfg.ErrorTracking.Enabled, cfg.ControlURL)
	}

	// Initialize SOC alert client
	cfg.SOCAlertClient = alerts.NewSOCAlertClient(alerts.AlertClientConfig{
		SOC: alerts.SOCConfig{
			Enabled:  cfg.SOC.Enabled,
			Endpoint: cfg.SOC.Endpoint,
			Timeout:  cfg.SOC.Timeout,
		},
		App: alerts.AppConfig{
			Name:    cfg.App.Name,
			Version: cfg.App.Version,
		},
		Dev: cfg.Dev,
	})

	return nil
}

func (cfg *Config) GetReceiverTimeout() time.Duration {
	return time.Duration(cfg.Receiver.TimeoutSec) * time.Second
}

// GetRetryDelay is deprecated - HTTP-level retries are disabled in favor of file-level retries

func (cfg *Config) GetUploadWorkerCount() int {
	if cfg.Receiver.UploadWorkerCount <= 0 {
		return 5 // Default to 5 upload workers (aligned with receiver)
	}
	return cfg.Receiver.UploadWorkerCount
}

func (cfg *Config) GetMaxIdleConns() int {
	if cfg.Receiver.MaxIdleConns <= 0 {
		return 10 // Default to 10 idle connections
	}
	return cfg.Receiver.MaxIdleConns
}

func (cfg *Config) GetMaxConnsPerHost() int {
	if cfg.Receiver.MaxConnsPerHost <= 0 {
		return 6 // Default to 6 connections per host
	}
	return cfg.Receiver.MaxConnsPerHost
}

// GetRetryMaxIdleConns returns the max idle connections for retry HTTP client
func (cfg *Config) GetRetryMaxIdleConns() int {
	if cfg.Receiver.RetryMaxIdleConns <= 0 {
		return cfg.GetMaxIdleConns() // Default to same as upload client (10)
	}
	return cfg.Receiver.RetryMaxIdleConns
}

// GetRetryMaxConnsPerHost returns the max connections per host for retry HTTP client
func (cfg *Config) GetRetryMaxConnsPerHost() int {
	if cfg.Receiver.RetryMaxConnsPerHost <= 0 {
		return cfg.GetMaxConnsPerHost() // Default to same as upload client (6)
	}
	return cfg.Receiver.RetryMaxConnsPerHost
}

// GetQueueProcessingInterval returns the queue processing interval with default fallback
func (cfg *Config) GetQueueProcessingInterval() int {
	if cfg.Spooling.QueueProcessingIntervalSec <= 0 {
		return 30 // Default to 30 seconds for backward compatibility
	}
	return cfg.Spooling.QueueProcessingIntervalSec
}

// validateIdentifiers validates all tenant and dataset identifiers in the configuration
func validateIdentifiers(cfg *Config) error {
	// tenant_id is optional when using account-based polling
	// tenant_id is required as global fallback when NOT using account-based polling
	if cfg.AccountID == "" {
		// Not using account-based polling - tenant_id is required
		if cfg.TenantID == "" {
			return fmt.Errorf("tenant_id is required when account_id is not configured")
		}
		if err := ValidateTenantID(cfg.TenantID); err != nil {
			return fmt.Errorf("global tenant_id: %w", err)
		}
	} else {
		// Using account-based polling - tenant_id is optional but validate if present
		if cfg.TenantID != "" {
			if err := ValidateTenantID(cfg.TenantID); err != nil {
				return fmt.Errorf("global tenant_id: %w", err)
			}
		}
	}

	// Validate plugin inputs for duplicate dataset names and port conflicts
	if err := validatePluginInputs(cfg); err != nil {
		return fmt.Errorf("plugin input validation failed: %w", err)
	}

	return nil
}

// ValidateConfig validates the entire configuration including plugin inputs and port conflicts
func ValidateConfig(cfg *Config) error {
	return validateIdentifiers(cfg)
}

// validatePluginInputs validates plugin inputs for duplicate dataset names and other conflicts
func validatePluginInputs(cfg *Config) error {
	if len(cfg.Inputs) == 0 {
		return nil // No plugin inputs to validate
	}

	// Track tenant-dataset combinations to detect duplicates
	tenantDatasetMap := make(map[string]map[string]string) // tenantID -> datasetID -> pluginType

	// Track port usage to prevent conflicts
	portMap := make(map[int]string) // port -> "pluginType[pluginName] for dataset_id"

	for i, input := range cfg.Inputs {
		// Get effective tenant ID
		var effectiveTenantID string
		if tenantIDValue, exists := input.Config["tenant_id"]; exists {
			if tenantIDStr, ok := tenantIDValue.(string); ok && tenantIDStr != "" {
				effectiveTenantID = tenantIDStr
			}
		}
		if effectiveTenantID == "" {
			effectiveTenantID = cfg.TenantID // Fallback to global tenant
		}

		// Get dataset ID
		var datasetID string
		if datasetIDValue, exists := input.Config["dataset_id"]; exists {
			if datasetIDStr, ok := datasetIDValue.(string); ok && datasetIDStr != "" {
				datasetID = datasetIDStr
			}
		}

		if datasetID == "" {
			return fmt.Errorf("plugin input %d (%s): dataset_id is required", i, input.Type)
		}

		// Validate identifiers
		if effectiveTenantID == "" {
			return fmt.Errorf("plugin input %d (%s): tenant_id is required in plugin config or as global tenant_id", i, input.Type)
		}
		if err := ValidateTenantID(effectiveTenantID); err != nil {
			return fmt.Errorf("plugin input %d (%s) tenant_id: %w", i, input.Type, err)
		}
		if err := ValidateDatasetID(datasetID); err != nil {
			return fmt.Errorf("plugin input %d (%s) dataset_id: %w", i, input.Type, err)
		}
		if err := ValidateIdentifierPair(effectiveTenantID, datasetID); err != nil {
			return fmt.Errorf("plugin input %d (%s): %w", i, input.Type, err)
		}

		// Check for port conflicts (only for plugins that use ports)
		if portValue, exists := input.Config["port"]; exists {
			var port int
			switch p := portValue.(type) {
			case int:
				port = p
			case float64:
				port = int(p)
			default:
				return fmt.Errorf("plugin input %d (%s): port must be a number", i, input.Type)
			}

			if port <= 0 || port > 65535 {
				return fmt.Errorf("plugin input %d (%s): port %d is invalid (must be between 1-65535)", i, input.Type, port)
			}

			if existingUser, exists := portMap[port]; exists {
				return fmt.Errorf("plugin input %d (%s): port %d is already in use by %s. Each port can only be used once to prevent conflicts",
					i, input.Type, port, existingUser)
			}

			// Record this port usage
			portMap[port] = fmt.Sprintf("%s[%s] for dataset_id=%s", input.Type, input.Name, datasetID)
		}

		// Initialize tenant map if needed
		if tenantDatasetMap[effectiveTenantID] == nil {
			tenantDatasetMap[effectiveTenantID] = make(map[string]string)
		}

		// Check for duplicate dataset within same tenant
		if existingType, exists := tenantDatasetMap[effectiveTenantID][datasetID]; exists {
			return fmt.Errorf("plugin input %d (%s): dataset_id '%s' for tenant '%s' is already configured with plugin type '%s'. Each dataset must be unique within a tenant to prevent data mixing and cross-injection",
				i, input.Type, datasetID, effectiveTenantID, existingType)
		}

		// Record this tenant-dataset combination
		tenantDatasetMap[effectiveTenantID][datasetID] = input.Type
	}

	return nil
}

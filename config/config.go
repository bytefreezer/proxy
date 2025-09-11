package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
	"github.com/pkg/errors"

	"github.com/n0needt0/bytefreezer-proxy/alerts"
	"github.com/n0needt0/go-goodies/log"
)

var k = koanf.New(".")

type Config struct {
	App          App           `mapstructure:"app"`
	Logging      LoggingConfig `mapstructure:"logging"`
	Server       Server        `mapstructure:"server"`
	UDP          UDP           `mapstructure:"udp"`
	Receiver     Receiver      `mapstructure:"receiver"`
	TenantID     string        `mapstructure:"tenant_id"`
	BearerToken  string        `mapstructure:"bearer_token"`
	SOC          SOCAlert      `mapstructure:"soc"`
	Otel         Otel          `mapstructure:"otel"`
	Housekeeping Housekeeping  `mapstructure:"housekeeping"`
	Spooling     Spooling      `mapstructure:"spooling"`
	Dev          bool          `mapstructure:"dev"`

	// Runtime components
	SOCAlertClient *alerts.SOCAlertClient `mapstructure:"-"`
}

type App struct {
	Name    string `mapstructure:"name"`
	Version string `mapstructure:"version"`
}

type LoggingConfig struct {
	Level    string `mapstructure:"level"`
	Encoding string `mapstructure:"encoding"`
}

type Server struct {
	ApiPort int `mapstructure:"api_port"`
}

type UDP struct {
	Enabled             bool          `mapstructure:"enabled"`
	Host                string        `mapstructure:"host"`
	ReadBufferSizeBytes int           `mapstructure:"read_buffer_size_bytes"`
	MaxBatchLines       int           `mapstructure:"max_batch_lines"`
	MaxBatchBytes       int64         `mapstructure:"max_batch_bytes"`
	BatchTimeoutSeconds int           `mapstructure:"batch_timeout_seconds"`
	CompressionLevel    int           `mapstructure:"compression_level"`
	EnableCompression   bool          `mapstructure:"enable_compression"`
	ChannelBufferSize   int           `mapstructure:"channel_buffer_size"` // Buffer size for UDP message channel
	WorkerCount         int           `mapstructure:"worker_count"`        // Number of worker goroutines for processing
	Listeners           []UDPListener `mapstructure:"listeners"`
}

type UDPListener struct {
	Port        int    `mapstructure:"port"`
	DatasetID   string `mapstructure:"dataset_id"`
	TenantID    string `mapstructure:"tenant_id,omitempty"`    // Optional: override global tenant
	BearerToken string `mapstructure:"bearer_token,omitempty"` // Optional: override global bearer token
	Protocol    string `mapstructure:"protocol,omitempty"`     // "udp" (default) or "syslog"
	SyslogMode  string `mapstructure:"syslog_mode,omitempty"`  // "rfc3164" (default) or "rfc5424"
}

type Receiver struct {
	BaseURL       string `mapstructure:"base_url"`
	TimeoutSec    int    `mapstructure:"timeout_seconds"`
	RetryCount    int    `mapstructure:"retry_count"`
	RetryDelaySec int    `mapstructure:"retry_delay_seconds"`
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

type Housekeeping struct {
	Enabled         bool `mapstructure:"enabled"`
	IntervalSeconds int  `mapstructure:"intervalseconds"`
}

type Spooling struct {
	Enabled            bool   `mapstructure:"enabled"`
	Directory          string `mapstructure:"directory"`
	MaxSizeBytes       int64  `mapstructure:"max_size_bytes"`
	RetryAttempts      int    `mapstructure:"retry_attempts"`
	RetryIntervalSec   int    `mapstructure:"retry_interval_seconds"`
	CleanupIntervalSec int    `mapstructure:"cleanup_interval_seconds"`

	// Organization settings
	Organization       string `mapstructure:"organization"`          // "flat", "tenant_dataset", "date_tenant", "protocol_tenant"
	PerTenantLimits    bool   `mapstructure:"per_tenant_limits"`     // Apply size limits per tenant instead of globally
	MaxFilesPerDataset int    `mapstructure:"max_files_per_dataset"` // Max files per dataset (0 = unlimited)
	MaxAgeDays         int    `mapstructure:"max_age_days"`          // Max age in days before cleanup (0 = unlimited)
}

func LoadConfig(cfgFile, envPrefix string, cfg *Config) error {
	if cfgFile == "" {
		cfgFile = "config.yaml"
	}

	err := k.Load(file.Provider(cfgFile), yaml.Parser())
	if err != nil {
		return errors.Wrapf(err, "failed to parse %s", cfgFile)
	}

	if err := k.Load(env.Provider(envPrefix, ".", func(s string) string {
		return strings.Replace(strings.ToLower(strings.TrimPrefix(s, envPrefix)), "_", ".", -1)
	}), nil); err != nil {
		return errors.Wrapf(err, "error loading config from env")
	}

	err = k.UnmarshalWithConf("", &cfg, koanf.UnmarshalConf{Tag: "mapstructure"})
	if err != nil {
		return errors.Wrapf(err, "failed to unmarshal %s", cfgFile)
	}

	// Validate multi-tenant configuration
	if err := validateMultiTenantConfig(cfg); err != nil {
		return errors.Wrapf(err, "multi-tenant configuration validation failed")
	}

	// Set defaults
	if cfg.UDP.BatchTimeoutSeconds == 0 {
		cfg.UDP.BatchTimeoutSeconds = 30
	}
	if cfg.Receiver.TimeoutSec == 0 {
		cfg.Receiver.TimeoutSec = 30
	}
	if cfg.Receiver.RetryCount == 0 {
		cfg.Receiver.RetryCount = 3
	}
	if cfg.Receiver.RetryDelaySec == 0 {
		cfg.Receiver.RetryDelaySec = 1
	}
	if cfg.UDP.ReadBufferSizeBytes == 0 {
		cfg.UDP.ReadBufferSizeBytes = 65536 // 64KB default
	}
	if cfg.UDP.CompressionLevel == 0 {
		cfg.UDP.CompressionLevel = 6 // Default gzip compression level
	}
	if cfg.UDP.ChannelBufferSize == 0 {
		cfg.UDP.ChannelBufferSize = 10000 // Default channel buffer size (10x larger)
	}
	if cfg.UDP.WorkerCount == 0 {
		cfg.UDP.WorkerCount = 4 // Default number of worker goroutines
	}

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

	return nil
}

func (cfg *Config) InitializeComponents() error {
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

func (cfg *Config) GetRetryDelay() time.Duration {
	return time.Duration(cfg.Receiver.RetryDelaySec) * time.Second
}

func (cfg *Config) GetBatchTimeout() time.Duration {
	return time.Duration(cfg.UDP.BatchTimeoutSeconds) * time.Second
}

// TenantInfo represents tenant configuration details
type TenantInfo struct {
	TenantID  string            `json:"tenant_id"`
	Ports     []int             `json:"ports"`
	Datasets  map[string]string `json:"datasets"` // dataset_id -> protocol
	PortCount int               `json:"port_count"`
}

// validateMultiTenantConfig validates the multi-tenant UDP listener configuration
func validateMultiTenantConfig(cfg *Config) error {
	if !cfg.UDP.Enabled {
		return nil // Skip validation if UDP is disabled
	}

	portMap := make(map[int]bool)
	tenantMap := make(map[string]*TenantInfo)

	for i, listener := range cfg.UDP.Listeners {
		// Validate required fields
		if listener.Port <= 0 || listener.Port > 65535 {
			return fmt.Errorf("listener %d: invalid port %d (must be 1-65535)", i, listener.Port)
		}

		if listener.DatasetID == "" {
			return fmt.Errorf("listener %d (port %d): dataset_id is required", i, listener.Port)
		}

		// Check for port conflicts
		if portMap[listener.Port] {
			return fmt.Errorf("listener %d (port %d): port already in use", i, listener.Port)
		}
		portMap[listener.Port] = true

		// Determine effective tenant ID
		tenantID := cfg.GetEffectiveTenantID(listener)
		if tenantID == "" {
			return fmt.Errorf("listener %d (port %d): tenant_id must be specified either per-listener or globally", i, listener.Port)
		}

		// Validate bearer token is available
		bearerToken := cfg.GetEffectiveBearerToken(listener)
		if bearerToken == "" {
			return fmt.Errorf("listener %d (port %d): bearer_token must be specified either per-listener or globally", i, listener.Port)
		}

		// Validate protocol
		protocol := listener.Protocol
		if protocol == "" {
			protocol = "udp"
		}
		validProtocols := map[string]bool{
			"udp": true, "syslog": true, "netflow": true, "sflow": true,
		}
		if !validProtocols[protocol] {
			return fmt.Errorf("listener %d (port %d): invalid protocol '%s' (supported: udp, syslog, netflow, sflow)", i, listener.Port, protocol)
		}

		// Build tenant information
		if tenantMap[tenantID] == nil {
			tenantMap[tenantID] = &TenantInfo{
				TenantID: tenantID,
				Ports:    []int{},
				Datasets: make(map[string]string),
			}
		}

		tenant := tenantMap[tenantID]
		tenant.Ports = append(tenant.Ports, listener.Port)
		tenant.Datasets[listener.DatasetID] = protocol
		tenant.PortCount++

		// Check for dataset conflicts within tenant
		for existingDataset, existingProtocol := range tenant.Datasets {
			if existingDataset == listener.DatasetID && existingProtocol != protocol {
				return fmt.Errorf("listener %d: dataset '%s' for tenant '%s' uses conflicting protocols ('%s' vs '%s')",
					i, listener.DatasetID, tenantID, protocol, existingProtocol)
			}
		}
	}

	// Log multi-tenant summary
	if len(tenantMap) > 1 {
		log.Infof("Multi-tenant configuration detected: %d tenants across %d ports", len(tenantMap), len(portMap))
		for tenantID, info := range tenantMap {
			log.Infof("  Tenant '%s': %d ports, %d datasets", tenantID, info.PortCount, len(info.Datasets))
		}
	} else if len(tenantMap) == 1 {
		for tenantID, info := range tenantMap {
			log.Infof("Single-tenant configuration: tenant=%s ports=%d datasets=%d", 
				tenantID, info.PortCount, len(info.Datasets))
		}
	}

	return nil
}

// GetTenantInfo returns information about all configured tenants
func (cfg *Config) GetTenantInfo() map[string]*TenantInfo {
	tenantMap := make(map[string]*TenantInfo)

	for _, listener := range cfg.UDP.Listeners {
		if listener.DatasetID == "" {
			continue // Skip inactive listeners
		}

		tenantID := cfg.GetEffectiveTenantID(listener)

		protocol := listener.Protocol
		if protocol == "" {
			protocol = "udp"
		}

		if tenantMap[tenantID] == nil {
			tenantMap[tenantID] = &TenantInfo{
				TenantID: tenantID,
				Ports:    []int{},
				Datasets: make(map[string]string),
			}
		}

		tenant := tenantMap[tenantID]
		tenant.Ports = append(tenant.Ports, listener.Port)
		tenant.Datasets[listener.DatasetID] = protocol
		tenant.PortCount++
	}

	return tenantMap
}

// IsMultiTenant returns true if multiple tenants are configured
func (cfg *Config) IsMultiTenant() bool {
	return len(cfg.GetTenantInfo()) > 1
}

// GetEffectiveBearerToken returns the bearer token for a listener (listener-specific or global fallback)
func (cfg *Config) GetEffectiveBearerToken(listener UDPListener) string {
	if listener.BearerToken != "" {
		return listener.BearerToken
	}
	return cfg.BearerToken
}

// GetEffectiveTenantID returns the tenant ID for a listener (listener-specific or global fallback)
func (cfg *Config) GetEffectiveTenantID(listener UDPListener) string {
	if listener.TenantID != "" {
		return listener.TenantID
	}
	return cfg.TenantID
}

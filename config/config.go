package config

import (
	"strings"
	"time"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
	"github.com/pkg/errors"

	"github.com/n0needt0/bytefreezer-proxy/alerts"
)

var k = koanf.New(".")

type Config struct {
	App          App           `mapstructure:"app"`
	Logging      LoggingConfig `mapstructure:"logging"`
	Server       Server        `mapstructure:"server"`
	UDP          UDP           `mapstructure:"udp"`
	Receiver     Receiver      `mapstructure:"receiver"`
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
	Port                int           `mapstructure:"port"` // Deprecated: use Listeners instead
	ReadBufferSizeBytes int           `mapstructure:"read_buffer_size_bytes"`
	MaxBatchLines       int           `mapstructure:"max_batch_lines"`
	MaxBatchBytes       int64         `mapstructure:"max_batch_bytes"`
	BatchTimeoutSeconds int           `mapstructure:"batch_timeout_seconds"`
	CompressionLevel    int           `mapstructure:"compression_level"`
	EnableCompression   bool          `mapstructure:"enable_compression"`
	Listeners           []UDPListener `mapstructure:"listeners"` // New: multiple port/dataset mapping
}

type UDPListener struct {
	Port      int    `mapstructure:"port"`
	DatasetID string `mapstructure:"dataset_id"`
	TenantID  string `mapstructure:"tenant_id,omitempty"` // Optional: override global tenant
}

type Receiver struct {
	BaseURL       string `mapstructure:"base_url"`
	TenantID      string `mapstructure:"tenant_id"`
	DatasetID     string `mapstructure:"dataset_id"`
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

	// Spooling defaults
	if cfg.Spooling.Directory == "" {
		cfg.Spooling.Directory = "/tmp/bytefreezer-proxy"
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

	// Backwards compatibility: if no listeners configured but port is set, create single listener
	if len(cfg.UDP.Listeners) == 0 && cfg.UDP.Port > 0 {
		cfg.UDP.Listeners = []UDPListener{
			{
				Port:      cfg.UDP.Port,
				DatasetID: "default-dataset",
				TenantID:  "", // Will use global tenant
			},
		}
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

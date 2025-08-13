package config

import (
	"strings"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/posflag"
	"github.com/knadh/koanf/v2"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

var k = koanf.New(".")

type Config struct {
	App          App           `mapstructure:"app"`
	Logging      LoggingConfig `mapstructure:"logging"`
	Server       Server        `mapstructure:"server"`
	Bytefreezer  Bytefreezer   `mapstructure:"bytefreezer"`
	Otel         Otel          `mapstructure:"otel"`
	Housekeeping Housekeeping  `mapstructure:"housekeeping"`
	S3           S3Config      `mapstructure:"s3"`
}

type Bytefreezer struct {
	Host                string `mapstructure:"udp_host"`
	Port                int    `mapstructure:"udp_port"`
	ReadBufferSizeBytes int    `mapstructure:"read_buffer_size_bytes"`
	DataChannelSize     int    `mapstructure:"data_channel_size"`
	MaxBatchRows        int    `mapstructure:"batch_rows"`
	MaxBatchBytes       int64  `mapstructure:"batch_bytes"`
	Token               string `mapstructure:"token"`
	EnableJsonOutput    bool   `mapstructure:"json"`
	EnableParquetOutput bool   `mapstructure:"parquet"`
	KeepJsonSource      bool   `mapstructure:"keep_json_source"`
	KeepParquetSource   bool   `mapstructure:"keep_parquet_source"`
	WebhookPort         int    `mapstructure:"webhook_port"`
	WebhookEnabled      bool   `mapstructure:"webhook_enabled"`
	UdpEnabled          bool   `mapstructure:"udpenabled"`
}

type S3Config struct {
	BucketName  string `mapstructure:"bucket_name"`
	Region      string `mapstructure:"region"`
	AccessKey   string `mapstructure:"access_key"`
	SecretKey   string `mapstructure:"secret_key"`
	Endpoint    string `mapstructure:"endpoint"`
	Ssl         bool   `mapstructure:"ssl"`
	Compression bool   `mapstructure:"compression"`
}

type Housekeeping struct {
	Enabled         bool `mapstructure:"enabled"`
	IntervalSeconds int  `mapstructure:"intervalseconds"`
}

type App struct {
	Name    string `mapstructure:"name"`
	Version string `mapstructure:"version"`
	Env     string `mapstructure:"env"`
}

// LoggingConfig stores global logging configurations
type LoggingConfig struct {
	Level    string `mapstructure:"level"`
	Encoding string `mapstructure:"encoding"`
}

type Server struct {
	Port int  `mapstructure:"port"`
	Dev  bool `mapstructure:"dev"`
}

type Otel struct {
	Enabled               bool   `mapstructure:"enabled"`
	Endpoint              string `mapstructure:"endpoint"`
	ScrapeIntervalSeconds int    `mapstructure:"scrapeIntervalseconds"`
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

	return nil
}

func LoadFlags(cmd *cobra.Command) error {
	return k.Load(posflag.Provider(cmd.Flags(), ".", k), nil)
}

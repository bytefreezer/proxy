package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/n0needt0/bytefreezer-proxy/api"
	"github.com/n0needt0/bytefreezer-proxy/config"
	"github.com/n0needt0/bytefreezer-proxy/plugins"
	"github.com/n0needt0/bytefreezer-proxy/services"
	"github.com/n0needt0/go-goodies/log"

	// Import plugin packages to register them
	_ "github.com/n0needt0/bytefreezer-proxy/plugins/ebpf"
	_ "github.com/n0needt0/bytefreezer-proxy/plugins/http"
	_ "github.com/n0needt0/bytefreezer-proxy/plugins/ipfix"
	_ "github.com/n0needt0/bytefreezer-proxy/plugins/kafka"
	_ "github.com/n0needt0/bytefreezer-proxy/plugins/kinesis"
	_ "github.com/n0needt0/bytefreezer-proxy/plugins/nats"
	_ "github.com/n0needt0/bytefreezer-proxy/plugins/netflow"
	_ "github.com/n0needt0/bytefreezer-proxy/plugins/sflow"
	_ "github.com/n0needt0/bytefreezer-proxy/plugins/sqs"
	_ "github.com/n0needt0/bytefreezer-proxy/plugins/syslog"
	_ "github.com/n0needt0/bytefreezer-proxy/plugins/udp"
)

var (
	version   = "dev"
	buildTime = "unknown"
	gitCommit = "unknown"
)

func main() {
	// Define command line flags
	var (
		showVersion    = flag.Bool("version", false, "Show version and exit")
		showHelp       = flag.Bool("help", false, "Show help and exit")
		configFile     = flag.String("config", "config.yaml", "Path to configuration file")
		validateConfig = flag.Bool("validate-config", false, "Validate configuration and exit")
		dryRun         = flag.Bool("dry-run", false, "Load configuration and exit (for testing)")
	)

	flag.Parse()

	// Handle version flag
	if *showVersion {
		fmt.Printf("bytefreezer-proxy version %s (built %s, commit %s)\n", version, buildTime, gitCommit)
		os.Exit(0)
	}

	// Handle help flag
	if *showHelp {
		fmt.Printf("ByteFreezer Proxy - UDP log forwarding service\n\n")
		fmt.Printf("Usage: %s [options]\n\n", os.Args[0])
		fmt.Printf("Options:\n")
		flag.PrintDefaults()
		os.Exit(0)
	}

	// Load configuration
	var cfg config.Config
	if err := config.LoadConfig(*configFile, "BYTEFREEZER_PROXY_", &cfg); err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Set runtime build info
	cfg.App.GitCommit = gitCommit

	// Always validate configuration during startup
	if err := config.ValidateConfig(&cfg); err != nil {
		log.Fatalf("Configuration validation failed: %v", err)
	}

	// Handle config validation flag
	if *validateConfig {
		if err := config.ValidateConfig(&cfg); err != nil {
			log.Fatalf("Configuration validation failed: %v", err)
		}
		fmt.Printf("Configuration validation successful: %s\n", *configFile)
		os.Exit(0)
	}

	// Handle dry run
	if *dryRun {
		fmt.Printf("Dry run successful - configuration loaded from: %s\n", *configFile)
		os.Exit(0)
	}

	// Initialize logging
	setLogLevel(cfg.Logging.Level)

	log.Info("Starting application: " + cfg.App.Name + " version: " + cfg.App.Version)

	// Initialize OTEL if enabled
	if cfg.Otel.Enabled {
		cleanup, err := initOTEL(&cfg)
		if err != nil {
			log.Fatalf("Failed to initialize OTEL: %v", err)
		}
		defer cleanup()
	}

	// Initialize configuration components
	if err := cfg.InitializeComponents(); err != nil {
		log.Fatalf("Failed to initialize components: %v", err)
	}

	// Initialize dataset metrics client
	if cfg.ControlURL != "" {
		cfg.DatasetMetricsClient = services.NewDatasetMetricsClient(
			cfg.ControlURL,
			5,    // 5 second timeout
			true, // enabled when ControlURL is set
		)
		log.Infof("Dataset metrics client initialized (endpoint: %s)", cfg.ControlURL)
	} else {
		log.Info("Dataset metrics client disabled (no control URL configured)")
	}

	// Initialize health reporting service
	var healthReportingService *services.HealthReportingService
	if cfg.HealthReporting.Enabled && cfg.ControlURL != "" {
		reportInterval := time.Duration(cfg.HealthReporting.ReportInterval) * time.Second
		if reportInterval <= 0 {
			reportInterval = 5 * time.Minute // Default 5 minutes
		}

		timeout := time.Duration(cfg.HealthReporting.TimeoutSeconds) * time.Second
		if timeout <= 0 {
			timeout = 30 * time.Second // Default 30 seconds
		}

		// Get actual hostname
		hostname, err := os.Hostname()
		if err != nil {
			log.Warnf("Failed to get hostname, using 'localhost': %v", err)
			hostname = "localhost"
		}

		// Determine instance API URL without protocol (proxy API endpoint)
		instanceAPI := fmt.Sprintf("%s:%d", hostname, cfg.Server.ApiPort)

		// Build configuration data with masked sensitive fields
		configuration := buildHealthConfiguration(&cfg, instanceAPI)

		healthReportingService = services.NewHealthReportingService(
			cfg.ControlURL,
			cfg.AccountID,
			cfg.BearerToken,
			"bytefreezer-proxy",
			instanceAPI,
			reportInterval,
			timeout,
			configuration,
		)

		// Register service on startup if enabled
		if cfg.HealthReporting.RegisterOnStartup {
			if err := healthReportingService.RegisterService(); err != nil {
				log.Warnf("Failed to register service on startup: %v", err)
			}
		}

		// Start health reporting
		healthReportingService.Start()
		log.Infof("Health reporting service started - reporting to %s every %v", cfg.ControlURL, reportInterval)
	} else {
		log.Info("Health reporting is disabled")
	}

	// Create services
	svcs := services.NewServices(&cfg)

	// Start spooling service if enabled
	if err := svcs.SpoolingService.Start(); err != nil {
		log.Fatalf("Failed to start spooling service: %v", err)
	}

	// Initialize uptime tracking
	startTime := time.Now()
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			svcs.ProxyStats.UptimeSeconds = int64(time.Since(startTime).Seconds())
		}
	}()

	// Create and start API server
	apiServer := api.NewAPIServer(svcs, &cfg)
	router := apiServer.NewRouter()

	var wg sync.WaitGroup

	// Start API server
	wg.Add(1)
	go func() {
		defer wg.Done()
		address := fmt.Sprintf(":%d", cfg.Server.ApiPort)
		apiServer.Serve(address, router)
	}()

	// Create HTTP forwarder (always needed for plugin system)
	forwarder := services.NewHTTPForwarderWithMetrics(&cfg, svcs.MetricsService)

	// Create plugin service with spooling support (even if no local inputs - may get remote inputs)
	var pluginService *services.PluginService
	var err error
	pluginService, err = services.NewPluginService(&cfg, forwarder, svcs.SpoolingService)
	if err != nil {
		log.Fatalf("Failed to create plugin service: %v", err)
	}

	// Store plugin service in svcs for access by config polling
	svcs.PluginService = pluginService

	// Initialize config polling service if control-only mode is enabled
	if cfg.ConfigMode == "control-only" && cfg.ControlURL != "" && cfg.ConfigPolling.Enabled {
		log.Infof("Initializing config polling service (mode: %s, account_id: %s)",
			cfg.ConfigMode, cfg.AccountID)

		// Create callback to handle config changes from Control
		onConfigChange := func(remoteConfig *services.ControlProxyConfig) error {
			log.Infof("Applying configuration change from Control: %d plugin configs",
				len(remoteConfig.PluginConfigs))

			// Convert map[string]interface{} plugin configs to plugins.PluginConfig
			newPluginConfigs := make([]plugins.PluginConfig, 0, len(remoteConfig.PluginConfigs))
			for _, pc := range remoteConfig.PluginConfigs {
				// Extract fields from map
				pluginType, _ := pc["type"].(string)
				pluginName, _ := pc["name"].(string)
				pluginConfig, _ := pc["config"].(map[string]interface{})

				if pluginType == "" || pluginName == "" {
					log.Warnf("Skipping invalid plugin config: missing type or name")
					continue
				}

				newPluginConfigs = append(newPluginConfigs, plugins.PluginConfig{
					Type:   pluginType,
					Name:   pluginName,
					Config: pluginConfig,
				})
			}

			// Merge with local configs (local configs from config.yaml)
			allConfigs := append([]plugins.PluginConfig{}, cfg.Inputs...)
			allConfigs = append(allConfigs, newPluginConfigs...)

			log.Infof("Reloading plugins: %d local + %d remote = %d total",
				len(cfg.Inputs), len(newPluginConfigs), len(allConfigs))

			// Reload plugin service with merged configs
			if err := pluginService.Reload(allConfigs); err != nil {
				return fmt.Errorf("failed to reload plugins: %w", err)
			}

			return nil
		}

		// Create config polling service
		configPollingService, err := services.NewConfigPollingService(&cfg, onConfigChange)
		if err != nil {
			log.Fatalf("Failed to create config polling service: %v", err)
		}

		// Store in services
		svcs.ConfigPollingService = configPollingService

		// Start config polling (will perform initial poll immediately)
		configPollingService.Start()
		log.Info("Config polling service started")
	} else {
		log.Infof("Config polling disabled (mode: %s, control_url: %s, polling_enabled: %v)",
			cfg.ConfigMode, cfg.ControlURL, cfg.ConfigPolling.Enabled)
	}

	// Start plugin service
	if len(cfg.Inputs) > 0 {
		log.Infof("Starting plugin system with %d local input plugins", len(cfg.Inputs))
		pluginTypes := make([]string, len(cfg.Inputs))
		for i, input := range cfg.Inputs {
			pluginTypes[i] = input.Type
		}
		log.Infof("Local plugin types: %v", pluginTypes)
	} else {
		log.Info("No local input plugins configured - waiting for remote configuration from Control")
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := pluginService.Start(); err != nil {
			log.Errorf("Plugin service failed: %v", err)
			// Send SOC alert
			if cfg.SOCAlertClient != nil {
				cfg.SOCAlertClient.SendAlert("high", "Plugin Service Failed",
					"Input plugin system failed to start", err.Error())
			}
		}
	}()

	// Setup signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	log.Info("ByteFreezer Proxy is running. Press Ctrl+C to stop.")

	// Wait for shutdown signal
	<-sigChan
	log.Info("Received shutdown signal, stopping services...")

	// Shutdown services gracefully
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Stop health reporting service
	if healthReportingService != nil {
		go func() {
			healthReportingService.Stop()
		}()
	}

	// Stop spooling service
	go func() {
		if err := svcs.SpoolingService.Stop(); err != nil {
			log.Errorf("Error stopping spooling service: %v", err)
		}
	}()

	// Stop config polling service
	if svcs.ConfigPollingService != nil {
		go func() {
			svcs.ConfigPollingService.Stop()
			log.Info("Config polling service stopped")
		}()
	}

	// Stop input systems
	if pluginService != nil {
		go func() {
			if err := pluginService.Stop(); err != nil {
				log.Errorf("Error stopping plugin service: %v", err)
			}
		}()
	}

	// Stop API server
	go func() {
		apiServer.Stop()
	}()

	// Wait for graceful shutdown or timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Info("All services stopped gracefully")
	case <-shutdownCtx.Done():
		log.Warn("Shutdown timeout exceeded, forcing exit")
	}

	log.Info("ByteFreezer Proxy stopped")
}

func setLogLevel(levelStr string) {
	switch strings.ToLower(levelStr) {
	case "debug":
		log.SetMinLogLevel(log.MinLevelDebug)
	case "info":
		log.SetMinLogLevel(log.MinLevelInfo)
	case "warn":
		log.SetMinLogLevel(log.MinLevelWarn)
	case "error":
		log.SetMinLogLevel(log.MinLevelError)
	}
}

// buildHealthConfiguration builds comprehensive configuration for health reporting
// with sensitive data masked
func buildHealthConfiguration(cfg *config.Config, instanceAPI string) map[string]interface{} {
	maskSensitive := func(value string) string {
		if value == "" {
			return ""
		}
		if len(value) <= 4 {
			return "****"
		}
		return value[:2] + "****" + value[len(value)-2:]
	}

	return map[string]interface{}{
		"service_type":    "bytefreezer-proxy",
		"version":         cfg.App.Version,
		"git_commit":      cfg.App.GitCommit,
		"instance_api":    instanceAPI,
		"report_interval": cfg.HealthReporting.ReportInterval,
		"timeout":         fmt.Sprintf("%ds", cfg.HealthReporting.TimeoutSeconds),
		"api": map[string]interface{}{
			"port": cfg.Server.ApiPort,
		},
		"account_id":   cfg.AccountID,
		"tenant_id":    cfg.TenantID,
		"bearer_token": maskSensitive(cfg.BearerToken),
		"batching": map[string]interface{}{
			"enabled":             cfg.Batching.Enabled,
			"max_lines":           cfg.Batching.MaxLines,
			"max_bytes":           cfg.Batching.MaxBytes,
			"timeout_seconds":     cfg.Batching.TimeoutSeconds,
			"compression_enabled": cfg.Batching.CompressionEnabled,
			"compression_level":   cfg.Batching.CompressionLevel,
		},
		"receiver": map[string]interface{}{
			"base_url":            cfg.Receiver.BaseURL,
			"timeout_seconds":     cfg.Receiver.TimeoutSec,
			"upload_worker_count": cfg.Receiver.UploadWorkerCount,
			"max_idle_conns":      cfg.Receiver.MaxIdleConns,
			"max_conns_per_host":  cfg.Receiver.MaxConnsPerHost,
		},
		"spooling": map[string]interface{}{
			"enabled":                         cfg.Spooling.Enabled,
			"directory":                       cfg.Spooling.Directory,
			"max_size_bytes":                  cfg.Spooling.MaxSizeBytes,
			"retry_attempts":                  cfg.Spooling.RetryAttempts,
			"retry_interval_seconds":          cfg.Spooling.RetryIntervalSec,
			"cleanup_interval_seconds":        cfg.Spooling.CleanupIntervalSec,
			"keep_src":                        cfg.Spooling.KeepSrc,
			"queue_processing_interval_seconds": cfg.Spooling.QueueProcessingIntervalSec,
			"organization":                    cfg.Spooling.Organization,
			"per_tenant_limits":               cfg.Spooling.PerTenantLimits,
			"max_files_per_dataset":           cfg.Spooling.MaxFilesPerDataset,
			"max_age_days":                    cfg.Spooling.MaxAgeDays,
		},
		"otel": map[string]interface{}{
			"enabled":         cfg.Otel.Enabled,
			"endpoint":        cfg.Otel.Endpoint,
			"service_name":    cfg.Otel.ServiceName,
			"prometheus_mode": cfg.Otel.PrometheusMode,
			"metrics_port":    cfg.Otel.MetricsPort,
		},
		"soc": map[string]interface{}{
			"enabled":  cfg.SOC.Enabled,
			"endpoint": cfg.SOC.Endpoint,
			"timeout":  cfg.SOC.Timeout,
		},
		"capabilities": []string{
			"udp_ingestion",
			"multi_protocol",
			"batching",
			"spooling",
			"http_forwarding",
			"plugin_system",
		},
		"plugin_schemas": getPluginSchemas(),
	}
}

// getPluginSchemas returns all registered plugin schemas for health reporting
func getPluginSchemas() []map[string]interface{} {
	schemas := plugins.GlobalRegistry.GetAllSchemas()
	result := make([]map[string]interface{}, len(schemas))

	for i, schema := range schemas {
		fields := make([]map[string]interface{}, len(schema.Fields))
		for j, field := range schema.Fields {
			fieldMap := map[string]interface{}{
				"name":        field.Name,
				"type":        field.Type,
				"required":    field.Required,
				"description": field.Description,
			}
			if field.Default != nil {
				fieldMap["default"] = field.Default
			}
			if field.Validation != "" {
				fieldMap["validation"] = field.Validation
			}
			if field.Placeholder != "" {
				fieldMap["placeholder"] = field.Placeholder
			}
			if len(field.Options) > 0 {
				fieldMap["options"] = field.Options
			}
			if field.Group != "" {
				fieldMap["group"] = field.Group
			}
			fields[j] = fieldMap
		}

		schemaMap := map[string]interface{}{
			"name":         schema.Name,
			"display_name": schema.DisplayName,
			"description":  schema.Description,
			"category":     schema.Category,
			"transport":    schema.Transport,
			"fields":       fields,
		}
		if schema.DefaultPort > 0 {
			schemaMap["default_port"] = schema.DefaultPort
		}
		result[i] = schemaMap
	}

	return result
}

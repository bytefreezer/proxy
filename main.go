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
	_ "github.com/n0needt0/bytefreezer-proxy/plugins"
	"github.com/n0needt0/bytefreezer-proxy/services"
	"github.com/n0needt0/go-goodies/log"

	// Import plugin packages to register them
	_ "github.com/n0needt0/bytefreezer-proxy/plugins/http"
	_ "github.com/n0needt0/bytefreezer-proxy/plugins/kafka"
	_ "github.com/n0needt0/bytefreezer-proxy/plugins/nats"
	_ "github.com/n0needt0/bytefreezer-proxy/plugins/udp"
)

var (
	version   = "dev"
	buildTime = "unknown"
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
		fmt.Printf("bytefreezer-proxy version %s (built %s)\n", version, buildTime)
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

	// Use plugin system for input processing
	var pluginService *services.PluginService

	if len(cfg.Inputs) > 0 {
		log.Infof("Starting plugin system with %d input plugins", len(cfg.Inputs))

		// Create HTTP forwarder
		forwarder := services.NewHTTPForwarderWithMetrics(&cfg, svcs.MetricsService)

		// Create plugin service with spooling support
		var err error
		pluginService, err = services.NewPluginService(&cfg, forwarder, svcs.SpoolingService)
		if err != nil {
			log.Fatalf("Failed to create plugin service: %v", err)
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

		pluginTypes := make([]string, len(cfg.Inputs))
		for i, input := range cfg.Inputs {
			pluginTypes[i] = input.Type
		}
		log.Infof("Plugin system started with input types: %v", pluginTypes)
	} else {
		log.Info("No input plugins configured. Please configure inputs in the configuration file.")
	}

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

	// Stop spooling service
	go func() {
		if err := svcs.SpoolingService.Stop(); err != nil {
			log.Errorf("Error stopping spooling service: %v", err)
		}
	}()

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


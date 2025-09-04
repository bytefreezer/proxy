package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/n0needt0/bytefreezer-proxy/api"
	"github.com/n0needt0/bytefreezer-proxy/config"
	"github.com/n0needt0/bytefreezer-proxy/services"
	"github.com/n0needt0/bytefreezer-proxy/udp"
	"github.com/n0needt0/go-goodies/log"
)

func main() {
	// Load configuration
	var cfg config.Config
	if err := config.LoadConfig("config.yaml", "BYTEFREEZER_PROXY_", &cfg); err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
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

	// Create and start UDP listener if enabled
	var udpListener *udp.Listener
	if cfg.UDP.Enabled {
		udpListener = udp.NewListener(svcs, &cfg)
		
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := udpListener.Start(); err != nil {
				log.Errorf("UDP listener failed: %v", err)
				
				// Send SOC alert
				if cfg.SOCAlertClient != nil {
					cfg.SOCAlertClient.SendUDPListenerFailureAlert(err)
				}
			}
		}()

		log.Info(fmt.Sprintf("UDP listener enabled on %s:%d", cfg.UDP.Host, cfg.UDP.Port))
	} else {
		log.Info("UDP listener is disabled")
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

	// Stop UDP listener
	if udpListener != nil {
		go func() {
			if err := udpListener.Stop(); err != nil {
				log.Errorf("Error stopping UDP listener: %v", err)
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
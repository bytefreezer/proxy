package http

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/mitchellh/mapstructure"
	"github.com/n0needt0/bytefreezer-proxy/plugins"
	"github.com/n0needt0/go-goodies/log"
)

// Plugin implements the HTTP webhook input plugin with direct filesystem writes
type Plugin struct {
	config  Config
	server  *http.Server
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	health  plugins.PluginHealth
	mu      sync.RWMutex
	spooler plugins.SpoolingInterface
	metrics PluginMetrics
}

// Config represents HTTP plugin configuration
type Config struct {
	Host                 string `mapstructure:"host"`
	Port                 int    `mapstructure:"port"`
	Path                 string `mapstructure:"path"`
	TenantID             string `mapstructure:"tenant_id"`
	DatasetID            string `mapstructure:"dataset_id"`
	DataHint             string `mapstructure:"data_hint,omitempty"`             // data format hint (e.g., "ndjson")
	BearerToken          string `mapstructure:"bearer_token,omitempty"`
	MaxPayloadSize       int64  `mapstructure:"max_payload_size,omitempty"`      // bytes
	MaxLinesPerRequest   int    `mapstructure:"max_lines_per_request,omitempty"` // lines limit
	ReadTimeout          int    `mapstructure:"read_timeout,omitempty"`          // seconds
	WriteTimeout         int    `mapstructure:"write_timeout,omitempty"`         // seconds
	EnableAuthentication bool   `mapstructure:"enable_authentication,omitempty"`
}

// PluginMetrics tracks HTTP plugin metrics
type PluginMetrics struct {
	RequestsReceived    uint64
	BytesReceived       uint64
	RequestsRejected    uint64
	LastRequestTime     time.Time
	StartTime           time.Time
	PayloadTooLarge     uint64
	TooManyLines        uint64
	AuthenticationFails uint64
}

// NewPlugin creates a new HTTP plugin instance
func NewPlugin() plugins.InputPlugin {
	return &Plugin{
		health: plugins.PluginHealth{
			Status:      plugins.HealthStatusStopped,
			Message:     "Plugin created but not started",
			LastUpdated: time.Now(),
		},
		metrics: PluginMetrics{
			StartTime: time.Now(),
		},
	}
}

// Name returns the plugin name
func (p *Plugin) Name() string {
	return "http"
}

// Configure initializes the plugin with configuration
func (p *Plugin) Configure(config map[string]interface{}) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Decode configuration
	if err := mapstructure.Decode(config, &p.config); err != nil {
		return fmt.Errorf("failed to decode HTTP plugin config: %w", err)
	}

	// Set defaults
	if p.config.Host == "" {
		p.config.Host = "0.0.0.0"
	}
	if p.config.Port == 0 {
		return fmt.Errorf("port is required for HTTP plugin")
	}
	if p.config.Path == "" {
		p.config.Path = "/webhook"
	}
	if p.config.TenantID == "" {
		return fmt.Errorf("tenant_id is required for HTTP plugin")
	}
	if p.config.DatasetID == "" {
		return fmt.Errorf("dataset_id is required for HTTP plugin")
	}
	if p.config.MaxPayloadSize == 0 {
		p.config.MaxPayloadSize = 1048576 // 1MB default
	}
	if p.config.MaxLinesPerRequest == 0 {
		p.config.MaxLinesPerRequest = 1000 // 1000 lines default
	}
	if p.config.ReadTimeout == 0 {
		p.config.ReadTimeout = 30 // 30 seconds
	}
	if p.config.WriteTimeout == 0 {
		p.config.WriteTimeout = 30 // 30 seconds
	}

	// Ensure path starts with /
	if !strings.HasPrefix(p.config.Path, "/") {
		p.config.Path = "/" + p.config.Path
	}

	p.updateHealth(plugins.HealthStatusStopped, "Plugin configured successfully", "")
	log.Infof("HTTP plugin configured: %s:%d%s -> %s/%s (max %d bytes, %d lines)",
		p.config.Host, p.config.Port, p.config.Path, p.config.TenantID, p.config.DatasetID,
		p.config.MaxPayloadSize, p.config.MaxLinesPerRequest)

	return nil
}

// Start begins the HTTP webhook server with direct filesystem writes
func (p *Plugin) Start(ctx context.Context, spooler plugins.SpoolingInterface) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.ctx != nil {
		return fmt.Errorf("plugin is already started")
	}

	// Set default data hint if not specified
	if p.config.DataHint == "" {
		p.config.DataHint = "raw"
	}

	p.ctx, p.cancel = context.WithCancel(ctx)
	p.spooler = spooler
	p.metrics.StartTime = time.Now()

	// Create HTTP server
	mux := http.NewServeMux()
	mux.HandleFunc(p.config.Path, p.webhookHandler)
	mux.HandleFunc("/health", p.healthHandler)

	p.server = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", p.config.Host, p.config.Port),
		Handler:      mux,
		ReadTimeout:  time.Duration(p.config.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(p.config.WriteTimeout) * time.Second,
	}

	p.updateHealth(plugins.HealthStatusStarting, "Starting HTTP webhook server", "")

	// Start server in goroutine
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()

		log.Infof("HTTP webhook server starting on %s", p.server.Addr)
		if err := p.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Errorf("HTTP server error: %v", err)
			p.updateHealth(plugins.HealthStatusUnhealthy, "HTTP server error", err.Error())
		}
	}()

	p.updateHealth(plugins.HealthStatusHealthy, fmt.Sprintf("HTTP webhook server active with direct spooling on %s%s", p.server.Addr, p.config.Path), "")
	log.Infof("HTTP plugin started with direct spooling on %s%s", p.server.Addr, p.config.Path)

	return nil
}

// Stop gracefully shuts down the HTTP plugin
func (p *Plugin) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cancel == nil {
		return nil // Already stopped
	}

	p.updateHealth(plugins.HealthStatusStopping, "Shutting down HTTP server", "")

	// Cancel context
	p.cancel()

	// Shutdown HTTP server
	if p.server != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := p.server.Shutdown(shutdownCtx); err != nil {
			log.Errorf("Error shutting down HTTP server: %v", err)
		}
	}

	// Wait for all goroutines to finish
	p.wg.Wait()

	p.ctx = nil
	p.cancel = nil
	p.server = nil

	p.updateHealth(plugins.HealthStatusStopped, "HTTP server stopped", "")
	log.Infof("HTTP plugin stopped")

	return nil
}

// Health returns the current plugin health status
func (p *Plugin) Health() plugins.PluginHealth {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.health
}

// updateHealth updates the plugin health status
func (p *Plugin) updateHealth(status plugins.HealthStatus, message, lastError string) {
	p.health = plugins.PluginHealth{
		Status:      status,
		Message:     message,
		LastError:   lastError,
		LastUpdated: time.Now(),
	}
}

// formatData normalizes data according to the specified format hint
func (p *Plugin) formatData(data []byte, dataHint string) ([]byte, error) {
	formatter := plugins.GetFormatter(dataHint)
	return formatter.Format(data)
}

// webhookHandler handles incoming HTTP webhook requests
func (p *Plugin) webhookHandler(w http.ResponseWriter, r *http.Request) {
	// Only accept POST requests
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		p.mu.Lock()
		p.metrics.RequestsRejected++
		p.mu.Unlock()
		return
	}

	// Check authentication if enabled
	if p.config.EnableAuthentication && p.config.BearerToken != "" {
		authHeader := r.Header.Get("Authorization")
		expectedToken := "Bearer " + p.config.BearerToken
		if authHeader != expectedToken {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			p.mu.Lock()
			p.metrics.AuthenticationFails++
			p.metrics.RequestsRejected++
			p.mu.Unlock()
			return
		}
	}

	// Check content length
	if r.ContentLength > p.config.MaxPayloadSize {
		http.Error(w, "Payload too large", http.StatusRequestEntityTooLarge)
		p.mu.Lock()
		p.metrics.PayloadTooLarge++
		p.metrics.RequestsRejected++
		p.mu.Unlock()
		return
	}

	// Read body with size limit
	body, err := io.ReadAll(io.LimitReader(r.Body, p.config.MaxPayloadSize))
	if err != nil {
		http.Error(w, "Error reading request body", http.StatusBadRequest)
		p.mu.Lock()
		p.metrics.RequestsRejected++
		p.mu.Unlock()
		return
	}

	// Check line count limit (simple newline count)
	lineCount := strings.Count(string(body), "\n") + 1
	if len(body) > 0 && body[len(body)-1] != '\n' {
		// Count lines properly if data doesn't end with newline
	}

	if lineCount > p.config.MaxLinesPerRequest {
		http.Error(w, fmt.Sprintf("Too many lines (max %d)", p.config.MaxLinesPerRequest), http.StatusRequestEntityTooLarge)
		p.mu.Lock()
		p.metrics.TooManyLines++
		p.metrics.RequestsRejected++
		p.mu.Unlock()
		return
	}

	// Format and normalize data according to data hint
	formattedData := body
	if p.config.DataHint != "" {
		var err error
		formattedData, err = p.formatData(body, p.config.DataHint)
		if err != nil {
			log.Warnf("Data formatting failed for request from %s (format: %s): %v", r.RemoteAddr, p.config.DataHint, err)
			http.Error(w, fmt.Sprintf("Invalid %s format: %v", p.config.DataHint, err), http.StatusBadRequest)
			p.mu.Lock()
			p.metrics.RequestsRejected++
			p.mu.Unlock()
			return
		}
		log.Debugf("Data formatting successful for %s/%s (format: %s), normalized %d bytes -> %d bytes",
			p.config.TenantID, p.config.DatasetID, p.config.DataHint, len(body), len(formattedData))
	}

	// Update metrics
	p.mu.Lock()
	p.metrics.RequestsReceived++
	p.metrics.BytesReceived += uint64(len(formattedData))
	p.metrics.LastRequestTime = time.Now()
	p.mu.Unlock()

	// Create data message for output
	dataMsg := &plugins.DataMessage{
		Data:      formattedData,
		TenantID:  p.config.TenantID,
		DatasetID: p.config.DatasetID,
		DataHint:  p.config.DataHint,
		Timestamp: time.Now(),
		SourceIP:  r.RemoteAddr,
		Metadata: map[string]string{
			"content_type": r.Header.Get("Content-Type"),
			"user_agent":   r.Header.Get("User-Agent"),
			"method":       r.Method,
			"path":         r.URL.Path,
			"plugin":       "http",
		},
	}

	// Add bearer token if configured
	if p.config.BearerToken != "" {
		dataMsg.Metadata["bearer_token"] = p.config.BearerToken
	}

	// Add custom headers (X- headers)
	for key, values := range r.Header {
		if strings.HasPrefix(key, "X-") && len(values) > 0 {
			dataMsg.Metadata["header_"+strings.ToLower(key)] = values[0]
		}
	}

	// Write directly to filesystem - NO CHANNEL DROPS POSSIBLE
	bearerToken := p.config.BearerToken
	dataHint := p.config.DataHint
	if dataHint == "" {
		dataHint = "raw" // default for HTTP
	}
	if err := p.spooler.StoreRawMessage(p.config.TenantID, p.config.DatasetID, bearerToken, formattedData, dataHint); err != nil {
		log.Errorf("Failed to store HTTP message to filesystem from %s: %v", r.RemoteAddr, err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		p.mu.Lock()
		p.metrics.RequestsRejected++
		p.mu.Unlock()
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))

	log.Debugf("Stored HTTP message from %s directly to filesystem (%d bytes)",
		r.RemoteAddr, len(formattedData))
}

// healthHandler provides a simple health check endpoint
func (p *Plugin) healthHandler(w http.ResponseWriter, r *http.Request) {
	p.mu.RLock()
	health := p.health
	metrics := p.metrics
	p.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	response := fmt.Sprintf(`{
		"status": "%s",
		"message": "%s", 
		"requests_received": %d,
		"bytes_received": %d,
		"requests_rejected": %d,
		"last_updated": "%s"
	}`, health.Status, health.Message, metrics.RequestsReceived,
		metrics.BytesReceived, metrics.RequestsRejected, health.LastUpdated.Format(time.RFC3339))

	w.Write([]byte(response))
}

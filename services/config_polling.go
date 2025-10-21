package services

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/n0needt0/bytefreezer-proxy/config"
	"github.com/n0needt0/bytefreezer-proxy/plugins"
	"github.com/n0needt0/go-goodies/log"
)

// ControlProxyConfig represents the configuration fetched from control
type ControlProxyConfig struct {
	InstanceID      string                   `json:"instance_id"`
	TenantID        string                   `json:"tenant_id"`
	InstanceAPI     string                   `json:"instance_api"`
	ConfigMode      string                   `json:"config_mode"`
	PluginConfigs   []map[string]interface{} `json:"plugin_configs"`
	ProxySettings   ProxySettings            `json:"proxy_settings"`
	ConfigVersion   int                      `json:"config_version"`
	ConfigHash      string                   `json:"config_hash"`
	ConfigApplied   bool                     `json:"config_applied"`
	ConfigAppliedAt *time.Time               `json:"config_applied_at"`
	Active          bool                     `json:"active"`
	CreatedAt       time.Time                `json:"created_at"`
	UpdatedAt       time.Time                `json:"updated_at"`
}

// ProxySettings represents proxy-level settings from control
type ProxySettings struct {
	Receiver     ReceiverSettings     `json:"receiver,omitempty"`
	Batching     BatchingSettings     `json:"batching,omitempty"`
	Spooling     SpoolingSettings     `json:"spooling,omitempty"`
	Housekeeping HousekeepingSettings `json:"housekeeping,omitempty"`
	Otel         OtelSettings         `json:"otel,omitempty"`
	SOC          SOCSettings          `json:"soc,omitempty"`
	Custom       map[string]interface{} `json:"custom,omitempty"`
}

type ReceiverSettings struct {
	BaseURL           string `json:"base_url,omitempty"`
	TimeoutSeconds    int    `json:"timeout_seconds,omitempty"`
	UploadWorkerCount int    `json:"upload_worker_count,omitempty"`
	MaxIdleConns      int    `json:"max_idle_conns,omitempty"`
	MaxConnsPerHost   int    `json:"max_conns_per_host,omitempty"`
}

type BatchingSettings struct {
	Enabled            bool  `json:"enabled"`
	MaxLines           int   `json:"max_lines,omitempty"`
	MaxBytes           int64 `json:"max_bytes,omitempty"`
	TimeoutSeconds     int   `json:"timeout_seconds,omitempty"`
	CompressionEnabled bool  `json:"compression_enabled"`
	CompressionLevel   int   `json:"compression_level,omitempty"`
}

type SpoolingSettings struct {
	Enabled                        bool   `json:"enabled"`
	Directory                      string `json:"directory,omitempty"`
	MaxSizeBytes                   int64  `json:"max_size_bytes,omitempty"`
	RetryAttempts                  int    `json:"retry_attempts,omitempty"`
	RetryIntervalSeconds           int    `json:"retry_interval_seconds,omitempty"`
	CleanupIntervalSeconds         int    `json:"cleanup_interval_seconds,omitempty"`
	QueueProcessingIntervalSeconds int    `json:"queue_processing_interval_seconds,omitempty"`
	Organization                   string `json:"organization,omitempty"`
	PerTenantLimits                bool   `json:"per_tenant_limits"`
	MaxFilesPerDataset             int    `json:"max_files_per_dataset,omitempty"`
	MaxAgeDays                     int    `json:"max_age_days,omitempty"`
}

type HousekeepingSettings struct {
	Enabled         bool `json:"enabled"`
	IntervalSeconds int  `json:"interval_seconds,omitempty"`
}

type OtelSettings struct {
	Enabled               bool   `json:"enabled"`
	Endpoint              string `json:"endpoint,omitempty"`
	ServiceName           string `json:"service_name,omitempty"`
	ScrapeIntervalSeconds int    `json:"scrape_interval_seconds,omitempty"`
	PrometheusMode        bool   `json:"prometheus_mode"`
	MetricsPort           int    `json:"metrics_port,omitempty"`
	MetricsHost           string `json:"metrics_host,omitempty"`
}

type SOCSettings struct {
	Enabled  bool   `json:"enabled"`
	Endpoint string `json:"endpoint,omitempty"`
	Timeout  int    `json:"timeout,omitempty"`
}

// ConfigPollingService polls control for configuration updates
type ConfigPollingService struct {
	cfg             *config.Config
	controlURL      string
	bearerToken     string
	instanceID      string
	cacheDirectory  string
	pollingInterval time.Duration
	timeout         time.Duration

	currentConfig     *ControlProxyConfig
	currentConfigHash string
	configMutex       sync.RWMutex

	ticker       *time.Ticker
	stopChan     chan struct{}
	httpClient   *http.Client
	onConfigChange func(*ControlProxyConfig) error // Callback for config changes
}

// NewConfigPollingService creates a new configuration polling service
func NewConfigPollingService(cfg *config.Config, onConfigChange func(*ControlProxyConfig) error) (*ConfigPollingService, error) {
	if cfg.ControlURL == "" {
		return nil, fmt.Errorf("control_url is required for config polling")
	}

	// Get instance ID (hostname)
	instanceID, err := os.Hostname()
	if err != nil {
		log.Warnf("Failed to get hostname, using 'localhost': %v", err)
		instanceID = "localhost"
	}

	pollingInterval := time.Duration(cfg.ConfigPolling.IntervalSeconds) * time.Second
	if pollingInterval <= 0 {
		pollingInterval = 5 * time.Minute
	}

	timeout := time.Duration(cfg.ConfigPolling.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	// Ensure cache directory exists
	cacheDir := cfg.ConfigPolling.CacheDirectory
	if cacheDir == "" {
		cacheDir = "/var/cache/bytefreezer-proxy"
	}
	// Use 0750 permissions for security - no access for others
	if err := os.MkdirAll(cacheDir, 0750); err != nil {
		return nil, fmt.Errorf("failed to create cache directory: %w", err)
	}

	return &ConfigPollingService{
		cfg:             cfg,
		controlURL:      cfg.ControlURL,
		bearerToken:     cfg.BearerToken,
		instanceID:      instanceID,
		cacheDirectory:  cacheDir,
		pollingInterval: pollingInterval,
		timeout:         timeout,
		stopChan:        make(chan struct{}),
		httpClient: &http.Client{
			Timeout: timeout,
		},
		onConfigChange: onConfigChange,
	}, nil
}

// Start begins polling for configuration updates
func (s *ConfigPollingService) Start() {
	log.Infof("Config polling service starting (mode: %s, interval: %v)", s.cfg.ConfigMode, s.pollingInterval)

	// Try to load cached config on startup
	if err := s.loadCachedConfig(); err != nil {
		log.Warnf("Failed to load cached config: %v", err)
	}

	// Perform initial poll immediately (if not local-only mode)
	if s.cfg.ConfigMode != "local-only" {
		if err := s.pollConfiguration(); err != nil {
			log.Errorf("Initial config poll failed: %v", err)
		}
	}

	// Start polling loop if enabled
	if s.cfg.ConfigPolling.Enabled && s.cfg.ConfigMode != "local-only" {
		s.ticker = time.NewTicker(s.pollingInterval)
		go s.pollingLoop()
		log.Infof("Config polling enabled - polling every %v", s.pollingInterval)
	} else {
		log.Info("Config polling disabled - using local configuration only")
	}
}

// Stop stops the polling service
func (s *ConfigPollingService) Stop() {
	log.Info("Stopping config polling service...")
	close(s.stopChan)
	if s.ticker != nil {
		s.ticker.Stop()
	}
}

// pollingLoop runs the continuous polling loop
func (s *ConfigPollingService) pollingLoop() {
	for {
		select {
		case <-s.ticker.C:
			if err := s.pollConfiguration(); err != nil {
				log.Errorf("Config poll failed: %v", err)
			}
		case <-s.stopChan:
			return
		}
	}
}

// pollConfiguration fetches configuration from control
func (s *ConfigPollingService) pollConfiguration() error {
	log.Debugf("Polling configuration from control: %s", s.controlURL)

	// Build request URL
	url := fmt.Sprintf("%s/api/v2/proxies/%s/config?tenant_id=%s", s.controlURL, s.instanceID, s.cfg.TenantID)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Add authorization header
	if s.bearerToken != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", s.bearerToken))
	}

	// Make request
	resp, err := s.httpClient.Do(req)
	if err != nil {
		if s.cfg.ConfigPolling.RetryOnError {
			log.Warnf("Control unreachable, using cached config: %v", err)
			return s.loadCachedConfig()
		}
		return fmt.Errorf("failed to poll control: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode == http.StatusNotFound {
		log.Warnf("Proxy instance not configured in control yet (404)")
		return nil // Not an error - just not configured yet
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("control returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var remoteConfig ControlProxyConfig
	if err := json.NewDecoder(resp.Body).Decode(&remoteConfig); err != nil {
		return fmt.Errorf("failed to decode config response: %w", err)
	}

	// Check if config has changed
	if s.hasConfigChanged(&remoteConfig) {
		log.Infof("Configuration changed (version %d -> %d)", s.getConfigVersion(), remoteConfig.ConfigVersion)

		// Apply the new configuration
		if err := s.applyConfiguration(&remoteConfig); err != nil {
			log.Errorf("Failed to apply configuration: %v", err)
			return err
		}

		// Cache the new configuration
		if err := s.cacheConfiguration(&remoteConfig); err != nil {
			log.Errorf("Failed to cache configuration: %v", err)
		}

		// Report back to control that config was applied
		if err := s.reportConfigApplied(remoteConfig.ConfigVersion); err != nil {
			log.Errorf("Failed to report config applied: %v", err)
		}
	} else {
		log.Debugf("Configuration unchanged (version %d, hash %s)", remoteConfig.ConfigVersion, remoteConfig.ConfigHash)
	}

	return nil
}

// hasConfigChanged checks if the remote config differs from current config
func (s *ConfigPollingService) hasConfigChanged(remoteConfig *ControlProxyConfig) bool {
	s.configMutex.RLock()
	defer s.configMutex.RUnlock()

	if s.currentConfig == nil {
		return true // No current config, definitely changed
	}

	// Compare config hashes for quick change detection
	if remoteConfig.ConfigHash != s.currentConfigHash {
		return true
	}

	// Compare versions as fallback
	if remoteConfig.ConfigVersion != s.currentConfig.ConfigVersion {
		return true
	}

	return false
}

// applyConfiguration applies the new configuration
func (s *ConfigPollingService) applyConfiguration(remoteConfig *ControlProxyConfig) error {
	log.Infof("Applying new configuration (version %d)", remoteConfig.ConfigVersion)

	// Detect port conflicts between local and remote config
	if err := s.detectPortConflicts(remoteConfig); err != nil {
		log.Warnf("Port conflicts detected: %v", err)
	}

	// Call the config change callback if registered
	if s.onConfigChange != nil {
		if err := s.onConfigChange(remoteConfig); err != nil {
			return fmt.Errorf("config change callback failed: %w", err)
		}
	}

	// Update current config
	s.configMutex.Lock()
	s.currentConfig = remoteConfig
	s.currentConfigHash = remoteConfig.ConfigHash
	s.configMutex.Unlock()

	log.Infof("Configuration applied successfully (version %d)", remoteConfig.ConfigVersion)
	return nil
}

// detectPortConflicts detects port conflicts between local and remote configs
func (s *ConfigPollingService) detectPortConflicts(remoteConfig *ControlProxyConfig) error {
	localPorts := make(map[int]string) // port -> plugin name

	// Collect ports from local config
	for _, input := range s.cfg.Inputs {
		if portValue, exists := input.Config["port"]; exists {
			var port int
			switch p := portValue.(type) {
			case int:
				port = p
			case float64:
				port = int(p)
			}
			if port > 0 {
				localPorts[port] = fmt.Sprintf("%s[%s]", input.Type, input.Name)
			}
		}
	}

	// Check remote config for conflicts
	conflicts := []string{}
	for _, pluginConfig := range remoteConfig.PluginConfigs {
		if portValue, exists := pluginConfig["port"]; exists {
			var port int
			switch p := portValue.(type) {
			case int:
				port = p
			case float64:
				port = int(p)
			}

			if port > 0 {
				if localPlugin, exists := localPorts[port]; exists {
					pluginName := "unknown"
					if name, ok := pluginConfig["name"].(string); ok {
						pluginName = name
					}
					conflicts = append(conflicts, fmt.Sprintf("Port %d: local '%s' vs remote '%s' - REMOTE TAKES PRECEDENCE",
						port, localPlugin, pluginName))
				}
			}
		}
	}

	if len(conflicts) > 0 {
		for _, conflict := range conflicts {
			log.Warnf("Port conflict: %s", conflict)
		}
		return fmt.Errorf("%d port conflicts detected (remote config takes precedence)", len(conflicts))
	}

	return nil
}

// validateCacheFilePath validates and sanitizes the cache file path to prevent path traversal
func (s *ConfigPollingService) validateCacheFilePath() (string, error) {
	// Create filename from tenant and instance ID (already validated in config)
	filename := fmt.Sprintf("%s-%s.json", s.cfg.TenantID, s.instanceID)

	// Use filepath.Join to construct path (prevents path traversal)
	cacheFile := filepath.Join(s.cacheDirectory, filename)

	// Clean the path to remove any relative path components
	cacheFile = filepath.Clean(cacheFile)

	// Get absolute paths for comparison
	absCache, err := filepath.Abs(s.cacheDirectory)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute cache directory: %w", err)
	}

	absFile, err := filepath.Abs(cacheFile)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute file path: %w", err)
	}

	// Verify the file path is within the cache directory using Rel
	relPath, err := filepath.Rel(absCache, absFile)
	if err != nil {
		return "", fmt.Errorf("failed to get relative path: %w", err)
	}

	// If the relative path starts with "..", it's outside the cache directory
	if strings.HasPrefix(relPath, "..") {
		return "", fmt.Errorf("cache file path outside of cache directory")
	}

	return cacheFile, nil
}

// cacheConfiguration saves configuration to disk
func (s *ConfigPollingService) cacheConfiguration(remoteConfig *ControlProxyConfig) error {
	cacheFile, err := s.validateCacheFilePath()
	if err != nil {
		return fmt.Errorf("invalid cache file path: %w", err)
	}

	data, err := json.MarshalIndent(remoteConfig, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(cacheFile, data, 0600); err != nil {
		return fmt.Errorf("failed to write cache file: %w", err)
	}

	log.Debugf("Configuration cached to %s", cacheFile)
	return nil
}

// loadCachedConfig loads configuration from disk cache
func (s *ConfigPollingService) loadCachedConfig() error {
	cacheFile, err := s.validateCacheFilePath()
	if err != nil {
		return fmt.Errorf("invalid cache file path: %w", err)
	}

	// #nosec G304 - cacheFile is validated via validateCacheFilePath()
	data, err := os.ReadFile(cacheFile)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("no cached config found")
		}
		return fmt.Errorf("failed to read cache file: %w", err)
	}

	var cachedConfig ControlProxyConfig
	if err := json.Unmarshal(data, &cachedConfig); err != nil {
		return fmt.Errorf("failed to unmarshal cached config: %w", err)
	}

	s.configMutex.Lock()
	s.currentConfig = &cachedConfig
	s.currentConfigHash = cachedConfig.ConfigHash
	s.configMutex.Unlock()

	log.Infof("Loaded cached configuration (version %d) from %s", cachedConfig.ConfigVersion, cacheFile)
	return nil
}

// reportConfigApplied reports to control that config was applied
func (s *ConfigPollingService) reportConfigApplied(configVersion int) error {
	url := fmt.Sprintf("%s/api/v2/proxies/%s/config/applied", s.controlURL, s.instanceID)

	payload := map[string]interface{}{
		"tenant_id":      s.cfg.TenantID,
		"config_version": configVersion,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if s.bearerToken != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", s.bearerToken))
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to report config applied: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("control returned status %d: %s", resp.StatusCode, string(body))
	}

	log.Debugf("Reported config version %d as applied to control", configVersion)
	return nil
}

// GetCurrentConfig returns the current config (thread-safe)
func (s *ConfigPollingService) GetCurrentConfig() *ControlProxyConfig {
	s.configMutex.RLock()
	defer s.configMutex.RUnlock()
	return s.currentConfig
}

// getConfigVersion returns current config version (thread-safe)
func (s *ConfigPollingService) getConfigVersion() int {
	s.configMutex.RLock()
	defer s.configMutex.RUnlock()
	if s.currentConfig == nil {
		return 0
	}
	return s.currentConfig.ConfigVersion
}

// CalculateConfigHash calculates SHA256 hash of plugin configs and proxy settings
func CalculateConfigHash(pluginConfigs []plugins.PluginConfig, proxySettings interface{}) string {
	combined := struct {
		PluginConfigs []plugins.PluginConfig `json:"plugin_configs"`
		ProxySettings interface{}            `json:"proxy_settings"`
	}{
		PluginConfigs: pluginConfigs,
		ProxySettings: proxySettings,
	}

	data, _ := json.Marshal(combined)
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

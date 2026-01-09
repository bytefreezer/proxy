// Licensed under Elastic License 2.0
// See LICENSE.txt for details

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

	"github.com/bytedance/sonic"
	"github.com/bytefreezer/goodies/log"
	"github.com/bytefreezer/proxy/config"
	"github.com/bytefreezer/proxy/plugins"
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
	Receiver     ReceiverSettings       `json:"receiver,omitempty"`
	Batching     BatchingSettings       `json:"batching,omitempty"`
	Spooling     SpoolingSettings       `json:"spooling,omitempty"`
	Housekeeping HousekeepingSettings   `json:"housekeeping,omitempty"`
	Otel         OtelSettings           `json:"otel,omitempty"`
	SOC          SOCSettings            `json:"soc,omitempty"`
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

// ReceiverInfo represents receiver capacity information from control
type ReceiverInfo struct {
	InstanceID     string `json:"instance_id"`
	InstanceAPI    string `json:"instance_api"`
	Status         string `json:"status"`
	MaxPayloadSize int64  `json:"max_payload_size"`
}

// ControlConfiguration represents the full configuration from control (account-based polling)
type ControlConfiguration struct {
	Tenants   []TenantWithDatasets `json:"tenants"`
	Count     int                  `json:"count"`
	Receivers []ReceiverInfo       `json:"receivers,omitempty"`
}

// TenantWithDatasets represents a tenant and its datasets
type TenantWithDatasets struct {
	Tenant   Tenant    `json:"tenant"`
	Datasets []Dataset `json:"datasets"`
}

// Tenant represents a tenant from control
type Tenant struct {
	ID          string                 `json:"id"`
	AccountID   string                 `json:"account_id"`
	Name        string                 `json:"name"`
	DisplayName string                 `json:"display_name"`
	Description string                 `json:"description"`
	Active      bool                   `json:"active"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
	Config      map[string]interface{} `json:"config"`
}

// Dataset represents a dataset from control
type Dataset struct {
	ID          string        `json:"id"`
	TenantID    string        `json:"tenant_id"`
	Name        string        `json:"name"`
	DisplayName string        `json:"display_name"`
	Description string        `json:"description"`
	Active      bool          `json:"active"`
	Status      string        `json:"status"`
	CreatedAt   time.Time     `json:"created_at"`
	UpdatedAt   time.Time     `json:"updated_at"`
	Config      DatasetConfig `json:"config"`
}

// DatasetConfig represents dataset configuration from control
type DatasetConfig struct {
	Source      SourceConfig           `json:"source"`
	Destination map[string]interface{} `json:"destination"`
	Custom      map[string]interface{} `json:"custom,omitempty"`
}

// SourceConfig represents dataset source configuration
type SourceConfig struct {
	Type   string                 `json:"type"`   // stream, api, file, database
	Custom map[string]interface{} `json:"custom"` // Plugin-specific configuration
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

	ticker            *time.Ticker
	stopChan          chan struct{}
	httpClient        *http.Client
	onConfigChange    func(*ControlProxyConfig) error // Callback for config changes
	onBatchSizeChange func(int64)                     // Callback when batch size is adjusted

	// Exponential backoff and circuit breaker
	consecutiveFailures int
	currentBackoff      time.Duration
	maxBackoff          time.Duration
	backoffMutex        sync.RWMutex
	circuitOpen         bool
	lastAuthError       time.Time
}

// NewConfigPollingService creates a new configuration polling service
func NewConfigPollingService(cfg *config.Config, onConfigChange func(*ControlProxyConfig) error) (*ConfigPollingService, error) {
	if cfg.ControlURL == "" {
		return nil, fmt.Errorf("control_url is required for config polling")
	}

	// Get instance ID from hostname
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
		onConfigChange:      onConfigChange,
		maxBackoff:          30 * time.Minute, // Maximum backoff time for inactive accounts
		consecutiveFailures: 0,
		currentBackoff:      0,
		circuitOpen:         false,
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

	// Determine polling mode
	if s.cfg.AccountID != "" {
		// Account-based polling (new): fetch all tenants + datasets for account
		return s.pollAccountConfiguration()
	}

	// Tenant-based polling (fallback when no account_id configured)
	return s.pollTenantConfiguration()
}

// pollAccountConfiguration fetches tenants + datasets for an account and converts to plugin configs
func (s *ConfigPollingService) pollAccountConfiguration() error {
	// Check if we should skip due to backoff
	if s.shouldBackoff() {
		log.Debugf("Skipping poll due to backoff (next retry in %v)", s.getEffectiveInterval())
		return nil
	}

	url := fmt.Sprintf("%s/api/v1/proxy/config?account_id=%s&proxy_instance_id=%s", s.controlURL, s.cfg.AccountID, s.instanceID)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	if s.bearerToken != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", s.bearerToken))
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		if s.cfg.ConfigPolling.RetryOnError {
			log.Warnf("Control unreachable, using cached config: %v", err)
			return s.loadCachedConfig()
		}
		return fmt.Errorf("failed to poll control: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)

		// Record failure for auth errors and account not found
		s.recordFailure(resp.StatusCode)

		// Try cached config for some error types
		if resp.StatusCode == http.StatusUnauthorized ||
			resp.StatusCode == http.StatusForbidden ||
			resp.StatusCode == http.StatusNotFound {
			log.Warnf("Control returned status %d (inactive/unauthorized account), using cached config if available", resp.StatusCode)
			_ = s.loadCachedConfig() // Ignore error, we'll return the original error
		}

		return fmt.Errorf("control returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse tenants + datasets response
	var controlConfig ControlConfiguration
	if err := sonic.ConfigDefault.NewDecoder(resp.Body).Decode(&controlConfig); err != nil {
		return fmt.Errorf("failed to decode config response: %w", err)
	}

	log.Infof("Received configuration for account %s: %d tenants, %d total datasets, %d receivers",
		s.cfg.AccountID, controlConfig.Count, s.countTotalDatasets(controlConfig), len(controlConfig.Receivers))

	// Apply receiver capacity limits if available
	s.applyReceiverCapacityLimits(controlConfig.Receivers)

	// Record successful poll (resets backoff)
	s.recordSuccess()

	// Convert datasets to plugin configs
	pluginConfigs := s.datasetsToPluginConfigs(controlConfig)

	// Create synthetic ControlProxyConfig for compatibility with existing code
	configVersion := int(time.Now().Unix())
	remoteConfig := ControlProxyConfig{
		PluginConfigs: pluginConfigs,
		ConfigVersion: configVersion,
		ConfigHash:    s.calculatePluginConfigsHash(pluginConfigs),
	}

	// Check if config has changed
	if s.hasConfigChanged(&remoteConfig) {
		log.Infof("Configuration changed, applying %d plugin configs", len(pluginConfigs))

		// Apply the new configuration
		if err := s.applyConfiguration(&remoteConfig); err != nil {
			log.Errorf("Failed to apply configuration: %v", err)
			return err
		}

		// Cache the new configuration
		if err := s.cacheConfiguration(&remoteConfig); err != nil {
			log.Errorf("Failed to cache configuration: %v", err)
		}

		// Report back to control for each tenant
		for _, tenantWithDatasets := range controlConfig.Tenants {
			if err := s.reportConfigAppliedForTenant(tenantWithDatasets.Tenant.ID, configVersion, pluginConfigs); err != nil {
				log.Errorf("Failed to report config applied for tenant %s: %v", tenantWithDatasets.Tenant.ID, err)
			}
		}
	} else {
		log.Debugf("Configuration unchanged (hash %s)", remoteConfig.ConfigHash)
	}

	return nil
}

// pollTenantConfiguration fetches config for a single tenant (fallback mode)
func (s *ConfigPollingService) pollTenantConfiguration() error {
	// Check if we should skip due to backoff
	if s.shouldBackoff() {
		log.Debugf("Skipping poll due to backoff (next retry in %v)", s.getEffectiveInterval())
		return nil
	}

	url := fmt.Sprintf("%s/api/v1/proxies/%s/config?tenant_id=%s", s.controlURL, s.instanceID, s.cfg.TenantID)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	if s.bearerToken != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", s.bearerToken))
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		if s.cfg.ConfigPolling.RetryOnError {
			log.Warnf("Control unreachable, using cached config: %v", err)
			return s.loadCachedConfig()
		}
		return fmt.Errorf("failed to poll control: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		log.Warnf("Proxy instance not configured in control yet (404)")
		s.recordFailure(resp.StatusCode)
		return nil
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)

		// Record failure for auth errors
		s.recordFailure(resp.StatusCode)

		// Try cached config for some error types
		if resp.StatusCode == http.StatusUnauthorized ||
			resp.StatusCode == http.StatusForbidden {
			log.Warnf("Control returned status %d (unauthorized tenant), using cached config if available", resp.StatusCode)
			_ = s.loadCachedConfig() // Ignore error, we'll return the original error
		}

		return fmt.Errorf("control returned status %d: %s", resp.StatusCode, string(body))
	}

	var remoteConfig ControlProxyConfig
	if err := sonic.ConfigDefault.NewDecoder(resp.Body).Decode(&remoteConfig); err != nil {
		return fmt.Errorf("failed to decode config response: %w", err)
	}

	// Record successful poll (resets backoff)
	s.recordSuccess()

	if s.hasConfigChanged(&remoteConfig) {
		log.Infof("Configuration changed (version %d -> %d)", s.getConfigVersion(), remoteConfig.ConfigVersion)

		if err := s.applyConfiguration(&remoteConfig); err != nil {
			log.Errorf("Failed to apply configuration: %v", err)
			return err
		}

		if err := s.cacheConfiguration(&remoteConfig); err != nil {
			log.Errorf("Failed to cache configuration: %v", err)
		}

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
		log.Debugf("Config change detection: no current config (first poll)")
		return true // No current config, definitely changed
	}

	// Compare config hashes for change detection
	// Hash is calculated deterministically from plugin configs using encoding/json
	if remoteConfig.ConfigHash != s.currentConfigHash {
		log.Debugf("Config change detection: hash mismatch (remote=%s, current=%s)",
			remoteConfig.ConfigHash, s.currentConfigHash)
		return true
	}

	// Hash matches - config unchanged
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

	data, err := sonic.MarshalIndent(remoteConfig, "", "  ")
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
	if err := sonic.Unmarshal(data, &cachedConfig); err != nil {
		return fmt.Errorf("failed to unmarshal cached config: %w", err)
	}

	log.Infof("Loaded cached configuration (version %d) from %s", cachedConfig.ConfigVersion, cacheFile)

	// Apply the cached configuration to actually start the plugins
	// This is critical - without this, plugins won't start until config changes
	if len(cachedConfig.PluginConfigs) > 0 {
		log.Infof("Applying cached configuration: %d plugin configs", len(cachedConfig.PluginConfigs))
		if err := s.applyConfiguration(&cachedConfig); err != nil {
			return fmt.Errorf("failed to apply cached configuration: %w", err)
		}
	} else {
		// No plugins to apply, just store the config state
		s.configMutex.Lock()
		s.currentConfig = &cachedConfig
		s.currentConfigHash = cachedConfig.ConfigHash
		s.configMutex.Unlock()
	}

	return nil
}

// reportConfigApplied reports to control that config was applied
func (s *ConfigPollingService) reportConfigApplied(configVersion int) error {
	url := fmt.Sprintf("%s/api/v1/proxies/%s/config/applied", s.controlURL, s.instanceID)

	payload := map[string]interface{}{
		"tenant_id":      s.cfg.TenantID,
		"config_version": configVersion,
	}

	data, err := sonic.Marshal(payload)
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

	data, _ := sonic.Marshal(combined)
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// countTotalDatasets counts total datasets across all tenants
func (s *ConfigPollingService) countTotalDatasets(config ControlConfiguration) int {
	total := 0
	for _, tenant := range config.Tenants {
		total += len(tenant.Datasets)
	}
	return total
}

// datasetsToPluginConfigs converts datasets from control to plugin configurations
func (s *ConfigPollingService) datasetsToPluginConfigs(config ControlConfiguration) []map[string]interface{} {
	pluginConfigs := []map[string]interface{}{}

	for _, tenantWithDatasets := range config.Tenants {
		tenant := tenantWithDatasets.Tenant

		for _, dataset := range tenantWithDatasets.Datasets {
			// Only process active datasets
			if !dataset.Active {
				continue
			}

			// Skip datasets without a source type
			if dataset.Config.Source.Type == "" {
				log.Debugf("Dataset %s has no source.type, skipping", dataset.ID)
				continue
			}

			// Extract plugin configuration from dataset source fields
			pluginConfig := s.datasetToPluginConfig(tenant, dataset)
			if pluginConfig != nil {
				pluginConfigs = append(pluginConfigs, pluginConfig)
			}
		}
	}

	log.Infof("Converted %d datasets to %d plugin configs", s.countTotalDatasets(config), len(pluginConfigs))
	return pluginConfigs
}

// datasetToPluginConfig converts a single dataset to a plugin configuration
func (s *ConfigPollingService) datasetToPluginConfig(tenant Tenant, dataset Dataset) map[string]interface{} {
	// Use source.type as the plugin type (e.g., "ebpf", "sflow", "netflow")
	// Example dataset structure:
	// {
	//   "source": {
	//     "type": "ebpf",
	//     "custom": {
	//       "host": "0.0.0.0",
	//       "port": 2056,
	//       "worker_count": 4,
	//       "read_buffer_size": 65536
	//     }
	//   }
	// }

	pluginType := dataset.Config.Source.Type
	if pluginType == "" {
		log.Warnf("Dataset %s has empty source.type, skipping", dataset.ID)
		return nil
	}

	// Build plugin config
	pluginConfig := map[string]interface{}{
		"type": pluginType,
		"name": fmt.Sprintf("%s-%s", dataset.Name, tenant.Name),
		"config": map[string]interface{}{
			"tenant_id":    tenant.ID,
			"dataset_id":   dataset.ID,
			"bearer_token": s.bearerToken,
		},
	}

	// Copy all custom fields to plugin config if they exist
	config := pluginConfig["config"].(map[string]interface{})
	if dataset.Config.Source.Custom != nil {
		for key, value := range dataset.Config.Source.Custom {
			config[key] = value
		}
	}

	// Set defaults if not provided
	if _, ok := config["host"]; !ok {
		config["host"] = "0.0.0.0"
	}

	log.Infof("Created plugin config for dataset %s (tenant %s): type=%s, port=%v",
		dataset.ID, tenant.ID, pluginType, config["port"])

	return pluginConfig
}

// calculatePluginConfigsHash calculates hash for plugin configs
// Uses encoding/json which produces sorted keys for deterministic output
func (s *ConfigPollingService) calculatePluginConfigsHash(pluginConfigs []map[string]interface{}) string {
	// encoding/json.Marshal sorts map keys alphabetically by default
	// This ensures deterministic output regardless of map iteration order
	data, _ := json.Marshal(pluginConfigs)
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// reportConfigAppliedForTenant reports to control that config was applied for a specific tenant
func (s *ConfigPollingService) reportConfigAppliedForTenant(tenantID string, configVersion int, pluginConfigs []map[string]interface{}) error {
	// Filter plugin configs for this tenant
	tenantPluginConfigs := []map[string]interface{}{}
	for _, pc := range pluginConfigs {
		if configMap, ok := pc["config"].(map[string]interface{}); ok {
			if tid, ok := configMap["tenant_id"].(string); ok && tid == tenantID {
				tenantPluginConfigs = append(tenantPluginConfigs, pc)
			}
		}
	}

	// Build report payload
	reportPayload := map[string]interface{}{
		"instance_id":    s.instanceID,
		"tenant_id":      tenantID,
		"instance_api":   fmt.Sprintf("%s:%d", s.instanceID, s.cfg.Server.ApiPort),
		"config_mode":    s.cfg.ConfigMode,
		"plugin_configs": tenantPluginConfigs,
		"proxy_settings": map[string]interface{}{}, // Empty for now
	}

	url := fmt.Sprintf("%s/api/v1/proxies/%s/config", s.controlURL, s.instanceID)
	payload, err := sonic.Marshal(reportPayload)
	if err != nil {
		return fmt.Errorf("failed to marshal report payload: %w", err)
	}

	req, err := http.NewRequest("PUT", url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("failed to create report request: %w", err)
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

	// Parse response to get the actual config_version assigned by control
	var response struct {
		ConfigVersion int `json:"config_version"`
	}
	if err := sonic.ConfigDefault.NewDecoder(resp.Body).Decode(&response); err != nil {
		return fmt.Errorf("failed to parse upsert response: %w", err)
	}

	log.Infof("Reported config to control for tenant %s: %d plugin configs (version %d)", tenantID, len(tenantPluginConfigs), response.ConfigVersion)

	// Now mark this config version as applied, using the version returned from control
	return s.markConfigAppliedForTenant(tenantID, response.ConfigVersion)
}

// markConfigAppliedForTenant marks a configuration version as applied for a specific tenant
func (s *ConfigPollingService) markConfigAppliedForTenant(tenantID string, configVersion int) error {
	url := fmt.Sprintf("%s/api/v1/proxies/%s/config/applied", s.controlURL, s.instanceID)

	payload := map[string]interface{}{
		"tenant_id":      tenantID,
		"config_version": configVersion,
	}

	data, err := sonic.Marshal(payload)
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
		return fmt.Errorf("failed to mark config applied: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("control returned status %d: %s", resp.StatusCode, string(body))
	}

	log.Debugf("Marked config version %d as applied for tenant %s", configVersion, tenantID)
	return nil
}

// shouldBackoff checks if we're currently in a backoff period
func (s *ConfigPollingService) shouldBackoff() bool {
	s.backoffMutex.RLock()
	defer s.backoffMutex.RUnlock()

	if s.circuitOpen {
		// Check if enough time has passed to retry
		if time.Since(s.lastAuthError) < s.currentBackoff {
			return true
		}
		// Circuit breaker timeout expired, allow retry
		return false
	}

	return false
}

// recordFailure records a polling failure and calculates exponential backoff
func (s *ConfigPollingService) recordFailure(statusCode int) {
	s.backoffMutex.Lock()
	defer s.backoffMutex.Unlock()

	// Only apply backoff for authentication errors (401, 403) or not found (404)
	// These indicate inactive/invalid accounts that shouldn't hammer Control
	if statusCode != http.StatusUnauthorized &&
		statusCode != http.StatusForbidden &&
		statusCode != http.StatusNotFound {
		return
	}

	s.consecutiveFailures++
	s.lastAuthError = time.Now()

	// Exponential backoff: 1min → 2min → 4min → 8min → 16min → 30min (max)
	if s.consecutiveFailures == 1 {
		s.currentBackoff = 1 * time.Minute
	} else {
		s.currentBackoff = s.currentBackoff * 2
		if s.currentBackoff > s.maxBackoff {
			s.currentBackoff = s.maxBackoff
		}
	}

	// Open circuit breaker after 5 consecutive auth failures
	if s.consecutiveFailures >= 5 {
		s.circuitOpen = true
		log.Warnf("Circuit breaker opened after %d consecutive auth failures (backoff: %v)",
			s.consecutiveFailures, s.currentBackoff)
	} else {
		log.Warnf("Polling failed (status %d), backing off for %v (failure %d/5)",
			statusCode, s.currentBackoff, s.consecutiveFailures)
	}
}

// recordSuccess resets backoff counters after successful poll
func (s *ConfigPollingService) recordSuccess() {
	s.backoffMutex.Lock()
	defer s.backoffMutex.Unlock()

	if s.consecutiveFailures > 0 || s.circuitOpen {
		log.Infof("Polling succeeded, resetting backoff (was: %d failures, %v backoff)",
			s.consecutiveFailures, s.currentBackoff)
	}

	s.consecutiveFailures = 0
	s.currentBackoff = 0
	s.circuitOpen = false
}

// getEffectiveInterval returns the polling interval to use (normal or backoff)
func (s *ConfigPollingService) getEffectiveInterval() time.Duration {
	s.backoffMutex.RLock()
	defer s.backoffMutex.RUnlock()

	if s.circuitOpen && s.currentBackoff > 0 {
		return s.currentBackoff
	}
	return s.pollingInterval
}

// TriggerPoll triggers an immediate configuration poll from Control Service
// Returns error if poll fails, nil on success
func (s *ConfigPollingService) TriggerPoll() error {
	log.Info("Manual configuration poll triggered via API")
	return s.pollConfiguration()
}

// SetBatchSizeChangeCallback sets a callback to be called when batch size is adjusted
func (s *ConfigPollingService) SetBatchSizeChangeCallback(callback func(int64)) {
	s.onBatchSizeChange = callback
}

// applyReceiverCapacityLimits adjusts batch size based on receiver capacity
// This prevents HTTP 413 (Payload Too Large) errors when receiver has smaller limits
func (s *ConfigPollingService) applyReceiverCapacityLimits(receivers []ReceiverInfo) {
	if len(receivers) == 0 {
		log.Debugf("No receiver capacity info received from control")
		return
	}

	// Get current batch max_bytes
	currentMaxBytes := s.cfg.Batching.MaxBytes
	if currentMaxBytes <= 0 {
		currentMaxBytes = 1048576 // Default 1MB
	}

	// Find the matching receiver based on configured receiver URL
	// Match using instance_api (hostname:port format)
	receiverURL := strings.TrimPrefix(s.cfg.Receiver.BaseURL, "http://")
	receiverURL = strings.TrimPrefix(receiverURL, "https://")
	receiverURL = strings.TrimSuffix(receiverURL, "/")

	// Extract port from receiver URL for fallback matching
	var receiverPort string
	if idx := strings.LastIndex(receiverURL, ":"); idx != -1 {
		receiverPort = receiverURL[idx+1:]
	}

	var matchedReceiver *ReceiverInfo
	for i := range receivers {
		// Match by instance_api (e.g., "tp1:8080" matches receiver URL "http://tp1:8080")
		if strings.Contains(receiverURL, receivers[i].InstanceAPI) ||
			strings.Contains(receivers[i].InstanceAPI, receiverURL) {
			matchedReceiver = &receivers[i]
			break
		}
		// Fallback: match by port number (handles IP vs hostname mismatch)
		if receiverPort != "" && strings.HasSuffix(receivers[i].InstanceAPI, ":"+receiverPort) {
			matchedReceiver = &receivers[i]
			log.Debugf("Matched receiver %s by port %s (URL: %s)", receivers[i].InstanceID, receiverPort, receiverURL)
			break
		}
	}

	if matchedReceiver == nil {
		// No exact match - use the smallest max_payload_size from all receivers as a safe default
		var minPayloadSize int64 = 0
		for _, r := range receivers {
			if minPayloadSize == 0 || r.MaxPayloadSize < minPayloadSize {
				minPayloadSize = r.MaxPayloadSize
			}
		}
		if minPayloadSize > 0 {
			threshold := int64(float64(minPayloadSize) * 0.95)
			if currentMaxBytes >= threshold {
				// Config is within 5% of min receiver limit - apply 10% margin
				effectiveLimit := int64(float64(minPayloadSize) * 0.90)
				log.Warnf("No matching receiver found for URL %s - using minimum receiver limit with 10%% margin: %d bytes (was %d)",
					s.cfg.Receiver.BaseURL, effectiveLimit, currentMaxBytes)
				s.cfg.Batching.MaxBytes = effectiveLimit
			}
		}
		return
	}

	// Check if receiver's limit requires adjustment
	// If config is within 5% of receiver limit, apply 10% margin for safety
	// This catches edge cases where config and receiver are nearly equal
	if matchedReceiver.MaxPayloadSize > 0 {
		threshold := int64(float64(matchedReceiver.MaxPayloadSize) * 0.95) // 5% margin threshold
		if currentMaxBytes >= threshold {
			// Config is within 5% of receiver limit - apply 10% margin
			effectiveLimit := int64(float64(matchedReceiver.MaxPayloadSize) * 0.90)
			log.Infof("Adjusting batch max_bytes from %d to %d based on receiver %s capacity (max_payload_size: %d, 10%% margin)",
				currentMaxBytes, effectiveLimit, matchedReceiver.InstanceID, matchedReceiver.MaxPayloadSize)
			s.cfg.Batching.MaxBytes = effectiveLimit
			// Notify health reporting of the change
			if s.onBatchSizeChange != nil {
				s.onBatchSizeChange(effectiveLimit)
			}
		} else {
			log.Debugf("Batch max_bytes (%d) well under receiver %s capacity (%d), no adjustment needed",
				currentMaxBytes, matchedReceiver.InstanceID, matchedReceiver.MaxPayloadSize)
		}
	}
}

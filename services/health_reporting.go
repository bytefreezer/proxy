// Licensed under Elastic License 2.0
// See LICENSE.txt for details

package services

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"sync/atomic"
	"time"

	"github.com/bytedance/sonic"
	"github.com/bytefreezer/goodies/log"
	"github.com/bytefreezer/proxy/domain"
	"github.com/bytefreezer/proxy/plugins"
	"github.com/bytefreezer/proxy/utils"
)

// PortDatasetInfo contains dataset info for a specific port
type PortDatasetInfo struct {
	Port      int
	TenantID  string
	DatasetID string
}

// UDPPortsProvider is an interface for getting active UDP ports
type UDPPortsProvider interface {
	GetUDPPorts() []int
	GetPortDatasetMap() []PortDatasetInfo
}

// PluginDetail contains summary info about an active plugin for health reporting
type PluginDetail struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	TenantID  string `json:"tenant_id,omitempty"`
	DatasetID string `json:"dataset_id,omitempty"`
}

// PluginHealthProvider is an interface for checking plugin health
type PluginHealthProvider interface {
	GetPluginCount() int
	GetExpectedPluginCount() int
	GetPluginDetails() []PluginDetail
}

// HealthReportingService handles health reporting to the control service
type HealthReportingService struct {
	controlURL           string
	accountID            string
	bearerToken          string
	serviceType          string
	instanceID           string
	instanceAPI          string
	reportInterval       time.Duration
	timeout              time.Duration
	httpClient           *http.Client
	enabled              bool
	stopChan             chan bool
	config               map[string]interface{} // Full configuration data to report
	diskPath             string                 // Path to monitor for disk metrics (defaults to /)
	proxyStats           *domain.ProxyStats     // Pointer to proxy stats for throughput metrics
	startTime            time.Time              // Service start time for uptime calculation
	udpPortsProvider     UDPPortsProvider       // Provider for getting active UDP ports
	pluginHealthProvider PluginHealthProvider   // Provider for checking plugin health
	lastUDPDrops         int64                  // Track previous UDP drops to detect new drops
	lastPortDrops        map[int]int64          // Track drops per port to detect new drops
	errorReporter        ErrorReporter          // Error reporter for reporting per-dataset errors
}

// ErrorReporter is an interface for reporting errors to control
type ErrorReporter interface {
	ReportWarning(ctx context.Context, errorType, errorMessage string, tenantID, datasetID string) error
	ResolveErrorsByType(ctx context.Context, errorType, datasetID string) error
}

// ServiceRegistrationRequest represents a service registration request
type ServiceRegistrationRequest struct {
	ServiceType   string                 `json:"service_type"`
	InstanceID    string                 `json:"instance_id,omitempty"`
	InstanceAPI   string                 `json:"instance_api"`
	Status        string                 `json:"status,omitempty"`
	Configuration map[string]interface{} `json:"configuration,omitempty"`
}

// ServiceRegistrationResponse represents a service registration response
type ServiceRegistrationResponse struct {
	Success     bool   `json:"success"`
	Message     string `json:"message"`
	InstanceID  string `json:"instance_id"`
	ServiceType string `json:"service_type"`
}

// HealthReportRequest represents a health report request
type HealthReportRequest struct {
	ServiceName   string                 `json:"service_name"`
	ServiceID     string                 `json:"service_id"`
	InstanceAPI   string                 `json:"instance_api"`
	Healthy       bool                   `json:"healthy"`
	Status        string                 `json:"status,omitempty"` // "healthy", "degraded", "unhealthy"
	Configuration map[string]interface{} `json:"configuration,omitempty"`
	Metrics       map[string]interface{} `json:"metrics,omitempty"`
}

// HealthReportResponse represents a health report response
type HealthReportResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// NewHealthReportingService creates a new health reporting service
func NewHealthReportingService(controlURL, accountID, bearerToken, serviceType, instanceID, instanceAPI string, reportInterval, timeout time.Duration, config map[string]interface{}) *HealthReportingService {
	// Use provided instanceID or fall back to hostname
	if instanceID == "" {
		var err error
		instanceID, err = os.Hostname()
		if err != nil {
			instanceID = "unknown"
		}
	}

	return &HealthReportingService{
		controlURL:     controlURL,
		accountID:      accountID,
		bearerToken:    bearerToken,
		serviceType:    serviceType,
		instanceID:     instanceID,
		instanceAPI:    instanceAPI,
		reportInterval: reportInterval,
		timeout:        timeout,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		enabled:       true,
		stopChan:      make(chan bool),
		config:        config,
		diskPath:      "/", // Default to root filesystem
		startTime:     time.Now(),
		lastPortDrops: make(map[int]int64),
	}
}

// SetDiskPath sets the disk path to monitor for disk metrics
func (h *HealthReportingService) SetDiskPath(path string) {
	h.diskPath = path
}

// UpdateConfiguration updates the configuration map for health reporting
// This should be called when runtime configuration changes (e.g., receiver capacity adjustment)
func (h *HealthReportingService) UpdateConfiguration(key string, value interface{}) {
	if h.config == nil {
		return
	}
	h.config[key] = value
}

// UpdateBatchingConfig updates the batching configuration in health reports
func (h *HealthReportingService) UpdateBatchingConfig(maxBytes int64) {
	if h.config == nil {
		return
	}
	if batching, ok := h.config["batching"].(map[string]interface{}); ok {
		batching["max_bytes"] = maxBytes
	}
}

// SetProxyStats sets the proxy stats pointer for throughput metrics
func (h *HealthReportingService) SetProxyStats(stats *domain.ProxyStats) {
	h.proxyStats = stats
}

// Start begins health reporting
func (h *HealthReportingService) Start() {
	if !h.enabled {
		log.Info("Health reporting is disabled")
		return
	}

	log.Infof("Starting health reporting service - reporting to %s every %v", h.controlURL, h.reportInterval)

	// Register service on startup
	if err := h.RegisterService(); err != nil {
		log.Warnf("Failed to register service on startup: %v", err)
	}

	// Start reporting goroutine
	go h.reportingLoop()
}

// Stop stops health reporting and deregisters from control
func (h *HealthReportingService) Stop() {
	if h.enabled {
		close(h.stopChan)
		if err := h.Deregister(); err != nil {
			log.Warnf("Failed to deregister service on shutdown: %v", err)
		}
		log.Info("Health reporting service stopped")
	}
}

// Deregister removes this service instance from the control service
func (h *HealthReportingService) Deregister() error {
	if !h.enabled || h.controlURL == "" {
		return nil
	}

	accountID := h.accountID
	if accountID == "" {
		accountID = "system"
	}

	url := fmt.Sprintf("%s/api/v1/accounts/%s/services/%s", h.controlURL, accountID, h.instanceID)
	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create deregister request: %w", err)
	}

	if h.bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+h.bearerToken)
	}

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to deregister service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("service deregistration failed with status %d", resp.StatusCode)
	}

	log.Infof("Successfully deregistered service %s (instance %s)", h.serviceType, h.instanceID)
	return nil
}

// RegisterService registers this service instance with the control service
func (h *HealthReportingService) RegisterService() error {
	if !h.enabled {
		return nil
	}

	registrationReq := ServiceRegistrationRequest{
		ServiceType:   h.serviceType,
		InstanceID:    h.instanceID,
		InstanceAPI:   h.instanceAPI,
		Status:        "Starting",
		Configuration: h.config, // Use full configuration data
	}

	reqBody, err := sonic.Marshal(registrationReq)
	if err != nil {
		return fmt.Errorf("failed to marshal registration request: %w", err)
	}

	// Use account-scoped registration endpoint
	url := fmt.Sprintf("%s/api/v1/accounts/%s/services/register", h.controlURL, h.accountID)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(reqBody))
	if err != nil {
		return fmt.Errorf("failed to create registration request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if h.bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+h.bearerToken)
	}

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to register service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("service registration failed with status %d", resp.StatusCode)
	}

	var registrationResp ServiceRegistrationResponse
	if err := sonic.ConfigDefault.NewDecoder(resp.Body).Decode(&registrationResp); err != nil {
		return fmt.Errorf("failed to decode registration response: %w", err)
	}

	if !registrationResp.Success {
		return fmt.Errorf("service registration failed: %s", registrationResp.Message)
	}

	log.Infof("Successfully registered service %s with instance ID %s", h.serviceType, registrationResp.InstanceID)
	return nil
}

// SendHealthReport sends a health report to the control service
func (h *HealthReportingService) SendHealthReport(healthy bool, status string, configuration map[string]interface{}, metrics map[string]interface{}) error {
	if !h.enabled {
		return nil
	}

	healthReq := HealthReportRequest{
		ServiceName:   h.serviceType,
		ServiceID:     h.instanceID,
		InstanceAPI:   h.instanceAPI,
		Healthy:       healthy,
		Status:        status,
		Configuration: configuration,
		Metrics:       metrics,
	}

	reqBody, err := sonic.Marshal(healthReq)
	if err != nil {
		return fmt.Errorf("failed to marshal health report: %w", err)
	}

	// Use account-scoped health reporting endpoint
	url := fmt.Sprintf("%s/api/v1/accounts/%s/services/report", h.controlURL, h.accountID)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(reqBody))
	if err != nil {
		return fmt.Errorf("failed to create health report request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if h.bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+h.bearerToken)
	}

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send health report: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health report failed with status %d", resp.StatusCode)
	}

	log.Debugf("Successfully sent health report for %s (healthy: %v)", h.serviceType, healthy)
	return nil
}

// reportingLoop runs the periodic health reporting
func (h *HealthReportingService) reportingLoop() {
	ticker := time.NewTicker(h.reportInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Generate health report with configuration and metrics
			healthy := h.isServiceHealthy()
			configuration := h.config // Configuration updated on every report

			// Inject current plugin/dataset details into configuration
			if h.pluginHealthProvider != nil {
				details := h.pluginHealthProvider.GetPluginDetails()
				configuration["assigned_datasets"] = len(details)
				configuration["plugins"] = details
			}

			metrics := h.generateMetrics()

			// Determine status based on health and UDP drops
			status := h.determineStatus(healthy, metrics)

			if err := h.SendHealthReport(healthy, status, configuration, metrics); err != nil {
				log.Warnf("Failed to send health report: %v", err)
			}

		case <-h.stopChan:
			return
		}
	}
}

// determineStatus determines the service status based on health and metrics
func (h *HealthReportingService) determineStatus(healthy bool, metrics map[string]interface{}) string {
	if !healthy {
		// Check if unhealthy due to no plugins running
		if h.pluginHealthProvider != nil {
			running := h.pluginHealthProvider.GetPluginCount()
			expected := h.pluginHealthProvider.GetExpectedPluginCount()
			if expected > 0 && running == 0 {
				// Add context to metrics about why we're unhealthy
				metrics["plugins_running"] = running
				metrics["plugins_expected"] = expected
				metrics["unhealthy_reason"] = "no_plugins_running"
			}
		}
		return "unhealthy"
	}

	// Check for UDP socket drops - if drops increased since last check, we're degraded
	if drops, ok := metrics["udp_socket_drops_total"].(int64); ok && drops > 0 {
		if h.lastUDPDrops == 0 {
			// First check after startup: kernel counters are cumulative, so
			// initialize baseline without reporting degraded
			h.lastUDPDrops = drops
		} else if drops > h.lastUDPDrops {
			// New drops occurred since last check
			h.lastUDPDrops = drops
			return "degraded"
		} else {
			h.lastUDPDrops = drops
		}
	}

	return "healthy"
}

// isServiceHealthy performs basic health checks
func (h *HealthReportingService) isServiceHealthy() bool {
	// Check if plugins are running
	if h.pluginHealthProvider != nil {
		running := h.pluginHealthProvider.GetPluginCount()
		expected := h.pluginHealthProvider.GetExpectedPluginCount()
		if expected > 0 && running == 0 {
			// No plugins running when we expect some - unhealthy
			return false
		}
	}
	return true
}

// generateMetrics generates service runtime metrics (not configuration)
func (h *HealthReportingService) generateMetrics() map[string]interface{} {
	metrics := map[string]interface{}{
		"timestamp":         time.Now().Unix(),
		"last_health_check": time.Now().UTC().Format(time.RFC3339),
	}

	// Collect system metrics (disk, CPU, memory)
	sysMetrics := CollectSystemMetrics(h.diskPath)
	if sysMetrics != nil {
		for k, v := range sysMetrics.ToMap() {
			metrics[k] = v
		}
	}

	// Add proxy throughput stats if available
	if h.proxyStats != nil {
		uptimeSeconds := int64(time.Since(h.startTime).Seconds())
		if uptimeSeconds < 1 {
			uptimeSeconds = 1 // Prevent division by zero
		}

		bytesReceived := atomic.LoadInt64(&h.proxyStats.BytesReceived)
		bytesForwarded := atomic.LoadInt64(&h.proxyStats.BytesForwarded)
		messagesReceived := atomic.LoadInt64(&h.proxyStats.UDPMessagesReceived)
		batchesForwarded := atomic.LoadInt64(&h.proxyStats.BatchesForwarded)
		forwardingErrors24h := h.proxyStats.ForwardingErrors24h()

		metrics["uptime_seconds"] = uptimeSeconds
		metrics["bytes_received"] = bytesReceived
		metrics["bytes_forwarded"] = bytesForwarded
		metrics["messages_received"] = messagesReceived
		metrics["batches_forwarded"] = batchesForwarded
		metrics["forwarding_errors"] = forwardingErrors24h

		// Calculate throughput (bytes per second)
		metrics["bytes_received_per_sec"] = float64(bytesReceived) / float64(uptimeSeconds)
		metrics["bytes_forwarded_per_sec"] = float64(bytesForwarded) / float64(uptimeSeconds)
		metrics["messages_received_per_sec"] = float64(messagesReceived) / float64(uptimeSeconds)
	}

	// Collect UDP socket drop statistics
	udpPorts := h.getUDPPortsFromConfig()
	if len(udpPorts) > 0 {
		socketStats := CollectUDPSocketDrops(udpPorts)
		var totalDrops int64
		portDrops := make(map[string]int64)

		// Get port-to-dataset mapping for error reporting
		var portDatasetMap []PortDatasetInfo
		if h.udpPortsProvider != nil {
			portDatasetMap = h.udpPortsProvider.GetPortDatasetMap()
		}
		portToDataset := make(map[int]PortDatasetInfo)
		for _, info := range portDatasetMap {
			portToDataset[info.Port] = info
		}

		for port, stats := range socketStats {
			totalDrops += stats.Drops
			portDrops[fmt.Sprintf("port_%d", port)] = stats.Drops

			// Check if drops increased for this port and report per-dataset error
			lastDrops := h.lastPortDrops[port]
			if stats.Drops > lastDrops && h.errorReporter != nil {
				newDrops := stats.Drops - lastDrops
				if info, ok := portToDataset[port]; ok && info.DatasetID != "" {
					// Report warning for this specific dataset
					errorMsg := fmt.Sprintf("UDP socket drops detected on port %d: %d new drops (%d total). Data loss is occurring.",
						port, newDrops, stats.Drops)
					if err := h.errorReporter.ReportWarning(
						context.Background(),
						"udp_buffer_limited",
						errorMsg,
						info.TenantID,
						info.DatasetID,
					); err != nil {
						log.Warnf("Failed to report UDP drop error for dataset %s: %v", info.DatasetID, err)
					} else {
						log.Debugf("Reported UDP drops for dataset %s (port %d): %d new drops", info.DatasetID, port, newDrops)
					}
				}
			} else if stats.Drops == 0 && lastDrops > 0 && h.errorReporter != nil {
				// Drops went to zero (proxy restarted) - resolve the error
				if info, ok := portToDataset[port]; ok && info.DatasetID != "" {
					if err := h.errorReporter.ResolveErrorsByType(
						context.Background(),
						"udp_buffer_limited",
						info.DatasetID,
					); err != nil {
						log.Debugf("Failed to resolve UDP drop error for dataset %s: %v", info.DatasetID, err)
					}
				}
			}
			h.lastPortDrops[port] = stats.Drops
		}
		metrics["udp_socket_drops_total"] = totalDrops
		metrics["udp_socket_drops_by_port"] = portDrops
	}

	return metrics
}

// SetUDPPortsProvider sets the provider for getting active UDP ports
func (h *HealthReportingService) SetUDPPortsProvider(provider UDPPortsProvider) {
	h.udpPortsProvider = provider
}

// SetPluginHealthProvider sets the provider for checking plugin health
func (h *HealthReportingService) SetPluginHealthProvider(provider PluginHealthProvider) {
	h.pluginHealthProvider = provider
}

// SetErrorReporter sets the error reporter for reporting per-dataset errors
func (h *HealthReportingService) SetErrorReporter(reporter ErrorReporter) {
	h.errorReporter = reporter
}

// getUDPPortsFromConfig extracts UDP listening ports from the configuration or provider
func (h *HealthReportingService) getUDPPortsFromConfig() []int {
	// First try to get ports from the provider (plugin manager)
	if h.udpPortsProvider != nil {
		ports := h.udpPortsProvider.GetUDPPorts()
		if len(ports) > 0 {
			return ports
		}
	}

	// Fallback to static config
	var ports []int

	if h.config == nil {
		return ports
	}

	// Look for inputs in the configuration
	inputs, ok := h.config["inputs"]
	if !ok {
		return ports
	}

	inputsList, ok := inputs.([]interface{})
	if !ok {
		return ports
	}

	for _, input := range inputsList {
		inputMap, ok := input.(map[string]interface{})
		if !ok {
			continue
		}

		pluginType, _ := inputMap["type"].(string)
		if !plugins.UDPPluginTypes[pluginType] {
			continue
		}

		// Get port from config section
		configSection, ok := inputMap["config"].(map[string]interface{})
		if !ok {
			continue
		}

		// Port can be int, float64 (from JSON), or int64
		if port, ok := utils.ToInt(configSection["port"]); ok {
			ports = append(ports, port)
		}
	}

	return ports
}

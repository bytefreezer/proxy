// Licensed under Elastic License 2.0
// See LICENSE.txt for details

package services

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/bytefreezer/goodies/log"
	"github.com/bytefreezer/proxy/config"
	"github.com/bytefreezer/proxy/domain"
	"github.com/bytefreezer/proxy/plugins"
	"github.com/bytefreezer/proxy/utils"
)

// UploadWorker handles upload processing (aligned with receiver pattern)
type UploadWorker struct {
	id            int
	pluginService *PluginService
	uploadChannel <-chan *domain.DataBatch
}

// PluginService manages input plugins and data batching
type PluginService struct {
	config              *config.Config
	pluginManager       *plugins.Manager
	batchProcessor      *BatchProcessor
	forwarder           *HTTPForwarder
	spoolingService     *SpoolingService
	ctx                 context.Context
	cancel              context.CancelFunc
	wg                  sync.WaitGroup
	uploadChannel       chan *domain.DataBatch // Channel for immediate upload notifications
	uploadWorkers       []*UploadWorker        // Upload worker instances (aligned with receiver)
	workerCount         int                    // Number of upload workers
	expectedPluginCount int                    // Expected number of plugins from config
	mu                  sync.RWMutex           // Protects expectedPluginCount
}

// NewPluginService creates a new plugin service
func NewPluginService(cfg *config.Config, forwarder *HTTPForwarder, spoolingService *SpoolingService) (*PluginService, error) {

	// Allow starting with zero plugins - they may be loaded dynamically from Control
	// if len(cfg.Inputs) == 0 {
	// 	return nil, fmt.Errorf("no input plugins configured")
	// }

	ctx, cancel := context.WithCancel(context.Background())

	// Create upload channel for immediate upload notifications
	uploadChannel := make(chan *domain.DataBatch, 1000)

	// Create plugin manager with global configuration support and direct filesystem writes
	pluginManager := plugins.NewManagerWithGlobals(cfg.Inputs, spoolingService, plugins.GlobalRegistry, cfg.TenantID, cfg.BearerToken)

	// Create batch processor (reuse existing batching logic)
	batchProcessor := NewBatchProcessor(cfg)

	workerCount := cfg.GetUploadWorkerCount()
	uploadWorkers := make([]*UploadWorker, workerCount)

	// Initialize upload workers (aligned with receiver pattern)
	for i := 0; i < workerCount; i++ {
		uploadWorkers[i] = &UploadWorker{
			id:            i,
			pluginService: nil, // Will be set after PluginService creation
			uploadChannel: uploadChannel,
		}
	}

	ps := &PluginService{
		config:          cfg,
		pluginManager:   pluginManager,
		batchProcessor:  batchProcessor,
		forwarder:       forwarder,
		spoolingService: spoolingService,
		ctx:             ctx,
		cancel:          cancel,
		uploadChannel:   uploadChannel,
		uploadWorkers:   uploadWorkers,
		workerCount:     workerCount,
	}

	// Set back-reference to plugin service
	for _, worker := range uploadWorkers {
		worker.pluginService = ps
	}

	return ps, nil
}

// Start begins the plugin service
func (ps *PluginService) Start() error {
	log.Info("Starting plugin service")

	// Start plugin manager
	if err := ps.pluginManager.Start(); err != nil {
		return fmt.Errorf("failed to start plugin manager: %w", err)
	}

	// Start batch processor
	if err := ps.batchProcessor.Start(); err != nil {
		return fmt.Errorf("failed to start batch processor: %w", err)
	}

	// Start batch forwarding
	ps.wg.Add(1)
	go ps.processBatches()

	// Start upload workers (aligned with receiver pattern)
	for i, worker := range ps.uploadWorkers {
		ps.wg.Add(1)
		go worker.run(ps.ctx, &ps.wg)
		log.Debugf("Started upload worker %d", i)
	}

	log.Infof("Plugin service started with %d plugins and %d upload workers", ps.pluginManager.GetPluginCount(), ps.workerCount)
	return nil
}

// Stop gracefully shuts down the plugin service
func (ps *PluginService) Stop() error {
	log.Info("Stopping plugin service")

	// Cancel context
	ps.cancel()

	// Stop plugin manager
	if err := ps.pluginManager.Stop(); err != nil {
		log.Errorf("Error stopping plugin manager: %v", err)
	}

	// Stop batch processor
	if err := ps.batchProcessor.Stop(); err != nil {
		log.Errorf("Error stopping batch processor: %v", err)
	}

	// Close upload channel
	close(ps.uploadChannel)

	// Wait for all goroutines to finish
	ps.wg.Wait()

	log.Info("Plugin service stopped")
	return nil
}

// Reload reloads the plugin service with new configuration
// Includes port change detection, verification, and error handling
func (ps *PluginService) Reload(newInputConfigs []plugins.PluginConfig) error {
	log.Info("Reloading plugin service with new configuration")

	// CRITICAL: Validate config BEFORE stopping any plugins
	// This prevents breaking working plugins due to bad config
	if err := validatePluginConfigs(newInputConfigs); err != nil {
		log.Errorf("Config validation failed - keeping existing plugins running: %v", err)
		return fmt.Errorf("invalid config (existing plugins preserved): %w", err)
	}

	// Update expected plugin count after validation passes
	ps.SetExpectedPluginCount(len(newInputConfigs))

	// Extract ports from old and new configs for comparison
	oldPorts := extractPortsFromConfigs(ps.pluginManager.GetConfigs())
	newPorts := extractPortsFromConfigs(newInputConfigs)

	// Detect port changes
	portChanges := detectPortChanges(oldPorts, newPorts)
	if len(portChanges) > 0 {
		log.Infof("Port changes detected during reload:")
		for _, change := range portChanges {
			log.Infof("  - %s", change)
		}
	}

	// Stop plugin manager to stop all running plugins
	log.Info("Stopping all running plugins for reload...")
	if err := ps.pluginManager.Stop(); err != nil {
		log.Errorf("Error stopping plugin manager during reload: %v", err)
		// Send SOC alert for stop failure
		if ps.config.SOCAlertClient != nil {
			ps.config.SOCAlertClient.SendAlert("high", "Plugin Reload Failed - Stop Phase",
				"Failed to stop plugins during reload", err.Error())
		}
		return fmt.Errorf("failed to stop plugins: %w", err)
	}

	// CRITICAL: Wait for OS to release ports (especially UDP)
	// This prevents "address already in use" errors when rebinding
	waitTime := 2 * time.Second
	log.Infof("Waiting %v for OS to release ports...", waitTime)
	time.Sleep(waitTime)

	// Verify new ports are available before attempting to start
	if err := verifyPortsAvailable(newPorts); err != nil {
		log.Errorf("Port availability check failed: %v", err)
		// Send SOC alert for port conflict
		if ps.config.SOCAlertClient != nil {
			ps.config.SOCAlertClient.SendAlert("high", "Plugin Reload Failed - Port Conflict",
				"Ports not available after plugin stop", err.Error())
		}
		return fmt.Errorf("ports not available: %w", err)
	}

	log.Infof("All %d ports verified as available", len(newPorts))

	// Create new plugin manager with updated configs
	ps.pluginManager = plugins.NewManagerWithGlobals(
		newInputConfigs,
		ps.spoolingService,
		plugins.GlobalRegistry,
		ps.config.TenantID,
		ps.config.BearerToken,
	)

	// Start new plugins
	log.Info("Starting plugins with new configuration...")
	if err := ps.pluginManager.Start(); err != nil {
		log.Errorf("CRITICAL: Error starting new plugins during reload: %v", err)
		// Send SOC alert for start failure
		if ps.config.SOCAlertClient != nil {
			ps.config.SOCAlertClient.SendAlert("critical", "Plugin Reload Failed - Start Phase",
				fmt.Sprintf("Failed to start %d new plugins after reload", len(newInputConfigs)), err.Error())
		}
		return fmt.Errorf("failed to start new plugins: %w", err)
	}

	log.Infof("✅ Plugin service reloaded successfully with %d plugins", len(newInputConfigs))
	if len(portChanges) > 0 {
		log.Info("⚠️  Port changes applied - data collection restarted on new ports")
	}
	return nil
}

// processBatches processes completed batches from the batch processor
func (ps *PluginService) processBatches() {
	defer ps.wg.Done()

	batchChannel := ps.batchProcessor.GetBatchChannel()

	for {
		select {
		case <-ps.ctx.Done():
			return
		case batch, ok := <-batchChannel:
			if !ok {
				return // Channel closed
			}
			ps.forwardBatch(batch)
		}
	}
}

// run handles upload processing for a worker (aligned with receiver pattern)
func (w *UploadWorker) run(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	log.Debugf("Upload worker %d started", w.id)

	for {
		select {
		case batch := <-w.uploadChannel:
			if batch == nil {
				continue
			}
			log.Debugf("Worker %d processing batch %s", w.id, batch.ID)
			w.pluginService.attemptImmediateUpload(batch)

		case <-ctx.Done():
			log.Debugf("Upload worker %d stopping", w.id)
			return
		}
	}
}

// forwardBatch spools data first for safety, then notifies uploader for immediate attempt
func (ps *PluginService) forwardBatch(batch *domain.DataBatch) {
	// SPOOL FIRST - preserve data safety
	if err := ps.spoolBatch(batch); err != nil {
		log.Errorf("CRITICAL: Failed to spool batch %s - DATA LOSS: %v", batch.ID, err)
		// Send SOC alert for spooling failure
		if ps.config.SOCAlertClient != nil {
			ps.config.SOCAlertClient.SendAlert("critical", "Data Spooling Failed",
				fmt.Sprintf("Failed to spool batch %s", batch.ID), err.Error())
		}
		return
	}

	log.Debugf("Spooled batch %s (%d bytes, %d lines), notifying uploader for immediate attempt",
		batch.ID, batch.TotalBytes, batch.LineCount)

	// NOTIFY UPLOADER - trigger immediate upload attempt via buffered channel
	select {
	case ps.uploadChannel <- batch:
		log.Debugf("Batch %s queued for immediate upload", batch.ID)
	default:
		log.Warnf("Upload channel at capacity, moving batch %s to retry queue", batch.ID)
		// If channel is at capacity, move file to retry directory
		if err := ps.spoolingService.MoveQueueToRetry(batch.TenantID, batch.DatasetID, batch.ID, "channel_capacity_exceeded", batch.TriggerReason); err != nil {
			log.Errorf("Failed to move batch %s to retry: %v", batch.ID, err)
		}
	}
}

// attemptImmediateUpload attempts to upload a batch immediately from the spooled file
func (ps *PluginService) attemptImmediateUpload(batch *domain.DataBatch) {
	log.Debugf("Attempting immediate upload for batch %s", batch.ID)

	// Try to upload the spooled file
	err := ps.forwarder.ForwardBatch(batch)
	if err != nil {
		log.Warnf("Immediate upload failed for batch %s: %v - moving to retry", batch.ID, err)
		// Move file from queue to retry directory for background processing
		if retryErr := ps.spoolingService.MoveQueueToRetry(batch.TenantID, batch.DatasetID, batch.ID, err.Error(), batch.TriggerReason); retryErr != nil {
			log.Errorf("Failed to move batch %s to retry after upload failure: %v", batch.ID, retryErr)
		}
		return
	}

	log.Infof("✅ Immediate upload successful for batch %s (%d bytes, %d lines)",
		batch.ID, batch.TotalBytes, batch.LineCount)

	// Remove file from queue directory on successful upload
	if cleanupErr := ps.spoolingService.RemoveFromQueue(batch.TenantID, batch.DatasetID, batch.ID); cleanupErr != nil {
		log.Errorf("Failed to cleanup successful batch %s from queue: %v", batch.ID, cleanupErr)
	}
}

// spoolBatch saves batch data immediately to spool (spool-first architecture)
func (ps *PluginService) spoolBatch(batch *domain.DataBatch) error {
	if ps.spoolingService == nil {
		return fmt.Errorf("spooling service not available")
	}

	// Use the already compressed data from the batch
	return ps.spoolingService.StoreBatchToQueue(
		batch.TenantID,
		batch.DatasetID,
		batch.BearerToken,
		batch.Data,          // Already compressed data
		"",                  // No failure reason - this is initial spooling
		batch.ID,            // Use the existing batch ID
		batch.TriggerReason, // Pass the trigger reason from the batch
		batch.DataHint,      // Data format hint for downstream processing
	)
}

// GetPluginHealth returns health status of all plugins
func (ps *PluginService) GetPluginHealth() map[string]plugins.PluginHealth {
	return ps.pluginManager.GetPluginHealth()
}

// GetPluginMetrics returns metrics for all plugins (if implemented)
func (ps *PluginService) GetPluginMetrics() map[string]interface{} {
	health := ps.pluginManager.GetPluginHealth()
	metrics := make(map[string]interface{})

	for name, h := range health {
		metrics[name] = map[string]interface{}{
			"status":       h.Status,
			"message":      h.Message,
			"last_updated": h.LastUpdated,
		}
	}

	return metrics
}

// GetActivePlugins returns list of active plugin names
func (ps *PluginService) GetActivePlugins() []string {
	return ps.pluginManager.ListPlugins()
}

// GetPluginConfigs returns the current plugin configurations
func (ps *PluginService) GetPluginConfigs() []plugins.PluginConfig {
	if ps.pluginManager == nil {
		return []plugins.PluginConfig{}
	}
	return ps.pluginManager.GetConfigs()
}

// GetUDPPorts returns the UDP listening ports for all active UDP-based plugins
// This implements the UDPPortsProvider interface for health reporting
func (ps *PluginService) GetUDPPorts() []int {
	if ps.pluginManager == nil {
		return []int{}
	}
	return ps.pluginManager.GetUDPPorts()
}

// GetPortDatasetMap returns port-to-dataset mapping for all active UDP plugins
// This implements the UDPPortsProvider interface for health reporting
func (ps *PluginService) GetPortDatasetMap() []PortDatasetInfo {
	if ps.pluginManager == nil {
		return []PortDatasetInfo{}
	}

	configs := ps.pluginManager.GetConfigs()
	var result []PortDatasetInfo

	for _, cfg := range configs {
		if !plugins.UDPPluginTypes[cfg.Type] {
			continue
		}

		// Get port from config
		port, _ := utils.ToInt(cfg.Config["port"])

		if port == 0 {
			continue
		}

		// Get tenant_id and dataset_id from config
		tenantID, _ := cfg.Config["tenant_id"].(string)
		datasetID, _ := cfg.Config["dataset_id"].(string)

		result = append(result, PortDatasetInfo{
			Port:      port,
			TenantID:  tenantID,
			DatasetID: datasetID,
		})
	}

	return result
}

// GetPluginCount returns the number of currently running plugins
// This implements part of the PluginHealthProvider interface
func (ps *PluginService) GetPluginCount() int {
	if ps.pluginManager == nil {
		return 0
	}
	return ps.pluginManager.GetPluginCount()
}

// GetExpectedPluginCount returns the number of plugins we expect to be running
// This implements part of the PluginHealthProvider interface
func (ps *PluginService) GetExpectedPluginCount() int {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	return ps.expectedPluginCount
}

// SetExpectedPluginCount sets the expected number of plugins
func (ps *PluginService) SetExpectedPluginCount(count int) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.expectedPluginCount = count
}

// generateBatchIDWithDataHint generates a unique batch ID with data hint for new format
func generateBatchIDWithDataHint(tenantID, datasetID, dataHint string) string {
	return fmt.Sprintf("%s--%s--%d--%s", tenantID, datasetID, time.Now().UnixNano(), dataHint)
}

// validatePluginConfigs validates all plugin configurations before applying
// This MUST be called before stopping any existing plugins
func validatePluginConfigs(configs []plugins.PluginConfig) error {
	var errors []string

	for _, cfg := range configs {
		pluginName := fmt.Sprintf("%s[%s]", cfg.Type, cfg.Name)

		// Validate port if present
		if portValue, exists := cfg.Config["port"]; exists {
			port, _ := utils.ToInt(portValue)

			// Port must be in valid range (1-65535)
			if port < 1 || port > 65535 {
				errors = append(errors, fmt.Sprintf("%s: invalid port %d (must be 1-65535)", pluginName, port))
			}
		}

		// Validate buffer_size and read_buffer_size if present (both field names are used)
		for _, fieldName := range []string{"buffer_size", "read_buffer_size"} {
			if bufferValue, exists := cfg.Config[fieldName]; exists {
				bufferSize, _ := utils.ToInt(bufferValue)

				// Buffer size sanity check (1KB to 1GB)
				if bufferSize < 1024 || bufferSize > 1073741824 {
					errors = append(errors, fmt.Sprintf("%s: invalid %s %d (must be 1KB-1GB)", pluginName, fieldName, bufferSize))
				}
			}
		}

		// Validate required fields based on plugin type
		switch cfg.Type {
		case "udp", "syslog", "netflow", "sflow", "ipfix", "ebpf":
			if _, exists := cfg.Config["port"]; !exists {
				errors = append(errors, fmt.Sprintf("%s: missing required port", pluginName))
			}
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("validation errors: %v", errors)
	}

	return nil
}

// extractPortsFromConfigs extracts all port numbers from plugin configurations
func extractPortsFromConfigs(configs []plugins.PluginConfig) map[int]string {
	ports := make(map[int]string)

	for _, cfg := range configs {
		if portValue, exists := cfg.Config["port"]; exists {
			port, _ := utils.ToInt(portValue)

			if port > 0 {
				pluginName := fmt.Sprintf("%s[%s]", cfg.Type, cfg.Name)
				ports[port] = pluginName
			}
		}
	}

	return ports
}

// detectPortChanges compares old and new port configurations and returns change descriptions
func detectPortChanges(oldPorts, newPorts map[int]string) []string {
	changes := []string{}

	// Check for removed ports
	for port, oldPlugin := range oldPorts {
		if _, exists := newPorts[port]; !exists {
			changes = append(changes, fmt.Sprintf("Port %d removed (was: %s)", port, oldPlugin))
		}
	}

	// Check for added or changed ports
	for port, newPlugin := range newPorts {
		if oldPlugin, exists := oldPorts[port]; exists {
			if oldPlugin != newPlugin {
				changes = append(changes, fmt.Sprintf("Port %d reassigned: %s → %s", port, oldPlugin, newPlugin))
			}
		} else {
			changes = append(changes, fmt.Sprintf("Port %d added (new: %s)", port, newPlugin))
		}
	}

	return changes
}

// verifyPortsAvailable checks if all ports are available for binding
// This helps detect port conflicts before attempting to start plugins
func verifyPortsAvailable(ports map[int]string) error {
	unavailablePorts := []string{}

	for port := range ports {
		// Try both UDP and TCP (plugins may use either)
		if err := checkPortAvailableUDP(port); err != nil {
			unavailablePorts = append(unavailablePorts, fmt.Sprintf("UDP:%d (%v)", port, err))
		}
		if err := checkPortAvailableTCP(port); err != nil {
			unavailablePorts = append(unavailablePorts, fmt.Sprintf("TCP:%d (%v)", port, err))
		}
	}

	if len(unavailablePorts) > 0 {
		return fmt.Errorf("ports not available: %v", unavailablePorts)
	}

	return nil
}

// checkPortAvailableUDP checks if a UDP port is available
func checkPortAvailableUDP(port int) error {
	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		return err
	}

	listener, err := net.ListenUDP("udp", addr)
	if err != nil {
		return err
	}
	listener.Close()

	return nil
}

// checkPortAvailableTCP checks if a TCP port is available
func checkPortAvailableTCP(port int) error {
	addr, err := net.ResolveTCPAddr("tcp", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		return err
	}

	listener, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return err
	}
	listener.Close()

	return nil
}

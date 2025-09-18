package services

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/n0needt0/bytefreezer-proxy/config"
	"github.com/n0needt0/bytefreezer-proxy/domain"
	"github.com/n0needt0/bytefreezer-proxy/plugins"
	"github.com/n0needt0/go-goodies/log"
)

// PluginService manages input plugins and data batching
type PluginService struct {
	config          *config.Config
	pluginManager   *plugins.Manager
	batchProcessor  *BatchProcessor
	forwarder       *HTTPForwarder
	spoolingService *SpoolingService
	ctx             context.Context
	cancel          context.CancelFunc
	wg              sync.WaitGroup
	inputChannel    chan *plugins.DataMessage
	batchingEnabled bool
}

// NewPluginService creates a new plugin service
func NewPluginService(cfg *config.Config, forwarder *HTTPForwarder, spoolingService *SpoolingService) (*PluginService, error) {

	if len(cfg.Inputs) == 0 {
		return nil, fmt.Errorf("no input plugins configured")
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Create input channel for plugin messages
	inputChannel := make(chan *plugins.DataMessage, 10000)

	// Create plugin manager with global configuration support
	pluginManager := plugins.NewManagerWithGlobals(cfg.Inputs, inputChannel, plugins.GlobalRegistry, cfg.TenantID, cfg.BearerToken)

	// Create batch processor (reuse existing batching logic)
	batchProcessor := NewBatchProcessor(cfg)

	return &PluginService{
		config:          cfg,
		pluginManager:   pluginManager,
		batchProcessor:  batchProcessor,
		forwarder:       forwarder,
		spoolingService: spoolingService,
		ctx:             ctx,
		cancel:          cancel,
		inputChannel:    inputChannel,
		batchingEnabled: true,
	}, nil
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

	// Start input message processor
	ps.wg.Add(1)
	go ps.processInputMessages()

	// Start batch forwarding
	ps.wg.Add(1)
	go ps.processBatches()

	log.Infof("Plugin service started with %d plugins", ps.pluginManager.GetPluginCount())
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

	// Close input channel
	close(ps.inputChannel)

	// Wait for all goroutines to finish
	ps.wg.Wait()

	log.Info("Plugin service stopped")
	return nil
}

// processInputMessages processes messages from input plugins
func (ps *PluginService) processInputMessages() {
	defer ps.wg.Done()

	for {
		select {
		case <-ps.ctx.Done():
			return
		case msg, ok := <-ps.inputChannel:
			if !ok {
				return // Channel closed
			}
			ps.processPluginMessage(msg)
		}
	}
}

// processPluginMessage processes a single message from a plugin
func (ps *PluginService) processPluginMessage(msg *plugins.DataMessage) {
	if ps.batchingEnabled {
		// Add to batch processor
		ps.batchProcessor.AddMessage(msg)
	} else {
		// Forward immediately
		batch := ps.createBatchFromMessage(msg)
		ps.forwardBatch(batch)
	}
}

// createBatchFromMessage creates a batch from a single message
func (ps *PluginService) createBatchFromMessage(msg *plugins.DataMessage) *domain.DataBatch {
	// Get bearer token from message metadata or config
	bearerToken := msg.Metadata["bearer_token"]
	if bearerToken == "" {
		bearerToken = ps.config.BearerToken
	}

	// Compress data
	compressedData, err := ps.compressData(msg.Data)
	if err != nil {
		log.Errorf("Failed to compress data: %v", err)
		compressedData = msg.Data // Use uncompressed as fallback
	}

	return &domain.DataBatch{
		ID:          generateBatchID(),
		TenantID:    msg.TenantID,
		DatasetID:   msg.DatasetID,
		Data:        compressedData,
		LineCount:   1, // Single message
		TotalBytes:  int64(len(msg.Data)),
		CreatedAt:   msg.Timestamp,
		BearerToken: bearerToken,
	}
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

// forwardBatch spools batch data immediately (spool-first architecture)
func (ps *PluginService) forwardBatch(batch *domain.DataBatch) {
	// SPOOL FIRST - all data goes to spool before any transmission attempts
	if err := ps.spoolBatch(batch); err != nil {
		log.Errorf("CRITICAL: Failed to spool batch %s - DATA LOSS: %v", batch.ID, err)
		// Send SOC alert for spooling failure
		if ps.config.SOCAlertClient != nil {
			ps.config.SOCAlertClient.SendAlert("critical", "Data Spooling Failed",
				fmt.Sprintf("Failed to spool batch %s", batch.ID), err.Error())
		}
	} else {
		log.Debugf("Spooled batch %s (%d bytes, %d lines) for async transmission",
			batch.ID, batch.TotalBytes, batch.LineCount)
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
		batch.Data, // Already compressed data
		"",         // No failure reason - this is initial spooling
	)
}

// compressData compresses raw data using gzip
func (ps *PluginService) compressData(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	writer, _ := gzip.NewWriterLevel(&buf, 6) // Default compression level

	if _, err := writer.Write(data); err != nil {
		writer.Close()
		return nil, err
	}

	if err := writer.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
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

// generateBatchID generates a unique batch ID
func generateBatchID() string {
	return fmt.Sprintf("batch_%d", time.Now().UnixNano())
}

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

// BatchProcessor handles batching of plugin messages
type BatchProcessor struct {
	config       *config.Config
	batches      map[string]*activeBatch // key: tenant_id/dataset_id
	batchChannel chan *domain.DataBatch
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	mu           sync.RWMutex
	ticker       *time.Ticker
}

// activeBatch represents a batch being built
type activeBatch struct {
	TenantID      string
	DatasetID     string
	BearerToken   string
	FileExtension string
	Messages      [][]byte
	LineCount     int64
	TotalBytes    int64
	CreatedAt     time.Time
	LastUpdated   time.Time
}

// NewBatchProcessor creates a new batch processor
func NewBatchProcessor(cfg *config.Config) *BatchProcessor {
	ctx, cancel := context.WithCancel(context.Background())

	return &BatchProcessor{
		config:       cfg,
		batches:      make(map[string]*activeBatch),
		batchChannel: make(chan *domain.DataBatch, 10000), // Increased from 100 to 10000
		ctx:          ctx,
		cancel:       cancel,
		ticker:       time.NewTicker(time.Duration(cfg.Batching.TimeoutSeconds) * time.Second),
	}
}

// Start begins the batch processor
func (bp *BatchProcessor) Start() error {
	log.Info("Starting batch processor")

	// Start timeout checker
	bp.wg.Add(1)
	go bp.timeoutChecker()

	log.Infof("Batch processor started with %d second timeout", bp.config.Batching.TimeoutSeconds)
	return nil
}

// Stop gracefully shuts down the batch processor
func (bp *BatchProcessor) Stop() error {
	log.Info("Stopping batch processor")

	bp.cancel()
	bp.ticker.Stop()

	// Flush remaining batches
	bp.mu.Lock()
	for key, batch := range bp.batches {
		bp.finalizeBatch(key, batch, "service_shutdown")
	}
	bp.mu.Unlock()

	close(bp.batchChannel)
	bp.wg.Wait()

	log.Info("Batch processor stopped")
	return nil
}

// AddMessage adds a message to the appropriate batch
func (bp *BatchProcessor) AddMessage(msg *plugins.DataMessage) {
	bp.mu.Lock()
	defer bp.mu.Unlock()

	key := fmt.Sprintf("%s/%s", msg.TenantID, msg.DatasetID)

	// Get or create batch
	batch, exists := bp.batches[key]
	if !exists {
		bearerToken := msg.Metadata["bearer_token"]
		if bearerToken == "" {
			bearerToken = bp.config.BearerToken
		}

		batch = &activeBatch{
			TenantID:      msg.TenantID,
			DatasetID:     msg.DatasetID,
			BearerToken:   bearerToken,
			FileExtension: msg.FileExtension,
			Messages:      make([][]byte, 0),
			CreatedAt:     msg.Timestamp,
			LastUpdated:   msg.Timestamp,
		}
		bp.batches[key] = batch
	}

	// Add message to batch
	batch.Messages = append(batch.Messages, msg.Data)
	batch.LineCount++
	batch.TotalBytes += int64(len(msg.Data))
	batch.LastUpdated = time.Now()

	// Check if batch is ready to be sent
	if reason := bp.shouldFinalizeBatch(batch); reason != "" {
		bp.finalizeBatch(key, batch, reason)
		delete(bp.batches, key)
	}
}

// shouldFinalizeBatch determines if a batch should be finalized and returns the reason
func (bp *BatchProcessor) shouldFinalizeBatch(batch *activeBatch) string {
	// Check line count limit (only if > 0, 0 = disabled)
	if bp.config.Batching.MaxLines > 0 && batch.LineCount >= int64(bp.config.Batching.MaxLines) {
		return "line_limit_reached"
	}

	// Check byte size limit (only if > 0, 0 = disabled)
	if bp.config.Batching.MaxBytes > 0 && batch.TotalBytes >= bp.config.Batching.MaxBytes {
		return "size_limit_reached"
	}

	return ""
}

// finalizeBatch converts an active batch to a domain batch and sends it
func (bp *BatchProcessor) finalizeBatch(key string, batch *activeBatch, reason string) {
	log.Debugf("Finalizing batch %s (%s): %d messages, %d bytes",
		key, reason, batch.LineCount, batch.TotalBytes)

	// Concatenate all messages with newlines
	var buffer bytes.Buffer
	for i, msg := range batch.Messages {
		if i > 0 {
			buffer.WriteByte('\n')
		}
		buffer.Write(msg)
	}

	// Compress the combined data
	compressedData, err := bp.compressData(buffer.Bytes())
	if err != nil {
		log.Errorf("Failed to compress batch data: %v", err)
		compressedData = buffer.Bytes() // Use uncompressed as fallback
	}

	// Create domain batch
	domainBatch := &domain.DataBatch{
		ID:            generateBatchID(batch.TenantID, batch.DatasetID),
		TenantID:      batch.TenantID,
		DatasetID:     batch.DatasetID,
		FileExtension: batch.FileExtension,
		Data:          compressedData,
		LineCount:     int(batch.LineCount),
		TotalBytes:    batch.TotalBytes, // Original uncompressed size
		CreatedAt:     batch.CreatedAt,
		BearerToken:   batch.BearerToken,
		TriggerReason: reason,
	}

	// Send to batch channel - NEVER drop batches, always block if needed
	select {
	case bp.batchChannel <- domainBatch:
	case <-bp.ctx.Done():
		return
	}
}

// timeoutChecker periodically checks for batches that have timed out
func (bp *BatchProcessor) timeoutChecker() {
	defer bp.wg.Done()

	for {
		select {
		case <-bp.ctx.Done():
			return
		case <-bp.ticker.C:
			bp.checkTimeouts()
		}
	}
}

// checkTimeouts checks for and finalizes timed-out batches
func (bp *BatchProcessor) checkTimeouts() {
	bp.mu.Lock()
	defer bp.mu.Unlock()

	now := time.Now()
	timeout := time.Duration(bp.config.Batching.TimeoutSeconds) * time.Second

	var keysToDelete []string

	for key, batch := range bp.batches {
		if now.Sub(batch.LastUpdated) > timeout {
			bp.finalizeBatch(key, batch, "timeout")
			keysToDelete = append(keysToDelete, key)
		}
	}

	// Delete finalized batches
	for _, key := range keysToDelete {
		delete(bp.batches, key)
	}

	if len(keysToDelete) > 0 {
		log.Debugf("Finalized %d batches due to timeout", len(keysToDelete))
	}
}

// compressData compresses data using gzip
func (bp *BatchProcessor) compressData(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)

	if _, err := gz.Write(data); err != nil {
		gz.Close()
		return nil, err
	}

	if err := gz.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// GetBatchChannel returns the channel for completed batches
func (bp *BatchProcessor) GetBatchChannel() <-chan *domain.DataBatch {
	return bp.batchChannel
}

// GetBatchStats returns current batching statistics
func (bp *BatchProcessor) GetBatchStats() map[string]interface{} {
	bp.mu.RLock()
	defer bp.mu.RUnlock()

	stats := make(map[string]interface{})
	stats["active_batches"] = len(bp.batches)

	totalMessages := int64(0)
	totalBytes := int64(0)

	for key, batch := range bp.batches {
		totalMessages += batch.LineCount
		totalBytes += batch.TotalBytes

		stats[fmt.Sprintf("batch_%s", key)] = map[string]interface{}{
			"messages": batch.LineCount,
			"bytes":    batch.TotalBytes,
			"age":      time.Since(batch.CreatedAt).Seconds(),
		}
	}

	stats["total_messages"] = totalMessages
	stats["total_bytes"] = totalBytes

	return stats
}

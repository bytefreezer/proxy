package services

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/n0needt0/bytefreezer-proxy/config"
	"github.com/n0needt0/bytefreezer-proxy/domain"
	"github.com/n0needt0/go-goodies/log"
)

// SpoolingService handles local file spooling for failed uploads
type SpoolingService struct {
	config          *config.Config
	directory       string
	maxSize         int64
	retryAttempts   int
	retryInterval   time.Duration
	cleanupInterval time.Duration

	// Runtime state
	currentSize int64
	mutex       sync.RWMutex
	shutdown    chan struct{}
	wg          sync.WaitGroup
}

// SpooledFile represents a file in the spooling directory
type SpooledFile struct {
	ID            string    `json:"id"`
	TenantID      string    `json:"tenant_id"`
	DatasetID     string    `json:"dataset_id"`
	BearerToken   string    `json:"bearer_token,omitempty"` // Authentication token for this tenant
	Filename      string    `json:"filename"`
	Size          int64     `json:"size"`
	LineCount     int       `json:"line_count"`
	CreatedAt     time.Time `json:"created_at"`
	LastRetry     time.Time `json:"last_retry"`
	RetryCount    int       `json:"retry_count"`
	Status        string    `json:"status"` // "pending", "retrying", "failed", "success"
	FailureReason string    `json:"failure_reason,omitempty"`
}

// NewSpoolingService creates a new spooling service
func NewSpoolingService(cfg *config.Config) *SpoolingService {
	// Set default organization if not specified
	if cfg.Spooling.Organization == "" {
		cfg.Spooling.Organization = "tenant_dataset" // Default to organized structure
	}

	return &SpoolingService{
		config:          cfg,
		directory:       cfg.Spooling.Directory,
		maxSize:         cfg.Spooling.MaxSizeBytes,
		retryAttempts:   cfg.Spooling.RetryAttempts,
		retryInterval:   time.Duration(cfg.Spooling.RetryIntervalSec) * time.Second,
		cleanupInterval: time.Duration(cfg.Spooling.CleanupIntervalSec) * time.Second,
		shutdown:        make(chan struct{}),
	}
}

// Start begins the spooling service
func (s *SpoolingService) Start() error {
	if !s.config.Spooling.Enabled {
		log.Info("Spooling service is disabled")
		return nil
	}

	// Create spooling directory
	if err := os.MkdirAll(s.directory, 0750); err != nil {
		return fmt.Errorf("failed to create spooling directory %s: %w", s.directory, err)
	}

	// Calculate current size
	if err := s.calculateCurrentSize(); err != nil {
		log.Warnf("Failed to calculate current spooling size: %v", err)
	}

	log.Info("Spooling service started - directory: " + s.directory +
		", max size: " + fmt.Sprintf("%d", s.maxSize) + " bytes")

	// Start background goroutines
	s.wg.Add(3)
	go s.retryWorker()
	go s.cleanupWorker()
	go s.batchProcessor()

	return nil
}

// Stop shuts down the spooling service
func (s *SpoolingService) Stop() error {
	if !s.config.Spooling.Enabled {
		return nil
	}

	log.Info("Stopping spooling service...")
	close(s.shutdown)
	s.wg.Wait()
	log.Info("Spooling service stopped")
	return nil
}

// StoreRawMessage stores individual message in raw directory for later batching
func (s *SpoolingService) StoreRawMessage(tenantID, datasetID, bearerToken string, data []byte) error {
	if !s.config.Spooling.Enabled {
		return nil
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Ensure spool directory exists
	if err := s.ensureSpoolDirectoryExists(); err != nil {
		return fmt.Errorf("failed to ensure spool directory exists: %w", err)
	}

	// Generate directory path based on organization strategy
	baseDir, _, _, err := s.generateSpoolPaths(tenantID, datasetID, data)
	if err != nil {
		return fmt.Errorf("failed to generate spool paths: %w", err)
	}

	// Create raw subdirectory within the organized structure
	rawDir := filepath.Join(baseDir, "raw")
	if err := os.MkdirAll(rawDir, 0750); err != nil {
		return fmt.Errorf("failed to create raw directory %s: %w", rawDir, err)
	}

	// Generate unique filename for this message
	now := time.Now()
	filename := fmt.Sprintf("%d_%d.ndjson", now.UnixNano(), len(data))
	filePath := filepath.Join(rawDir, filename)

	// Write raw message
	if err := os.WriteFile(filePath, data, 0600); err != nil {
		return fmt.Errorf("failed to write raw message file: %w", err)
	}

	log.Debugf("Stored raw message for tenant=%s dataset=%s: %s", tenantID, datasetID, filename)
	return nil
}

// BatchRawFiles combines raw files into compressed batches and creates metadata
func (s *SpoolingService) BatchRawFiles(tenantID, datasetID, bearerToken string) error {
	if !s.config.Spooling.Enabled {
		return nil
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()

	rawDir := filepath.Join(s.directory, tenantID, datasetID, "raw")
	queueDir := filepath.Join(s.directory, tenantID, datasetID, "queue")
	metaDir := filepath.Join(s.directory, tenantID, "meta")

	// Create queue and meta directories
	if err := os.MkdirAll(queueDir, 0750); err != nil {
		return fmt.Errorf("failed to create queue directory: %w", err)
	}
	if err := os.MkdirAll(metaDir, 0750); err != nil {
		return fmt.Errorf("failed to create meta directory: %w", err)
	}

	// Read all raw files
	rawFiles, err := os.ReadDir(rawDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No raw files to process
		}
		return fmt.Errorf("failed to read raw directory: %w", err)
	}

	if len(rawFiles) == 0 {
		return nil // No files to process
	}

	// Combine raw files into NDJSON
	var ndjsonData bytes.Buffer
	var processedFiles []string
	var totalBytes int64

	for _, file := range rawFiles {
		if !strings.HasSuffix(file.Name(), ".ndjson") {
			continue
		}

		filePath := filepath.Join(rawDir, file.Name())
		// #nosec G304 - filePath is constructed from controlled rawDir and validated file.Name()
		data, err := os.ReadFile(filePath)
		if err != nil {
			log.Warnf("Failed to read raw file %s: %v", filePath, err)
			continue
		}

		ndjsonData.Write(data)
		if !bytes.HasSuffix(data, []byte("\n")) {
			ndjsonData.WriteByte('\n')
		}

		processedFiles = append(processedFiles, filePath)
		totalBytes += int64(len(data))
	}

	if len(processedFiles) == 0 {
		return nil // No valid files processed
	}

	// Generate batch ID and paths
	now := time.Now()
	batchID := fmt.Sprintf("%s_%s_%s", now.Format("20060102-150405"), tenantID, datasetID)

	// Compress data
	var compressed bytes.Buffer
	gzipWriter, err := gzip.NewWriterLevel(&compressed, 6)
	if err != nil {
		return fmt.Errorf("failed to create gzip writer: %w", err)
	}

	if _, err := gzipWriter.Write(ndjsonData.Bytes()); err != nil {
		return fmt.Errorf("failed to compress data: %w", err)
	}

	if err := gzipWriter.Close(); err != nil {
		return fmt.Errorf("failed to close gzip writer: %w", err)
	}

	// Write compressed batch to queue
	batchFileName := fmt.Sprintf("%s.ndjson.gz", batchID)
	batchFilePath := filepath.Join(queueDir, batchFileName)

	if err := os.WriteFile(batchFilePath, compressed.Bytes(), 0600); err != nil {
		return fmt.Errorf("failed to write compressed batch: %w", err)
	}

	// Create metadata
	metadata := SpooledFile{
		ID:            batchID,
		TenantID:      tenantID,
		DatasetID:     datasetID,
		BearerToken:   bearerToken,
		Filename:      batchFilePath, // Full path to the .gz file
		Size:          int64(compressed.Len()),
		LineCount:     s.countLines(ndjsonData.Bytes()),
		CreatedAt:     now,
		LastRetry:     time.Time{},
		RetryCount:    0,
		Status:        "pending",
		FailureReason: "",
	}

	// Write metadata file
	metaData, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	metaFilePath := filepath.Join(metaDir, fmt.Sprintf("%s.meta", batchID))
	if err := os.WriteFile(metaFilePath, metaData, 0600); err != nil {
		return fmt.Errorf("failed to write metadata: %w", err)
	}

	// Remove processed raw files
	for _, filePath := range processedFiles {
		if err := os.Remove(filePath); err != nil {
			log.Warnf("Failed to remove processed raw file %s: %v", filePath, err)
		}
	}

	log.Infof("Created batch %s from %d raw files for tenant=%s dataset=%s",
		batchID, len(processedFiles), tenantID, datasetID)

	return nil
}

// StoreBatchToQueue stores already-compressed batch data directly to the queue directory
func (s *SpoolingService) StoreBatchToQueue(tenantID, datasetID, bearerToken string, data []byte, failureReason string) error {
	if !s.config.Spooling.Enabled {
		return nil
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Create directories
	queueDir := filepath.Join(s.directory, tenantID, datasetID, "queue")
	metaDir := filepath.Join(s.directory, tenantID, "meta")

	if err := os.MkdirAll(queueDir, 0750); err != nil {
		return fmt.Errorf("failed to create queue directory: %w", err)
	}
	if err := os.MkdirAll(metaDir, 0750); err != nil {
		return fmt.Errorf("failed to create meta directory: %w", err)
	}

	// Generate batch ID and paths
	now := time.Now()
	batchID := fmt.Sprintf("%s_%s_%s", now.Format("20060102-150405"), tenantID, datasetID)

	// Determine file extension based on compression
	var extension string
	if len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b {
		extension = ".ndjson.gz"
	} else {
		extension = ".ndjson"
	}

	// Write batch file to queue
	batchFileName := fmt.Sprintf("%s%s", batchID, extension)
	batchFilePath := filepath.Join(queueDir, batchFileName)

	if err := os.WriteFile(batchFilePath, data, 0600); err != nil {
		return fmt.Errorf("failed to write batch to queue: %w", err)
	}

	// Create metadata
	metadata := SpooledFile{
		ID:            batchID,
		TenantID:      tenantID,
		DatasetID:     datasetID,
		BearerToken:   bearerToken,
		Filename:      batchFilePath, // Full path to the batch file
		Size:          int64(len(data)),
		LineCount:     s.countLines(data),
		CreatedAt:     now,
		LastRetry:     time.Time{},
		RetryCount:    0,
		Status:        "pending",
		FailureReason: failureReason,
	}

	// Write metadata file
	metaData, err := json.Marshal(metadata)
	if err != nil {
		// Clean up batch file on metadata error
		os.Remove(batchFilePath)
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	metaFilePath := filepath.Join(metaDir, fmt.Sprintf("%s.meta", batchID))
	if err := os.WriteFile(metaFilePath, metaData, 0600); err != nil {
		// Clean up batch file on metadata error
		os.Remove(batchFilePath)
		return fmt.Errorf("failed to write metadata file: %w", err)
	}

	log.Infof("Stored failed batch %s to queue for tenant=%s dataset=%s",
		batchID, tenantID, datasetID)

	return nil
}

// SpoolData stores data locally when upload fails (legacy method for backward compatibility)
func (s *SpoolingService) SpoolData(tenantID, datasetID, bearerToken string, data []byte, failureReason string) error {
	if !s.config.Spooling.Enabled {
		return nil
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Ensure spool directory exists before spooling data
	if err := s.ensureSpoolDirectoryExists(); err != nil {
		return fmt.Errorf("failed to ensure spool directory exists: %w", err)
	}

	// Check size limit (per-tenant or global)
	dataSize := int64(len(data))
	if s.config.Spooling.PerTenantLimits {
		// Check per-tenant limit
		tenantSize, err := s.getTenantSize(tenantID)
		if err != nil {
			log.Warnf("Failed to get tenant size for %s: %v", tenantID, err)
			tenantSize = 0
		}
		if tenantSize+dataSize > s.maxSize {
			return fmt.Errorf("tenant spooling directory full for %s (current: %d + new: %d > max: %d)",
				tenantID, tenantSize, dataSize, s.maxSize)
		}
	} else {
		// Global size limit
		if s.currentSize+dataSize > s.maxSize {
			// Try cleanup first
			if err := s.cleanupOldFiles(); err != nil {
				log.Warnf("Failed to cleanup old files: %v", err)
			}

			// Check again
			if s.currentSize+dataSize > s.maxSize {
				return fmt.Errorf("spooling directory full (current: %d + new: %d > max: %d)",
					s.currentSize, dataSize, s.maxSize)
			}
		}
	}

	// Generate directory and file paths based on organization
	spoolDir, filename, id, err := s.generateSpoolPaths(tenantID, datasetID, data)
	if err != nil {
		return fmt.Errorf("failed to generate spool paths: %w", err)
	}

	// Create directory structure
	if err := os.MkdirAll(spoolDir, 0750); err != nil {
		return fmt.Errorf("failed to create spool directory %s: %w", spoolDir, err)
	}

	filePath := filepath.Join(spoolDir, filename)
	metaFilepath := filepath.Join(spoolDir, fmt.Sprintf("%s.meta", id))

	// Write data file
	if err := os.WriteFile(filePath, data, 0600); err != nil {
		return fmt.Errorf("failed to write spooled data file: %w", err)
	}

	// Count lines in the data
	lineCount := s.countLines(data)

	// Write metadata file
	metadata := SpooledFile{
		ID:            id,
		TenantID:      tenantID,
		DatasetID:     datasetID,
		BearerToken:   bearerToken,
		Filename:      filename,
		Size:          dataSize,
		LineCount:     lineCount,
		CreatedAt:     time.Now(),
		LastRetry:     time.Time{},
		RetryCount:    0,
		Status:        "pending",
		FailureReason: failureReason,
	}

	metaData, err := json.Marshal(metadata)
	if err != nil {
		// Clean up data file on metadata error
		os.Remove(filePath)
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	if err := os.WriteFile(metaFilepath, metaData, 0600); err != nil {
		// Clean up data file on metadata error
		os.Remove(filePath)
		return fmt.Errorf("failed to write metadata file: %w", err)
	}

	// Update current size
	s.currentSize += dataSize

	log.Debugf("Spooled data for %s/%s: %d bytes, reason: %s",
		tenantID, datasetID, dataSize, failureReason)

	return nil
}

// retryWorker periodically retries failed uploads
func (s *SpoolingService) retryWorker() {
	defer s.wg.Done()

	ticker := time.NewTicker(s.retryInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.shutdown:
			return
		case <-ticker.C:
			s.processRetries()
		}
	}
}

// getTenants returns list of tenant directories
func (s *SpoolingService) getTenants() ([]string, error) {
	entries, err := os.ReadDir(s.directory)
	if err != nil {
		return nil, fmt.Errorf("failed to read spool directory: %w", err)
	}

	var tenants []string
	for _, entry := range entries {
		if entry.IsDir() {
			tenants = append(tenants, entry.Name())
		}
	}
	return tenants, nil
}

// getTenantMetadataFiles returns metadata files for a specific tenant
func (s *SpoolingService) getTenantMetadataFiles(tenantID string) ([]SpooledFile, error) {
	metaDir := filepath.Join(s.directory, tenantID, "meta")

	entries, err := os.ReadDir(metaDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // No metadata directory
		}
		return nil, fmt.Errorf("failed to read meta directory: %w", err)
	}

	var files []SpooledFile
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".meta") {
			continue
		}

		metaPath := filepath.Join(metaDir, entry.Name())
		// #nosec G304 - metaPath is constructed from controlled metaDir and validated entry.Name()
		data, err := os.ReadFile(metaPath)
		if err != nil {
			log.Warnf("Failed to read metadata file %s: %v", metaPath, err)
			continue
		}

		var file SpooledFile
		if err := json.Unmarshal(data, &file); err != nil {
			log.Warnf("Failed to unmarshal metadata file %s: %v", metaPath, err)
			continue
		}

		files = append(files, file)
	}

	return files, nil
}

// removeMetadataFile removes a metadata file for a tenant
func (s *SpoolingService) removeMetadataFile(tenantID, fileID string) error {
	metaPath := filepath.Join(s.directory, tenantID, "meta", fmt.Sprintf("%s.meta", fileID))
	if err := os.Remove(metaPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove metadata file: %w", err)
	}
	return nil
}

// updateTenantRetryMetadata updates retry count and last retry time for a file
func (s *SpoolingService) updateTenantRetryMetadata(tenantID string, file SpooledFile, errorMessage string) {
	file.RetryCount++
	file.LastRetry = time.Now()
	file.FailureReason = errorMessage
	file.Status = "retrying"

	// Write updated metadata
	metaData, err := json.Marshal(file)
	if err != nil {
		log.Errorf("Failed to marshal updated metadata for %s: %v", file.ID, err)
		return
	}

	metaPath := filepath.Join(s.directory, tenantID, "meta", fmt.Sprintf("%s.meta", file.ID))
	if err := os.WriteFile(metaPath, metaData, 0600); err != nil {
		log.Errorf("Failed to write updated metadata for %s: %v", file.ID, err)
	}
}

// removeSuccessfulFile removes both the .gz file and metadata after successful upload
func (s *SpoolingService) removeSuccessfulFile(tenantID string, file SpooledFile) {
	// Remove the .gz file (file.Filename contains full path)
	if err := os.Remove(file.Filename); err != nil && !os.IsNotExist(err) {
		log.Warnf("Failed to remove queue file %s: %v", file.Filename, err)
	}

	// Remove the metadata file
	if err := s.removeMetadataFile(tenantID, file.ID); err != nil {
		log.Warnf("Failed to remove metadata for %s: %v", file.ID, err)
	}

	log.Debugf("Successfully removed files for %s", file.ID)
}

// moveToNewDLQ moves failed files to tenant/dlq/dataset/ structure
func (s *SpoolingService) moveToNewDLQ(file SpooledFile) error {
	// Create DLQ directory: tenant/dlq/dataset/
	dlqDir := filepath.Join(s.directory, file.TenantID, "dlq", file.DatasetID)
	if err := os.MkdirAll(dlqDir, 0750); err != nil {
		return fmt.Errorf("failed to create DLQ directory: %w", err)
	}

	// Move .gz file to DLQ
	dlqFilePath := filepath.Join(dlqDir, filepath.Base(file.Filename))
	if err := os.Rename(file.Filename, dlqFilePath); err != nil {
		// If rename fails, try copy and delete
		if copyErr := s.copyFile(file.Filename, dlqFilePath); copyErr != nil {
			return fmt.Errorf("failed to move .gz file to DLQ: %w", err)
		}
		os.Remove(file.Filename) // Best effort cleanup
	}

	// Update metadata and move to DLQ
	file.Status = "dlq"
	file.FailureReason = "Moved to DLQ after exceeding maximum retry attempts (4)"
	file.LastRetry = time.Now()
	file.Filename = dlqFilePath // Update path to DLQ location

	metaData, err := json.Marshal(file)
	if err != nil {
		return fmt.Errorf("failed to marshal DLQ metadata: %w", err)
	}

	// Write metadata to DLQ directory
	dlqMetaPath := filepath.Join(dlqDir, fmt.Sprintf("%s.meta", file.ID))
	if err := os.WriteFile(dlqMetaPath, metaData, 0600); err != nil {
		return fmt.Errorf("failed to write DLQ metadata: %w", err)
	}

	// Remove original metadata file from tenant/meta/
	originalMetaPath := filepath.Join(s.directory, file.TenantID, "meta", fmt.Sprintf("%s.meta", file.ID))
	os.Remove(originalMetaPath) // Best effort cleanup

	log.Warnf("Moved file %s to DLQ: %s", file.ID, dlqFilePath)
	return nil
}

// processRetries attempts to retry failed uploads
func (s *SpoolingService) processRetries() {
	// Ensure spool directory exists before processing retries
	if err := s.ensureSpoolDirectoryExists(); err != nil {
		log.Errorf("Failed to ensure spool directory exists: %v", err)
		return
	}

	// Process each tenant's metadata directory
	tenants, err := s.getTenants()
	if err != nil {
		log.Errorf("Failed to get tenants for retry processing: %v", err)
		return
	}

	forwarder := NewHTTPForwarder(s.config)
	totalSuccessCount := 0
	totalFailureCount := 0

	for _, tenantID := range tenants {
		files, err := s.getTenantMetadataFiles(tenantID)
		if err != nil {
			log.Errorf("Failed to get metadata files for tenant %s: %v", tenantID, err)
			continue
		}

		if len(files) == 0 {
			continue
		}

		log.Debugf("Processing %d metadata files for tenant %s", len(files), tenantID)
		successCount := 0
		failureCount := 0

		for _, file := range files {
			// Skip files that are permanently failed (fallback case if DLQ move failed)
			if file.Status == "failed" || file.Status == "dlq" {
				continue
			}

			// Check if it's time to retry
			if time.Since(file.LastRetry) < s.retryInterval {
				continue
			}

			// Check retry limit (4 times as requested)
			if file.RetryCount >= 4 {
				// Move to DLQ
				log.Warnf("File %s exceeded retry limit (4), moving to DLQ", file.ID)
				if dlqErr := s.moveToNewDLQ(file); dlqErr != nil {
					log.Errorf("Failed to move file %s to DLQ: %v", file.ID, dlqErr)
				}
				failureCount++
				continue
			}

			// Read .gz file data (file.Filename contains full path to .gz file)
			data, err := os.ReadFile(file.Filename)
			if err != nil {
				if os.IsNotExist(err) {
					log.Warnf("Queue file %s missing, removing orphaned metadata %s", file.Filename, file.ID)
					s.removeMetadataFile(tenantID, file.ID)
				} else {
					log.Errorf("Failed to read queue file %s: %v", file.Filename, err)
				}
				continue
			}

			// Create batch for retry
			batch := &domain.DataBatch{
				ID:          file.ID,
				TenantID:    file.TenantID,
				DatasetID:   file.DatasetID,
				BearerToken: file.BearerToken,
				Data:        data,
				CreatedAt:   file.CreatedAt,
			}

			// Attempt upload
			if err := forwarder.ForwardBatch(batch); err != nil {
				// Update retry count and last retry time
				s.updateTenantRetryMetadata(tenantID, file, err.Error())
				failureCount++
				log.Debugf("Retry failed for %s: %v", file.ID, err)
			} else {
				// Success - remove both .gz file and metadata
				s.removeSuccessfulFile(tenantID, file)
				successCount++
				log.Debugf("Retry succeeded for %s", file.ID)
			}
		}

		totalSuccessCount += successCount
		totalFailureCount += failureCount

		if successCount > 0 || failureCount > 0 {
			log.Infof("Tenant %s retry results: %d succeeded, %d failed", tenantID, successCount, failureCount)
		}
	}

	if totalSuccessCount > 0 || totalFailureCount > 0 {
		log.Infof("Total retry results: %d succeeded, %d failed", totalSuccessCount, totalFailureCount)
	}
}

// cleanupWorker periodically cleans up old files
func (s *SpoolingService) cleanupWorker() {
	defer s.wg.Done()

	ticker := time.NewTicker(s.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.shutdown:
			return
		case <-ticker.C:
			s.cleanupOldFiles()
		}
	}
}

// batchProcessor periodically processes raw files into compressed batches
func (s *SpoolingService) batchProcessor() {
	defer s.wg.Done()

	// Process batches every 30 seconds (configurable)
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.shutdown:
			return
		case <-ticker.C:
			s.processBatches()
		}
	}
}

// processBatches finds all tenants/datasets with raw files and processes them
func (s *SpoolingService) processBatches() {
	// Walk through tenant directories to find raw files
	tenants, err := s.getTenants()
	if err != nil {
		log.Warnf("Failed to get tenants for batch processing: %v", err)
		return
	}

	for _, tenantID := range tenants {
		datasets, err := s.getTenantDatasets(tenantID)
		if err != nil {
			log.Warnf("Failed to get datasets for tenant %s: %v", tenantID, err)
			continue
		}

		for _, datasetID := range datasets {
			// Check if raw directory has files
			rawDir := filepath.Join(s.directory, tenantID, datasetID, "raw")
			if _, err := os.Stat(rawDir); os.IsNotExist(err) {
				continue
			}

			rawFiles, err := os.ReadDir(rawDir)
			if err != nil {
				log.Warnf("Failed to read raw directory %s: %v", rawDir, err)
				continue
			}

			// Count .ndjson files
			var ndjsonCount int
			for _, file := range rawFiles {
				if strings.HasSuffix(file.Name(), ".ndjson") {
					ndjsonCount++
				}
			}

			// Process if we have raw files (could be configurable threshold)
			if ndjsonCount > 0 {
				log.Debugf("Processing %d raw files for tenant=%s dataset=%s", ndjsonCount, tenantID, datasetID)

				// Get bearer token from recent metadata files (fallback to global)
				bearerToken := s.config.BearerToken
				if tenantToken := s.getTenantBearerToken(tenantID); tenantToken != "" {
					bearerToken = tenantToken
				}

				if err := s.BatchRawFiles(tenantID, datasetID, bearerToken); err != nil {
					log.Warnf("Failed to batch raw files for tenant=%s dataset=%s: %v", tenantID, datasetID, err)
				}
			}
		}
	}
}

// getTenantDatasets returns all dataset IDs for a tenant
func (s *SpoolingService) getTenantDatasets(tenantID string) ([]string, error) {
	tenantDir := filepath.Join(s.directory, tenantID)
	entries, err := os.ReadDir(tenantDir)
	if err != nil {
		return nil, err
	}

	var datasets []string
	for _, entry := range entries {
		if entry.IsDir() && entry.Name() != "meta" && entry.Name() != "dlq" {
			datasets = append(datasets, entry.Name())
		}
	}

	return datasets, nil
}

// getTenantBearerToken gets the bearer token from recent metadata files for a tenant
func (s *SpoolingService) getTenantBearerToken(tenantID string) string {
	files, err := s.getTenantMetadataFiles(tenantID)
	if err != nil || len(files) == 0 {
		return ""
	}

	// Return bearer token from the most recent metadata file
	var mostRecent SpooledFile
	for _, file := range files {
		if mostRecent.CreatedAt.IsZero() || file.CreatedAt.After(mostRecent.CreatedAt) {
			mostRecent = file
		}
	}

	return mostRecent.BearerToken
}

// cleanupOldFiles removes old spooled files to free space
func (s *SpoolingService) cleanupOldFiles() error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Ensure spool directory exists before cleanup
	if err := s.ensureSpoolDirectoryExists(); err != nil {
		return fmt.Errorf("failed to ensure spool directory exists: %w", err)
	}

	files, err := s.getSpooledFiles()
	if err != nil {
		return fmt.Errorf("failed to get spooled files for cleanup: %w", err)
	}

	if s.config.Spooling.PerTenantLimits {
		return s.cleanupWithTenantPolicies(files)
	} else {
		return s.cleanupWithGlobalPolicy(files)
	}
}

// cleanupWithGlobalPolicy performs cleanup using global policies
func (s *SpoolingService) cleanupWithGlobalPolicy(files []SpooledFile) error {
	// Sort by age (oldest first)
	sort.Slice(files, func(i, j int) bool {
		return files[i].CreatedAt.Before(files[j].CreatedAt)
	})

	cleaned := 0
	for _, file := range files {
		if s.shouldCleanupFile(file) {
			if err := s.removeSpooledFile(file); err != nil {
				log.Warnf("Failed to remove old spooled file %s: %v", file.ID, err)
			} else {
				cleaned++
			}
		}
	}

	if cleaned > 0 {
		log.Infof("Global cleanup: removed %d old spooled files", cleaned)
	}

	return nil
}

// cleanupWithTenantPolicies performs cleanup using per-tenant policies
func (s *SpoolingService) cleanupWithTenantPolicies(files []SpooledFile) error {
	// Group files by tenant
	tenantFiles := make(map[string][]SpooledFile)
	for _, file := range files {
		tenantFiles[file.TenantID] = append(tenantFiles[file.TenantID], file)
	}

	totalCleaned := 0
	for tenantID, files := range tenantFiles {
		cleaned, err := s.cleanupTenantFiles(tenantID, files)
		if err != nil {
			log.Warnf("Failed to cleanup files for tenant %s: %v", tenantID, err)
			continue
		}
		totalCleaned += cleaned
	}

	if totalCleaned > 0 {
		log.Infof("Per-tenant cleanup: removed %d old spooled files across all tenants", totalCleaned)
	}

	return nil
}

// cleanupTenantFiles performs cleanup for a specific tenant
func (s *SpoolingService) cleanupTenantFiles(tenantID string, files []SpooledFile) (int, error) {
	// Sort by age (oldest first)
	sort.Slice(files, func(i, j int) bool {
		return files[i].CreatedAt.Before(files[j].CreatedAt)
	})

	// Check tenant size limits
	tenantSize, err := s.getTenantSize(tenantID)
	if err != nil {
		log.Warnf("Failed to get tenant size for %s: %v", tenantID, err)
		tenantSize = 0
	}

	cleaned := 0

	// First pass: remove files that should be cleaned up regardless of size
	for _, file := range files {
		if s.shouldCleanupFile(file) {
			if err := s.removeSpooledFile(file); err != nil {
				log.Warnf("Failed to remove old spooled file %s: %v", file.ID, err)
			} else {
				cleaned++
				tenantSize -= file.Size
			}
		}
	}

	// Second pass: if tenant is over size limit, remove oldest files
	if tenantSize > s.maxSize {
		log.Infof("Tenant %s over size limit (%d > %d), cleaning up oldest files",
			tenantID, tenantSize, s.maxSize)

		for _, file := range files {
			if tenantSize <= s.maxSize {
				break
			}

			// Skip files already cleaned up
			if s.shouldCleanupFile(file) {
				continue
			}

			if err := s.removeSpooledFile(file); err != nil {
				log.Warnf("Failed to remove spooled file %s for size limit: %v", file.ID, err)
			} else {
				cleaned++
				tenantSize -= file.Size
				log.Debugf("Removed file %s for tenant %s size limit", file.ID, tenantID)
			}
		}
	}

	// Third pass: check per-dataset file limits
	if s.config.Spooling.MaxFilesPerDataset > 0 {
		datasetCounts := s.countFilesPerDataset(files)
		for datasetID, count := range datasetCounts {
			if count > s.config.Spooling.MaxFilesPerDataset {
				removed := s.cleanupDatasetFiles(tenantID, datasetID, files, count-s.config.Spooling.MaxFilesPerDataset)
				cleaned += removed
			}
		}
	}

	if cleaned > 0 {
		log.Debugf("Tenant %s cleanup: removed %d files", tenantID, cleaned)
	}

	return cleaned, nil
}

// shouldCleanupFile determines if a file should be cleaned up based on global policies
func (s *SpoolingService) shouldCleanupFile(file SpooledFile) bool {
	// Remove files that exceeded retry attempts
	if file.RetryCount >= s.retryAttempts {
		return true
	}

	// Remove files older than max age if configured
	if s.config.Spooling.MaxAgeDays > 0 {
		maxAge := time.Duration(s.config.Spooling.MaxAgeDays) * 24 * time.Hour
		if time.Since(file.CreatedAt) > maxAge {
			return true
		}
	}

	// Fallback: remove files that are very old (2x retry period)
	maxAge := time.Duration(s.retryAttempts) * s.retryInterval * 2
	return time.Since(file.CreatedAt) > maxAge
}

// countFilesPerDataset counts files by dataset for a tenant
func (s *SpoolingService) countFilesPerDataset(files []SpooledFile) map[string]int {
	counts := make(map[string]int)
	for _, file := range files {
		// Only count files that haven't been cleaned up already
		if !s.shouldCleanupFile(file) {
			counts[file.DatasetID]++
		}
	}
	return counts
}

// cleanupDatasetFiles removes excess files for a dataset
func (s *SpoolingService) cleanupDatasetFiles(tenantID, datasetID string, files []SpooledFile, toRemove int) int {
	// Find files for this dataset and sort by age (oldest first)
	var datasetFiles []SpooledFile
	for _, file := range files {
		if file.DatasetID == datasetID && !s.shouldCleanupFile(file) {
			datasetFiles = append(datasetFiles, file)
		}
	}

	sort.Slice(datasetFiles, func(i, j int) bool {
		return datasetFiles[i].CreatedAt.Before(datasetFiles[j].CreatedAt)
	})

	removed := 0
	for i := 0; i < toRemove && i < len(datasetFiles); i++ {
		file := datasetFiles[i]
		if err := s.removeSpooledFile(file); err != nil {
			log.Warnf("Failed to remove excess file %s for dataset %s/%s: %v",
				file.ID, tenantID, datasetID, err)
		} else {
			removed++
			log.Debugf("Removed excess file %s for dataset %s/%s (limit: %d)",
				file.ID, tenantID, datasetID, s.config.Spooling.MaxFilesPerDataset)
		}
	}

	return removed
}

// getSpooledFiles returns all spooled files with metadata
func (s *SpoolingService) getSpooledFiles() ([]SpooledFile, error) {
	var files []SpooledFile

	// Walk through the entire spooling directory tree to find .meta files
	err := filepath.Walk(s.directory, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip DLQ directory entirely - don't process files in DLQ
		if info.IsDir() && info.Name() == "DLQ" {
			return filepath.SkipDir
		}

		// Skip directories and non-meta files
		if info.IsDir() || !strings.HasSuffix(info.Name(), ".meta") {
			return nil
		}

		// Read and parse metadata file
		// #nosec G304 - path is from filepath.Walk, controlled traversal
		metaData, err := os.ReadFile(path)
		if err != nil {
			log.Warnf("Failed to read metadata file %s: %v", path, err)
			return nil // Continue walking
		}

		var file SpooledFile
		if err := json.Unmarshal(metaData, &file); err != nil {
			log.Warnf("Failed to unmarshal metadata file %s: %v", path, err)
			return nil // Continue walking
		}

		files = append(files, file)
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to walk spooling directory: %w", err)
	}

	return files, nil
}

// removeSpooledFile removes both data and metadata files
func (s *SpoolingService) removeSpooledFile(file SpooledFile) error {
	// Find the actual file paths based on organization structure
	dataPath, metaPath, err := s.findFilePaths(file)
	if err != nil {
		return fmt.Errorf("failed to find file paths for %s: %w", file.ID, err)
	}

	// Remove data file
	if err := os.Remove(dataPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove data file %s: %w", dataPath, err)
	}

	// Remove metadata file
	if err := os.Remove(metaPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove metadata file %s: %w", metaPath, err)
	}

	// Update current size
	s.currentSize -= file.Size

	// Try to remove empty directories
	s.cleanupEmptyDirs(filepath.Dir(dataPath))

	return nil
}

// markAsPermanentlyFailed moves permanently failed files to DLQ

// generateSpoolPaths generates directory and file paths based on organization strategy
func (s *SpoolingService) generateSpoolPaths(tenantID, datasetID string, data []byte) (string, string, string, error) {
	now := time.Now()
	batchID := fmt.Sprintf("batch%d", now.UnixNano())
	id := fmt.Sprintf("%s_%s_%s", now.Format("20060102-150405"), tenantID, datasetID)

	// Determine file extension based on compression
	var extension string
	if len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b {
		extension = ".ndjson.gz"
	} else {
		extension = ".ndjson"
	}

	var spoolDir, filename string

	switch s.config.Spooling.Organization {
	case "flat":
		// Legacy flat structure: timestamp_tenant_dataset.ndjson
		spoolDir = s.directory
		filename = fmt.Sprintf("%s%s", id, extension)

	case "tenant_dataset":
		// Tenant/Dataset hierarchy (default): tenant1/dataset1/20060102-150405-batch123.ndjson
		spoolDir = filepath.Join(s.directory, tenantID, datasetID)
		filename = fmt.Sprintf("%s-%s%s", now.Format("20060102-150405"), batchID, extension)

	case "date_tenant":
		// Date-based hierarchy: 2025/09/10/tenant1/dataset1/150405-batch123.ndjson
		spoolDir = filepath.Join(s.directory, now.Format("2006"), now.Format("01"), now.Format("02"), tenantID, datasetID)
		filename = fmt.Sprintf("%s-%s%s", now.Format("150405"), batchID, extension)

	case "protocol_tenant":
		// Protocol-aware hierarchy: netflow/tenant1/dataset1/20060102-150405-batch123.ndjson
		// Determine protocol from context (simplified - could be enhanced)
		protocol := "data" // Default protocol
		spoolDir = filepath.Join(s.directory, protocol, tenantID, datasetID)
		filename = fmt.Sprintf("%s-%s%s", now.Format("20060102-150405"), batchID, extension)

	case "":
		// Fallback to default if organization is somehow empty
		spoolDir = filepath.Join(s.directory, tenantID, datasetID)
		filename = fmt.Sprintf("%s-%s%s", now.Format("20060102-150405"), batchID, extension)

	default:
		return "", "", "", fmt.Errorf("unsupported spooling organization: '%s' (supported: flat, tenant_dataset, date_tenant, protocol_tenant)", s.config.Spooling.Organization)
	}

	return spoolDir, filename, id, nil
}

// getTenantSize calculates the total size of spooled files for a specific tenant
func (s *SpoolingService) getTenantSize(tenantID string) (int64, error) {
	var totalSize int64

	// Walk through all possible tenant directories based on organization
	var searchPaths []string

	switch s.config.Spooling.Organization {
	case "flat":
		// In flat organization, need to check all files
		return s.getTenantSizeFlat(tenantID)

	case "tenant_dataset":
		// tenant1/
		searchPaths = []string{filepath.Join(s.directory, tenantID)}

	case "date_tenant":
		// Search in date directories: 2025/09/10/tenant1/
		searchPaths = s.getDateTenantPaths(tenantID)

	case "protocol_tenant":
		// Search in protocol directories: netflow/tenant1/, sflow/tenant1/, etc.
		searchPaths = s.getProtocolTenantPaths(tenantID)
	}

	for _, path := range searchPaths {
		size, err := s.getDirectorySize(path)
		if err != nil {
			continue // Directory may not exist yet
		}
		totalSize += size
	}

	return totalSize, nil
}

// getTenantSizeFlat calculates tenant size in flat organization by checking filenames
func (s *SpoolingService) getTenantSizeFlat(tenantID string) (int64, error) {
	files, err := s.getSpooledFiles()
	if err != nil {
		return 0, err
	}

	var totalSize int64
	for _, file := range files {
		if file.TenantID == tenantID {
			totalSize += file.Size
		}
	}

	return totalSize, nil
}

// getDateTenantPaths returns paths for date-based tenant organization
func (s *SpoolingService) getDateTenantPaths(tenantID string) []string {
	var paths []string

	// Search in current and previous days/months (reasonable window)
	now := time.Now()
	for i := 0; i < 7; i++ { // Last 7 days
		date := now.AddDate(0, 0, -i)
		path := filepath.Join(s.directory, date.Format("2006"), date.Format("01"), date.Format("02"), tenantID)
		paths = append(paths, path)
	}

	return paths
}

// getProtocolTenantPaths returns paths for protocol-based tenant organization
func (s *SpoolingService) getProtocolTenantPaths(tenantID string) []string {
	var paths []string
	protocols := []string{"data", "netflow", "sflow", "syslog"} // Known protocols

	for _, protocol := range protocols {
		path := filepath.Join(s.directory, protocol, tenantID)
		paths = append(paths, path)
	}

	return paths
}

// getDirectorySize calculates the total size of .ndjson files in a directory tree
func (s *SpoolingService) getDirectorySize(dirPath string) (int64, error) {
	var totalSize int64

	err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() && (strings.HasSuffix(info.Name(), ".ndjson") || strings.HasSuffix(info.Name(), ".ndjson.gz")) {
			totalSize += info.Size()
		}

		return nil
	})

	return totalSize, err
}

// calculateCurrentSize calculates the current total size of spooled files
func (s *SpoolingService) calculateCurrentSize() error {
	var totalSize int64

	err := filepath.Walk(s.directory, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() && strings.HasSuffix(info.Name(), ".ndjson") {
			totalSize += info.Size()
		}

		return nil
	})

	if err != nil {
		return err
	}

	s.currentSize = totalSize
	log.Debugf("Current spooling size: %d bytes", s.currentSize)
	return nil
}

// countLines counts the number of lines in the data
func (s *SpoolingService) countLines(data []byte) int {
	if len(data) == 0 {
		return 0
	}

	var dataToCount []byte

	// Check if data is gzip compressed
	if len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b {
		// Decompress gzip data to count lines
		reader, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			log.Warnf("Failed to create gzip reader for line counting: %v", err)
			return 0
		}
		defer reader.Close()

		decompressed, err := io.ReadAll(reader)
		if err != nil {
			log.Warnf("Failed to decompress data for line counting: %v", err)
			return 0
		}
		dataToCount = decompressed
	} else {
		// Use uncompressed data directly
		dataToCount = data
	}

	// Count newline characters
	count := 0
	for _, b := range dataToCount {
		if b == '\n' {
			count++
		}
	}

	// If data doesn't end with newline, the last line still counts
	if len(dataToCount) > 0 && dataToCount[len(dataToCount)-1] != '\n' {
		count++
	}

	return count
}

// findFilePaths locates the actual file paths for a spooled file based on organization structure
func (s *SpoolingService) findFilePaths(file SpooledFile) (string, string, error) {
	// For hierarchical organizations, we need to search for the files
	// since the file.Filename only contains the basename
	var dataPath, metaPath string

	if s.config.Spooling.Organization == "flat" {
		// In flat organization, files are directly in the spooling directory
		dataPath = filepath.Join(s.directory, file.Filename)
		metaPath = filepath.Join(s.directory, fmt.Sprintf("%s.meta", file.ID))
		return dataPath, metaPath, nil
	}

	// For hierarchical organizations, search for the files
	found := false
	err := filepath.Walk(s.directory, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		// Check if this is our data file
		if info.Name() == file.Filename {
			dataPath = path
			metaPath = filepath.Join(filepath.Dir(path), fmt.Sprintf("%s.meta", file.ID))
			found = true
			return filepath.SkipDir // Stop walking once found
		}

		return nil
	})

	if err != nil {
		return "", "", fmt.Errorf("error searching for file %s: %w", file.Filename, err)
	}

	if !found {
		return "", "", fmt.Errorf("file %s not found in spooling directory", file.Filename)
	}

	return dataPath, metaPath, nil
}

// cleanupEmptyDirs removes empty directories up the hierarchy
func (s *SpoolingService) cleanupEmptyDirs(startDir string) {
	// Don't remove the root spooling directory
	if startDir == s.directory {
		return
	}

	// Check if directory is empty
	entries, err := os.ReadDir(startDir)
	if err != nil {
		return // Directory may not exist or other error
	}

	// If directory is empty, remove it and try parent
	if len(entries) == 0 {
		if err := os.Remove(startDir); err == nil {
			log.Debugf("Removed empty spooling directory: %s", startDir)
			// Recursively try to remove parent directories
			s.cleanupEmptyDirs(filepath.Dir(startDir))
		}
	}
}

// SpoolingStats represents detailed spooling statistics
type SpoolingStats struct {
	Enabled         bool                    `json:"enabled"`
	Organization    string                  `json:"organization"`
	TotalSizeBytes  int64                   `json:"total_size_bytes"`
	TotalFiles      int                     `json:"total_files"`
	PerTenantStats  map[string]*TenantStats `json:"per_tenant_stats"`
	StatusBreakdown map[string]int          `json:"status_breakdown"`
	MaxSizeBytes    int64                   `json:"max_size_bytes"`
	PerTenantLimits bool                    `json:"per_tenant_limits"`
}

// TenantStats represents statistics for a specific tenant
type TenantStats struct {
	TenantID        string                   `json:"tenant_id"`
	SizeBytes       int64                    `json:"size_bytes"`
	FileCount       int                      `json:"file_count"`
	PerDatasetStats map[string]*DatasetStats `json:"per_dataset_stats"`
}

// DatasetStats represents statistics for a specific dataset
type DatasetStats struct {
	DatasetID       string         `json:"dataset_id"`
	SizeBytes       int64          `json:"size_bytes"`
	FileCount       int            `json:"file_count"`
	OldestFile      string         `json:"oldest_file"`
	NewestFile      string         `json:"newest_file"`
	StatusBreakdown map[string]int `json:"status_breakdown"`
}

// GetStats returns basic spooling statistics (backward compatibility)
func (s *SpoolingService) GetStats() (int64, int, error) {
	if !s.config.Spooling.Enabled {
		return 0, 0, nil
	}

	s.mutex.RLock()
	defer s.mutex.RUnlock()

	files, err := s.getSpooledFiles()
	if err != nil {
		return s.currentSize, 0, err
	}

	return s.currentSize, len(files), nil
}

// GetDetailedStats returns comprehensive spooling statistics
func (s *SpoolingService) GetDetailedStats() (*SpoolingStats, error) {
	if !s.config.Spooling.Enabled {
		return &SpoolingStats{
			Enabled:      false,
			Organization: s.config.Spooling.Organization,
		}, nil
	}

	s.mutex.RLock()
	defer s.mutex.RUnlock()

	files, err := s.getSpooledFiles()
	if err != nil {
		return nil, fmt.Errorf("failed to get spooled files for stats: %w", err)
	}

	stats := &SpoolingStats{
		Enabled:         true,
		Organization:    s.config.Spooling.Organization,
		TotalSizeBytes:  s.currentSize,
		TotalFiles:      len(files),
		PerTenantStats:  make(map[string]*TenantStats),
		StatusBreakdown: make(map[string]int),
		MaxSizeBytes:    s.maxSize,
		PerTenantLimits: s.config.Spooling.PerTenantLimits,
	}

	// Process files and build statistics
	for _, file := range files {
		// Update global status breakdown
		stats.StatusBreakdown[file.Status]++

		// Get or create tenant stats
		tenantStats, exists := stats.PerTenantStats[file.TenantID]
		if !exists {
			tenantStats = &TenantStats{
				TenantID:        file.TenantID,
				SizeBytes:       0,
				FileCount:       0,
				PerDatasetStats: make(map[string]*DatasetStats),
			}
			stats.PerTenantStats[file.TenantID] = tenantStats
		}

		// Update tenant stats
		tenantStats.SizeBytes += file.Size
		tenantStats.FileCount++

		// Get or create dataset stats
		datasetStats, exists := tenantStats.PerDatasetStats[file.DatasetID]
		if !exists {
			datasetStats = &DatasetStats{
				DatasetID:       file.DatasetID,
				SizeBytes:       0,
				FileCount:       0,
				OldestFile:      file.CreatedAt.Format(time.RFC3339),
				NewestFile:      file.CreatedAt.Format(time.RFC3339),
				StatusBreakdown: make(map[string]int),
			}
			tenantStats.PerDatasetStats[file.DatasetID] = datasetStats
		}

		// Update dataset stats
		datasetStats.SizeBytes += file.Size
		datasetStats.FileCount++
		datasetStats.StatusBreakdown[file.Status]++

		// Update oldest/newest timestamps
		if file.CreatedAt.Before(parseRFC3339(datasetStats.OldestFile)) {
			datasetStats.OldestFile = file.CreatedAt.Format(time.RFC3339)
		}
		if file.CreatedAt.After(parseRFC3339(datasetStats.NewestFile)) {
			datasetStats.NewestFile = file.CreatedAt.Format(time.RFC3339)
		}
	}

	return stats, nil
}

// parseRFC3339 safely parses RFC3339 timestamp, returns zero time on error
func parseRFC3339(timeStr string) time.Time {
	t, err := time.Parse(time.RFC3339, timeStr)
	if err != nil {
		return time.Time{}
	}
	return t
}

// GetTenantStats returns statistics for a specific tenant
func (s *SpoolingService) GetTenantStats(tenantID string) (*TenantStats, error) {
	if !s.config.Spooling.Enabled {
		return nil, fmt.Errorf("spooling is disabled")
	}

	stats, err := s.GetDetailedStats()
	if err != nil {
		return nil, err
	}

	tenantStats, exists := stats.PerTenantStats[tenantID]
	if !exists {
		return &TenantStats{
			TenantID:        tenantID,
			SizeBytes:       0,
			FileCount:       0,
			PerDatasetStats: make(map[string]*DatasetStats),
		}, nil
	}

	return tenantStats, nil
}

// GetDatasetStats returns statistics for a specific dataset
func (s *SpoolingService) GetDatasetStats(tenantID, datasetID string) (*DatasetStats, error) {
	if !s.config.Spooling.Enabled {
		return nil, fmt.Errorf("spooling is disabled")
	}

	tenantStats, err := s.GetTenantStats(tenantID)
	if err != nil {
		return nil, err
	}

	datasetStats, exists := tenantStats.PerDatasetStats[datasetID]
	if !exists {
		return &DatasetStats{
			DatasetID:       datasetID,
			SizeBytes:       0,
			FileCount:       0,
			StatusBreakdown: make(map[string]int),
		}, nil
	}

	return datasetStats, nil
}

// copyFile copies a file from src to dst (paths are validated by caller)
func (s *SpoolingService) copyFile(src, dst string) error {
	// #nosec G304 - src path validated by findFilePaths function in moveToDeadLetterQueue caller
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	// #nosec G304 - dst path is constructed within DLQ directory by moveToDeadLetterQueue caller
	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}

// ensureSpoolDirectoryExists checks if the root spool directory exists and recreates it if necessary
func (s *SpoolingService) ensureSpoolDirectoryExists() error {
	if _, err := os.Stat(s.directory); os.IsNotExist(err) {
		log.Warnf("Spool directory %s missing, recreating it", s.directory)
		if err := os.MkdirAll(s.directory, 0750); err != nil {
			return fmt.Errorf("failed to recreate spool directory %s: %w", s.directory, err)
		}
		log.Infof("Successfully recreated spool directory %s", s.directory)
	} else if err != nil {
		return fmt.Errorf("failed to check spool directory %s: %w", s.directory, err)
	}
	return nil
}

// DLQStats represents comprehensive DLQ statistics
type DLQStats struct {
	TotalFilesInQueue int                        `json:"total_files_in_queue"`
	TotalFilesInDLQ   int                        `json:"total_files_in_dlq"`
	TotalBytesInQueue int64                      `json:"total_bytes_in_queue"`
	TotalBytesInDLQ   int64                      `json:"total_bytes_in_dlq"`
	TenantStats       map[string]*TenantDLQStats `json:"tenant_stats"`
	OldestQueueFile   *SpooledFile               `json:"oldest_queue_file,omitempty"`
	OldestDLQFile     *SpooledFile               `json:"oldest_dlq_file,omitempty"`
}

// TenantDLQStats represents per-tenant DLQ statistics
type TenantDLQStats struct {
	QueueFiles   int                         `json:"queue_files"`
	DLQFiles     int                         `json:"dlq_files"`
	QueueBytes   int64                       `json:"queue_bytes"`
	DLQBytes     int64                       `json:"dlq_bytes"`
	DatasetStats map[string]*DatasetDLQStats `json:"dataset_stats"`
}

// DatasetDLQStats represents per-dataset DLQ statistics
type DatasetDLQStats struct {
	QueueFiles int   `json:"queue_files"`
	DLQFiles   int   `json:"dlq_files"`
	QueueBytes int64 `json:"queue_bytes"`
	DLQBytes   int64 `json:"dlq_bytes"`
}

// DLQRetryResult represents the result of DLQ retry operation
type DLQRetryResult struct {
	FilesRetried int              `json:"files_retried"`
	Details      []DLQRetryDetail `json:"details,omitempty"`
}

// DLQRetryDetail represents details of a single file retry operation
type DLQRetryDetail struct {
	FileID    string `json:"file_id"`
	TenantID  string `json:"tenant_id"`
	DatasetID string `json:"dataset_id"`
	Success   bool   `json:"success"`
	Error     string `json:"error,omitempty"`
}

// GetDLQStats returns comprehensive DLQ statistics
func (s *SpoolingService) GetDLQStats() (*DLQStats, error) {
	if !s.config.Spooling.Enabled {
		return &DLQStats{
			TenantStats: make(map[string]*TenantDLQStats),
		}, nil
	}

	s.mutex.RLock()
	defer s.mutex.RUnlock()

	stats := &DLQStats{
		TenantStats: make(map[string]*TenantDLQStats),
	}

	// Get all tenants
	tenants, err := s.getTenants()
	if err != nil {
		return nil, fmt.Errorf("failed to get tenants: %w", err)
	}

	var oldestQueueFile, oldestDLQFile *SpooledFile

	for _, tenantID := range tenants {
		tenantStats := &TenantDLQStats{
			DatasetStats: make(map[string]*DatasetDLQStats),
		}

		// Get datasets for this tenant
		datasets, err := s.getTenantDatasets(tenantID)
		if err != nil {
			continue // Skip this tenant if we can't get datasets
		}

		// Process queue files
		for _, datasetID := range datasets {
			datasetStats := &DatasetDLQStats{}

			// Count queue files
			queueDir := filepath.Join(s.directory, tenantID, datasetID, "queue")
			queueFiles, queueBytes, queueOldest := s.countFilesInDirectory(queueDir, ".ndjson.gz")
			datasetStats.QueueFiles = queueFiles
			datasetStats.QueueBytes = queueBytes

			if queueOldest != nil && (oldestQueueFile == nil || queueOldest.CreatedAt.Before(oldestQueueFile.CreatedAt)) {
				oldestQueueFile = queueOldest
			}

			// Count DLQ files
			dlqDir := filepath.Join(s.directory, tenantID, "dlq", datasetID)
			dlqFiles, dlqBytes, dlqOldest := s.countFilesInDirectory(dlqDir, ".ndjson.gz")
			datasetStats.DLQFiles = dlqFiles
			datasetStats.DLQBytes = dlqBytes

			if dlqOldest != nil && (oldestDLQFile == nil || dlqOldest.CreatedAt.Before(oldestDLQFile.CreatedAt)) {
				oldestDLQFile = dlqOldest
			}

			// Add to tenant stats
			tenantStats.QueueFiles += datasetStats.QueueFiles
			tenantStats.DLQFiles += datasetStats.DLQFiles
			tenantStats.QueueBytes += datasetStats.QueueBytes
			tenantStats.DLQBytes += datasetStats.DLQBytes
			tenantStats.DatasetStats[datasetID] = datasetStats
		}

		// Add to overall stats
		stats.TotalFilesInQueue += tenantStats.QueueFiles
		stats.TotalFilesInDLQ += tenantStats.DLQFiles
		stats.TotalBytesInQueue += tenantStats.QueueBytes
		stats.TotalBytesInDLQ += tenantStats.DLQBytes
		stats.TenantStats[tenantID] = tenantStats
	}

	stats.OldestQueueFile = oldestQueueFile
	stats.OldestDLQFile = oldestDLQFile

	return stats, nil
}

// countFilesInDirectory counts files in a directory and returns count, total bytes, and oldest file
func (s *SpoolingService) countFilesInDirectory(dirPath, extension string) (int, int64, *SpooledFile) {
	files, err := os.ReadDir(dirPath)
	if err != nil {
		return 0, 0, nil
	}

	var count int
	var totalBytes int64
	var oldestFile *SpooledFile

	for _, file := range files {
		if !strings.HasSuffix(file.Name(), extension) {
			continue
		}

		info, err := file.Info()
		if err != nil {
			continue
		}

		count++
		totalBytes += info.Size()

		// Try to find corresponding metadata to get creation time
		baseName := strings.TrimSuffix(file.Name(), extension)
		metaFiles := []string{
			filepath.Join(dirPath, baseName+".meta"),                             // DLQ meta files
			filepath.Join(filepath.Dir(dirPath), "..", "meta", baseName+".meta"), // Queue meta files
		}

		var createdAt time.Time
		found := false
		for _, metaPath := range metaFiles {
			// #nosec G304 - metaPath is constructed from controlled directories and validated filenames
			metaData, err := os.ReadFile(metaPath)
			if err != nil {
				continue
			}

			var spooledFile SpooledFile
			if err := json.Unmarshal(metaData, &spooledFile); err == nil {
				createdAt = spooledFile.CreatedAt
				if oldestFile == nil || createdAt.Before(oldestFile.CreatedAt) {
					oldestFile = &spooledFile
				}
				found = true
				break
			}
		}

		// Fallback to file modification time if no metadata found
		if !found {
			createdAt = info.ModTime()
			spooledFile := &SpooledFile{
				ID:        baseName,
				Filename:  filepath.Join(dirPath, file.Name()),
				Size:      info.Size(),
				CreatedAt: createdAt,
			}
			if oldestFile == nil || createdAt.Before(oldestFile.CreatedAt) {
				oldestFile = spooledFile
			}
		}
	}

	return count, totalBytes, oldestFile
}

// RetryDLQFiles moves DLQ files back to the processing queue
func (s *SpoolingService) RetryDLQFiles(tenantID, datasetID string) (*DLQRetryResult, error) {
	if !s.config.Spooling.Enabled {
		return nil, fmt.Errorf("spooling is disabled")
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()

	result := &DLQRetryResult{
		Details: make([]DLQRetryDetail, 0),
	}

	// Get tenants to process
	var tenants []string
	if tenantID != "" {
		tenants = []string{tenantID}
	} else {
		allTenants, err := s.getTenants()
		if err != nil {
			return nil, fmt.Errorf("failed to get tenants: %w", err)
		}
		tenants = allTenants
	}

	for _, tenant := range tenants {
		// Get datasets to process
		var datasets []string
		if datasetID != "" {
			datasets = []string{datasetID}
		} else {
			allDatasets, err := s.getTenantDatasets(tenant)
			if err != nil {
				continue // Skip this tenant
			}
			datasets = allDatasets
		}

		for _, dataset := range datasets {
			dlqDir := filepath.Join(s.directory, tenant, "dlq", dataset)
			queueDir := filepath.Join(s.directory, tenant, dataset, "queue")
			metaDir := filepath.Join(s.directory, tenant, "meta")

			// Create queue and meta directories if they don't exist
			if err := os.MkdirAll(queueDir, 0750); err != nil {
				detail := DLQRetryDetail{
					TenantID:  tenant,
					DatasetID: dataset,
					Success:   false,
					Error:     fmt.Sprintf("failed to create queue directory: %v", err),
				}
				result.Details = append(result.Details, detail)
				continue
			}
			if err := os.MkdirAll(metaDir, 0750); err != nil {
				detail := DLQRetryDetail{
					TenantID:  tenant,
					DatasetID: dataset,
					Success:   false,
					Error:     fmt.Sprintf("failed to create meta directory: %v", err),
				}
				result.Details = append(result.Details, detail)
				continue
			}

			// Process DLQ files
			dlqFiles, err := os.ReadDir(dlqDir)
			if err != nil {
				if !os.IsNotExist(err) {
					detail := DLQRetryDetail{
						TenantID:  tenant,
						DatasetID: dataset,
						Success:   false,
						Error:     fmt.Sprintf("failed to read DLQ directory: %v", err),
					}
					result.Details = append(result.Details, detail)
				}
				continue
			}

			for _, file := range dlqFiles {
				if !strings.HasSuffix(file.Name(), ".ndjson.gz") && !strings.HasSuffix(file.Name(), ".ndjson") {
					continue
				}

				baseName := strings.TrimSuffix(file.Name(), filepath.Ext(file.Name()))
				baseName = strings.TrimSuffix(baseName, ".ndjson")

				// Move data file
				srcPath := filepath.Join(dlqDir, file.Name())
				dstPath := filepath.Join(queueDir, file.Name())
				if err := os.Rename(srcPath, dstPath); err != nil {
					detail := DLQRetryDetail{
						FileID:    baseName,
						TenantID:  tenant,
						DatasetID: dataset,
						Success:   false,
						Error:     fmt.Sprintf("failed to move data file: %v", err),
					}
					result.Details = append(result.Details, detail)
					continue
				}

				// Move metadata file
				srcMetaPath := filepath.Join(dlqDir, baseName+".meta")
				dstMetaPath := filepath.Join(metaDir, baseName+".meta")

				if _, err := os.Stat(srcMetaPath); err == nil {
					// Read and update metadata
					// #nosec G304 - srcMetaPath is constructed from controlled dlqDir and validated baseName
					metaData, err := os.ReadFile(srcMetaPath)
					if err == nil {
						var spooledFile SpooledFile
						if err := json.Unmarshal(metaData, &spooledFile); err == nil {
							// Reset for retry
							spooledFile.Status = "pending"
							spooledFile.RetryCount = 0
							spooledFile.LastRetry = time.Time{}
							spooledFile.FailureReason = ""
							spooledFile.Filename = dstPath

							// Write updated metadata
							updatedMetaData, err := json.Marshal(spooledFile)
							if err == nil {
								if err := os.WriteFile(dstMetaPath, updatedMetaData, 0600); err == nil {
									os.Remove(srcMetaPath) // Remove old metadata
								}
							}
						}
					}
				}

				result.FilesRetried++
				detail := DLQRetryDetail{
					FileID:    baseName,
					TenantID:  tenant,
					DatasetID: dataset,
					Success:   true,
				}
				result.Details = append(result.Details, detail)

				log.Infof("Retried DLQ file: %s (tenant=%s, dataset=%s)", baseName, tenant, dataset)
			}
		}
	}

	return result, nil
}

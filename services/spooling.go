package services

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bytedance/sonic"
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

	// Dedicated HTTP client for retry processing
	retryForwarder *HTTPForwarder

	// Tenant validation
	tenantValidator *TenantValidator

	// Runtime state
	currentSize int64
	mutex       sync.RWMutex
	shutdown    chan struct{}
	wg          sync.WaitGroup

	// Immediate retry trigger channel
	retryTrigger chan struct{}
}

// SpooledFile represents a file in the spooling directory
type SpooledFile struct {
	ID            string    `json:"id"`
	TenantID      string    `json:"tenant_id"`
	DatasetID     string    `json:"dataset_id"`
	BearerToken   string    `json:"bearer_token,omitempty"` // Authentication token for this tenant
	Filename         string    `json:"filename"`
	CompressedSize   int64     `json:"compressed_size"`     // Compressed size on disk
	UncompressedSize int64     `json:"uncompressed_size"`   // Original data size before compression
	LineCount        int       `json:"line_count"`
	CreatedAt     time.Time `json:"created_at"`
	LastRetry     time.Time `json:"last_retry"`
	RetryCount    int       `json:"retry_count"`
	Status        string    `json:"status"` // "pending", "retrying", "failed", "success"
	FailureReason string    `json:"failure_reason,omitempty"`
	TriggerReason string    `json:"trigger_reason,omitempty"` // Reason batch was created: "timeout", "size_limit_reached", "service_shutdown"
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
		retryForwarder:  NewRetryHTTPForwarder(cfg),          // Dedicated HTTP client for retry processing
		tenantValidator: NewTenantValidator(&cfg.TenantValidation, cfg.ControlURL), // Layer 4: Proactive tenant validation
		shutdown:        make(chan struct{}),
		retryTrigger:    make(chan struct{}, 100), // Buffered channel to prevent blocking
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

	// Process any orphaned queue files from previous session
	if err := s.processOrphanedQueueFiles(); err != nil {
		log.Warnf("Failed to process orphaned queue files: %v", err)
	}

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
func (s *SpoolingService) StoreRawMessage(tenantID, datasetID, bearerToken string, data []byte, dataHint string) error {
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

	// Count lines for verification tracking
	lineCount := s.countLines(data)

	// Get appropriate file extension based on data hint
	extension := getFileExtensionForDataHint(dataHint)

	// Generate unique filename with line count for verification (data should already be validated at input)
	now := time.Now()
	filename := fmt.Sprintf("%d_%d_%d.%s", now.UnixNano(), len(data), lineCount, extension)
	filePath := filepath.Join(rawDir, filename)

	// Write message (data should already be validated at input)
	if err := os.WriteFile(filePath, data, 0600); err != nil {
		return fmt.Errorf("failed to write raw message file: %w", err)
	}

	log.Debugf("Stored raw message for tenant=%s dataset=%s: %s (%d lines, %d bytes)",
		tenantID, datasetID, filename, lineCount, len(data))
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

	// Create queue directory only - metadata created when files move to retry/dlq
	if err := os.MkdirAll(queueDir, 0750); err != nil {
		return fmt.Errorf("failed to create queue directory: %w", err)
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

	// Group files by data hint (extension)
	filesByDataHint := make(map[string][]string)

	for _, file := range rawFiles {
		// Skip directories and invalid files
		if file.IsDir() {
			continue
		}

		dataHint := extractDataHintFromFilename(file.Name())
		if filesByDataHint[dataHint] == nil {
			filesByDataHint[dataHint] = make([]string, 0)
		}
		filesByDataHint[dataHint] = append(filesByDataHint[dataHint], file.Name())
	}

	// Process each data hint group separately
	for dataHint, files := range filesByDataHint {
		if len(files) == 0 {
			continue
		}

		// Combine files of the same data hint
		var combinedData bytes.Buffer
		var processedFiles []string
		var totalBytes int64

		for _, fileName := range files {
			filePath := filepath.Join(rawDir, fileName)
			// #nosec G304 - filePath is constructed from controlled rawDir and validated fileName
			data, err := os.ReadFile(filePath)
			if err != nil {
				log.Warnf("Failed to read raw file %s: %v", filePath, err)
				continue
			}

			// For line-based formats (ndjson, csv, syslog), ensure each file ends with newline
			combinedData.Write(data)
			if len(data) > 0 && isLineBasedFormat(dataHint) && data[len(data)-1] != '\n' {
				combinedData.WriteByte('\n')
			}

			processedFiles = append(processedFiles, filePath)
			totalBytes += int64(len(data))
		}

		if len(processedFiles) == 0 {
			continue // No valid files processed for this data hint
		}

		// Generate batch file for this data hint
		now := time.Now()
		batchFileName := generateProxyFilename(tenantID, datasetID, now, dataHint)
		batchID := strings.TrimSuffix(batchFileName, ".gz")

		// Compress data
		var compressed bytes.Buffer
		gzipWriter, err := gzip.NewWriterLevel(&compressed, 6)
		if err != nil {
			log.Errorf("Failed to create gzip writer for %s: %v", dataHint, err)
			continue
		}

		if _, err := gzipWriter.Write(combinedData.Bytes()); err != nil {
			log.Errorf("Failed to compress %s data: %v", dataHint, err)
			gzipWriter.Close()
			continue
		}

		if err := gzipWriter.Close(); err != nil {
			log.Errorf("Failed to close gzip writer for %s: %v", dataHint, err)
			continue
		}

		// Write compressed batch to queue - filename already in new format
		batchFilePath := filepath.Join(queueDir, batchFileName)

		if err := os.WriteFile(batchFilePath, compressed.Bytes(), 0600); err != nil {
			log.Errorf("Failed to write compressed batch for %s: %v", dataHint, err)
			continue
		}

		// Note: Queue files don't need metadata - metadata is created only when files move to retry/dlq

		// Remove processed raw files
		for _, filePath := range processedFiles {
			if err := os.Remove(filePath); err != nil {
				log.Warnf("Failed to remove processed raw file %s: %v", filePath, err)
			}
		}

		log.Infof("Created batch %s from %d raw files (data hint: %s) for tenant=%s dataset=%s",
			batchID, len(processedFiles), dataHint, tenantID, datasetID)
	}

	return nil
}

// StoreBatchToQueue stores already-compressed batch data directly to the queue directory
func (s *SpoolingService) StoreBatchToQueue(tenantID, datasetID, bearerToken string, data []byte, failureReason string, batchID string, triggerReason string, fileExtension string) error {
	if !s.config.Spooling.Enabled {
		return nil
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Create queue directory only - no metadata needed for queue files
	queueDir := filepath.Join(s.directory, tenantID, datasetID, "queue")

	if err := os.MkdirAll(queueDir, 0750); err != nil {
		return fmt.Errorf("failed to create queue directory: %w", err)
	}

	// Use the new filename format: tenant--dataset--timestamp--extension.gz
	// The batchID from batching contains timestamp, so we need to parse or regenerate

	// Generate new format filename - batchID contains timestamp, extract it
	var batchFileName string
	if strings.Contains(batchID, "--") {
		// Already in new format (batchID is actually filename without .gz)
		batchFileName = batchID + ".gz"
	} else {
		// Old format batchID, need to convert to new format
		// Extract timestamp from batchID format: "20250924-103045--tenant--dataset"
		parts := strings.Split(batchID, "--")
		if len(parts) >= 3 {
			// Parse timestamp from batch ID
			timeStr := parts[0] // "20250924-103045"
			if timestamp, err := time.Parse("20060102-150405", timeStr); err == nil {
				// Use new filename format
				batchFileName = generateProxyFilename(tenantID, datasetID, timestamp, fileExtension)
			} else {
				// Fallback: use current time
				batchFileName = generateProxyFilename(tenantID, datasetID, time.Now(), fileExtension)
			}
		} else {
			// Fallback: use current time with new format
			batchFileName = generateProxyFilename(tenantID, datasetID, time.Now(), fileExtension)
		}
	}
	batchFilePath := filepath.Join(queueDir, batchFileName)

	if err := os.WriteFile(batchFilePath, data, 0600); err != nil {
		return fmt.Errorf("failed to write batch to queue: %w", err)
	}

	// No metadata created for queue files - they're meant for immediate processing
	actualFilename := filepath.Base(batchFilePath)
	log.Debugf("Stored file %s to queue for tenant=%s dataset=%s (no metadata needed)",
		actualFilename, tenantID, datasetID)

	return nil
}


// SpoolData stores data locally when upload fails (maintained for API compatibility)
func (s *SpoolingService) SpoolData(tenantID, datasetID, bearerToken string, data []byte, failureReason string, triggerReason string) error {
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

	// For SpoolData, the data is written as-is (no compression), so:
	// - UncompressedSize = original data size
	// - CompressedSize = same as Size (no compression applied here)
	uncompressedSize := dataSize
	compressedSize := dataSize

	// Write metadata file
	metadata := SpooledFile{
		ID:               id,
		TenantID:         tenantID,
		DatasetID:        datasetID,
		BearerToken:      bearerToken,
		Filename:         filename,
		CompressedSize:   compressedSize,     // File size on disk
		UncompressedSize: uncompressedSize,   // Original data size (same as compressed here)
		LineCount:        lineCount,
		CreatedAt:        time.Now(),
		LastRetry:        time.Time{},
		RetryCount:       0,
		Status:           "pending",
		FailureReason:    failureReason,
		TriggerReason:    triggerReason,
	}

	metaData, err := sonic.Marshal(metadata)
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
		case <-s.retryTrigger:
			// Immediate retry triggered by DLQ resubmit
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

// getTenantMetadataFiles returns metadata files from retry/ and dlq/ directories for a tenant
func (s *SpoolingService) getTenantMetadataFiles(tenantID string) ([]SpooledFile, error) {
	var allFiles []SpooledFile

	// Get all datasets for this tenant
	datasets, err := s.getTenantDatasets(tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to get datasets for tenant %s: %w", tenantID, err)
	}

	// Collect metadata from retry/ and dlq/ directories (no metadata in queue/)
	for _, datasetID := range datasets {
		// Check retry directory
		retryDir := filepath.Join(s.directory, tenantID, datasetID, "retry")
		retryFiles := s.getMetadataFromDirectory(retryDir)
		allFiles = append(allFiles, retryFiles...)

		// Check dlq directory
		dlqDir := filepath.Join(s.directory, tenantID, datasetID, "dlq")
		dlqFiles := s.getMetadataFromDirectory(dlqDir)
		allFiles = append(allFiles, dlqFiles...)
	}

	return allFiles, nil
}

// getMetadataFromDirectory reads all .meta files from a directory
func (s *SpoolingService) getMetadataFromDirectory(dir string) []SpooledFile {
	var files []SpooledFile

	entries, err := os.ReadDir(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Warnf("Failed to read directory %s: %v", dir, err)
		}
		return files
	}

	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".meta") {
			continue
		}

		metaPath := filepath.Join(dir, entry.Name())
		// #nosec G304 - metaPath is constructed from controlled dir and validated entry.Name()
		data, err := os.ReadFile(metaPath)
		if err != nil {
			log.Warnf("Failed to read metadata file %s: %v", metaPath, err)
			continue
		}

		var file SpooledFile
		if err := sonic.Unmarshal(data, &file); err != nil {
			log.Warnf("Failed to unmarshal metadata file %s: %v", metaPath, err)
			continue
		}

		files = append(files, file)
	}

	return files
}

// Legacy methods removed - retry/dlq files now manage their own metadata through concurrent worker system

// moveToNewDLQ moves failed files to tenant/dataset/dlq/ structure
func (s *SpoolingService) moveToNewDLQ(file SpooledFile) error {
	// Create DLQ directory: tenant/dataset/dlq/
	dlqDir := filepath.Join(s.directory, file.TenantID, file.DatasetID, "dlq")
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

	metaData, err := sonic.Marshal(file)
	if err != nil {
		return fmt.Errorf("failed to marshal DLQ metadata: %w", err)
	}

	// Write metadata to DLQ directory
	dlqMetaPath := filepath.Join(dlqDir, fmt.Sprintf("%s.meta", file.ID))
	if err := os.WriteFile(dlqMetaPath, metaData, 0600); err != nil {
		return fmt.Errorf("failed to write DLQ metadata: %w", err)
	}

	// Remove original metadata file from retry/ directory
	originalMetaPath := filepath.Join(s.directory, file.TenantID, file.DatasetID, "retry", fmt.Sprintf("%s.meta", file.ID))
	if err := os.Remove(originalMetaPath); err != nil && !os.IsNotExist(err) {
		log.Warnf("Failed to remove original metadata file %s: %v", originalMetaPath, err)
	}

	log.Warnf("Moved file %s to DLQ: %s", file.ID, dlqFilePath)
	return nil
}

// RetryJob represents a retry task for a worker
type RetryJob struct {
	TenantID   string
	DatasetID  string
	BatchID    string
	FilePath   string
	MetaPath   string
}

// processRetries attempts to retry failed uploads from both queue/ and retry/ directories using concurrent workers
func (s *SpoolingService) processRetries() {
	// Ensure spool directory exists before processing retries
	if err := s.ensureSpoolDirectoryExists(); err != nil {
		log.Errorf("Failed to ensure spool directory exists: %v", err)
		return
	}

	// Process each tenant's files
	tenants, err := s.getTenants()
	if err != nil {
		log.Errorf("Failed to get tenants for retry processing: %v", err)
		return
	}

	// Collect all retry jobs
	var jobs []RetryJob
	log.Debugf("Found %d tenants for retry processing: %v", len(tenants), tenants)
	for _, tenantID := range tenants {
		tenantJobs := s.collectRetryJobs(tenantID)
		log.Debugf("Tenant %s: collected %d retry jobs", tenantID, len(tenantJobs))
		jobs = append(jobs, tenantJobs...)
	}

	if len(jobs) == 0 {
		log.Debugf("No retry jobs found for processing")
		return // No jobs to process
	}

	log.Infof("Processing %d retry jobs with %d upload workers", len(jobs), s.config.GetUploadWorkerCount())

	// Create job channel and results channel
	jobChannel := make(chan RetryJob, len(jobs))
	resultChannel := make(chan struct {
		success bool
		tenantID string
		batchID string
	}, len(jobs))

	// Start worker pool
	numWorkers := s.config.GetUploadWorkerCount()
	var wg sync.WaitGroup

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go s.retryJobWorker(i, jobChannel, resultChannel, &wg)
	}

	// Send jobs to workers
	for _, job := range jobs {
		jobChannel <- job
	}
	close(jobChannel)

	// Wait for all workers to complete
	wg.Wait()
	close(resultChannel)

	// Collect results
	totalSuccessCount := 0
	totalFailureCount := 0
	tenantResults := make(map[string]struct{ success, failed int })

	for result := range resultChannel {
		if result.success {
			totalSuccessCount++
		} else {
			totalFailureCount++
		}

		stats := tenantResults[result.tenantID]
		if result.success {
			stats.success++
		} else {
			stats.failed++
		}
		tenantResults[result.tenantID] = stats
	}

	// Log results
	for tenantID, stats := range tenantResults {
		if stats.success > 0 || stats.failed > 0 {
			log.Infof("Tenant %s retry results: %d succeeded, %d failed", tenantID, stats.success, stats.failed)
		}
	}

	if totalSuccessCount > 0 || totalFailureCount > 0 {
		log.Infof("Total retry results: %d succeeded, %d failed", totalSuccessCount, totalFailureCount)
	}
}

// collectRetryJobs collects all retry jobs for a tenant from both queue/ and retry/ directories
func (s *SpoolingService) collectRetryJobs(tenantID string) []RetryJob {
	var jobs []RetryJob

	// Get all datasets for this tenant
	datasets, err := s.getTenantDatasets(tenantID)
	if err != nil {
		log.Errorf("Failed to get datasets for tenant %s: %v", tenantID, err)
		return jobs
	}

	for _, datasetID := range datasets {
		// Process queue directory first (files that haven't been attempted yet)
		queueDir := filepath.Join(s.directory, tenantID, datasetID, "queue")
		queueJobs := s.collectQueueJobs(tenantID, datasetID, queueDir)
		jobs = append(jobs, queueJobs...)

		// Then process retry directory (files that have failed before)
		retryDir := filepath.Join(s.directory, tenantID, datasetID, "retry")

		// Check if retry directory exists
		if _, err := os.Stat(retryDir); os.IsNotExist(err) {
			continue
		}

		// Read retry directory
		entries, err := os.ReadDir(retryDir)
		if err != nil {
			log.Errorf("Failed to read retry directory %s: %v", retryDir, err)
			continue
		}

		log.Debugf("Found %d entries in retry directory %s", len(entries), retryDir)

		// Process each file
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}

			fileName := entry.Name()
			if !strings.HasSuffix(fileName, ".meta") {
				continue // Only process .meta metadata files
			}

			// Extract batch ID from filename (remove .meta extension)
			batchID := strings.TrimSuffix(fileName, ".meta")
			metaFilePath := filepath.Join(retryDir, fileName)

			// Find the corresponding data file (with any extension: .raw.gz, .csv.gz, .ndjson.gz, etc.)
			var dataFilePath string
			retryFiles, err := os.ReadDir(retryDir)
			if err != nil {
				log.Warnf("Failed to read retry directory for batch %s: %v", batchID, err)
				continue
			}

			for _, file := range retryFiles {
				if strings.HasPrefix(file.Name(), batchID+".") && !strings.HasSuffix(file.Name(), ".meta") {
					dataFilePath = filepath.Join(retryDir, file.Name())
					break
				}
			}

			if dataFilePath == "" {
				log.Warnf("No data file found for batch %s in retry directory", batchID)
				continue
			}

			jobs = append(jobs, RetryJob{
				TenantID:  tenantID,
				DatasetID: datasetID,
				BatchID:   batchID,
				FilePath:  dataFilePath,
				MetaPath:  metaFilePath,
			})
		}
	}

	return jobs
}

// collectQueueJobs collects jobs from the queue directory (files that haven't been attempted yet)
func (s *SpoolingService) collectQueueJobs(tenantID, datasetID, queueDir string) []RetryJob {
	var jobs []RetryJob

	// Check if queue directory exists
	if _, err := os.Stat(queueDir); os.IsNotExist(err) {
		return jobs
	}

	// Read queue directory
	entries, err := os.ReadDir(queueDir)
	if err != nil {
		log.Errorf("Failed to read queue directory %s: %v", queueDir, err)
		return jobs
	}

	log.Debugf("Found %d entries in queue directory %s", len(entries), queueDir)

	// Process each data file in queue (no metadata files in queue)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		fileName := entry.Name()

		// Extract batch ID from filename (remove file extension)
		var batchID string
		if strings.Contains(fileName, ".") {
			// Find the first dot to get the base filename
			parts := strings.Split(fileName, ".")
			batchID = parts[0]
		} else {
			batchID = fileName
		}

		dataFilePath := filepath.Join(queueDir, fileName)

		// Create retry job for queue file (no metadata file path)
		job := RetryJob{
			TenantID:  tenantID,
			DatasetID: datasetID,
			BatchID:   batchID,
			FilePath:  dataFilePath,
			MetaPath:  "", // Queue files don't have metadata
		}

		jobs = append(jobs, job)
		log.Debugf("Added queue job for batch %s from file %s", batchID, fileName)
	}

	return jobs
}

// retryJobWorker processes retry jobs from the job channel
func (s *SpoolingService) retryJobWorker(workerID int, jobChannel <-chan RetryJob, resultChannel chan<- struct {
	success bool
	tenantID string
	batchID string
}, wg *sync.WaitGroup) {
	defer wg.Done()

	log.Debugf("Retry worker %d started (using dedicated retry HTTP client)", workerID)

	for job := range jobChannel {
		log.Debugf("Worker %d processing retry job for batch %s", workerID, job.BatchID)

		success := s.processRetryJob(job, s.retryForwarder)

		resultChannel <- struct {
			success bool
			tenantID string
			batchID string
		}{
			success:  success,
			tenantID: job.TenantID,
			batchID:  job.BatchID,
		}
	}

	log.Debugf("Retry worker %d finished", workerID)
}

// processRetryJob processes a single retry job (either from queue or retry directory)
func (s *SpoolingService) processRetryJob(job RetryJob, forwarder *HTTPForwarder) bool {
	if job.MetaPath == "" {
		// Queue job - no metadata, attempt direct upload
		return s.processQueueJob(job, forwarder)
	}

	// Retry job - has metadata, process normally
	metadata, err := s.readRetryMetadata(job.MetaPath)
	if err != nil {
		log.Errorf("Failed to read metadata for batch %s: %v", job.BatchID, err)
		return false
	}

	// Check if we should retry this file
	if metadata.RetryCount >= s.config.Spooling.RetryAttempts {
		log.Warnf("Batch %s exceeded max retries (%d), moving to DLQ", job.BatchID, s.config.Spooling.RetryAttempts)

		// Report critical error to control service
		if s.config.ErrorReporter != nil {
			s.config.ErrorReporter.ReportCritical(
				context.Background(),
				"upload_max_retries_exceeded",
				fmt.Sprintf("File moved to DLQ after %d retry attempts", s.config.Spooling.RetryAttempts),
				metadata.TenantID,
				metadata.DatasetID,
			)
		}

		if err := s.moveToDLQ(metadata, "max_retries_exceeded"); err != nil {
			log.Errorf("Failed to move batch %s to DLQ: %v", job.BatchID, err)
		}
		return false
	}

	// Try to upload the file
	success := s.attemptRetryUpload(metadata, forwarder)

	if success {
		// Remove successful file and metadata
		actualFilename := filepath.Base(metadata.Filename)
		log.Infof("✅ Retry upload successful for file %s (%d bytes compressed, %d lines)",
			actualFilename, metadata.CompressedSize, metadata.LineCount)

		if err := s.removeSuccessfulRetryFile(metadata); err != nil {
			log.Errorf("Failed to cleanup successful retry file %s: %v", metadata.ID, err)
		}
		return true
	} else {
		// Update retry count and metadata
		metadata.RetryCount++
		metadata.LastRetry = time.Now()

		if err := s.updateRetryMetadata(metadata); err != nil {
			log.Errorf("Failed to update retry metadata for batch %s: %v", metadata.ID, err)
		}
		return false
	}
}

// processQueueJob processes a queue job (file without metadata)
func (s *SpoolingService) processQueueJob(job RetryJob, forwarder *HTTPForwarder) bool {
	// Read the data file directly
	data, err := os.ReadFile(job.FilePath)
	if err != nil {
		log.Errorf("Failed to read queue file %s: %v", job.FilePath, err)
		return false
	}

	// Extract data hint from filename for proper DataBatch creation
	fileName := filepath.Base(job.FilePath)
	dataHint := extractDataHint(fileName)
	if dataHint == "" {
		dataHint = "raw" // default fallback
	}

	// Line count verification - extract expected count from filename and compare with actual
	actualLineCount := s.countLines(data)
	expectedLineCount := extractLineCountFromFilename(fileName)
	if expectedLineCount > 0 && expectedLineCount != actualLineCount {
		log.Warnf("Line count mismatch in %s: expected %d, actual %d - possible data corruption",
			fileName, expectedLineCount, actualLineCount)
	} else {
		log.Debugf("Line count verified for %s: %d lines", fileName, actualLineCount)
	}

	// Create a DataBatch for upload
	batch := &domain.DataBatch{
		ID:            job.BatchID,
		TenantID:      job.TenantID,
		DatasetID:     job.DatasetID,
		Data:          data,
		DataHint:      dataHint,
		Filename:      fileName, // Preserve original filename
		CreatedAt:     time.Now(),
		TotalBytes:    int64(len(data)),
		LineCount:     actualLineCount, // Use verified actual count
	}

	// Validate tenant is active before attempting upload (Layer 4 optimization)
	if s.tenantValidator != nil && s.tenantValidator.IsEnabled() {
		if !s.tenantValidator.IsActiveTenant(job.TenantID) {
			log.Warnf("Tenant %s is inactive - skipping upload and moving to DLQ", job.TenantID)

			// Report warning to control service
			if s.config.ErrorReporter != nil {
				s.config.ErrorReporter.ReportWarning(
					context.Background(),
					"tenant_inactive",
					fmt.Sprintf("Data received for inactive tenant: %s", job.TenantID),
					job.TenantID,
					job.DatasetID,
				)
			}

			// Move directly to DLQ for inactive tenants (no retry)
			if moveErr := s.MoveQueueToDLQ(job.TenantID, job.DatasetID, job.BatchID, "tenant inactive or not found"); moveErr != nil {
				log.Errorf("Failed to move queue file %s to DLQ: %v", fileName, moveErr)
			}
			return false
		}
	}

	// Attempt upload
	err = forwarder.ForwardBatch(batch)
	if err != nil {
		// Check if this is a permanent failure (don't retry)
		if uploadErr, ok := err.(*UploadError); ok && uploadErr.IsPermanent {
			log.Warnf("Queue file upload failed permanently for %s (HTTP %d) - moving directly to DLQ", fileName, uploadErr.StatusCode)

			// Report error to control service
			if s.config.ErrorReporter != nil {
				s.config.ErrorReporter.ReportErrorSimple(
					context.Background(),
					"upload_permanent_failure",
					fmt.Sprintf("HTTP %d: %s", uploadErr.StatusCode, uploadErr.Message),
					"error",
					job.TenantID,
					job.DatasetID,
				)
			}

			// Move directly to DLQ for permanent failures (no retry)
			if moveErr := s.MoveQueueToDLQ(job.TenantID, job.DatasetID, job.BatchID, uploadErr.FailureReason); moveErr != nil {
				log.Errorf("Failed to move queue file %s to DLQ: %v", fileName, moveErr)
			}
			return false
		}

		// Transient failure - move to retry queue
		log.Warnf("Queue file upload failed for %s: %v - moving to retry", fileName, err)
		if moveErr := s.MoveQueueToRetry(job.TenantID, job.DatasetID, job.BatchID, err.Error(), "upload_failed"); moveErr != nil {
			log.Errorf("Failed to move queue file %s to retry: %v", fileName, moveErr)
		}
		return false
	}

	// Success - remove from queue
	log.Infof("✅ Queue file upload successful for %s (%d bytes)", fileName, len(data))
	if err := os.Remove(job.FilePath); err != nil {
		log.Errorf("Failed to remove successful queue file %s: %v", job.FilePath, err)
	}
	return true
}

// readRetryMetadata reads metadata from a retry metadata file
func (s *SpoolingService) readRetryMetadata(metaPath string) (*SpooledFile, error) {
	// #nosec G304 - metaPath is constructed from controlled retry directory paths and validated filenames
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read metadata file %s: %w", metaPath, err)
	}

	var metadata SpooledFile
	if err := sonic.Unmarshal(data, &metadata); err != nil {
		return nil, fmt.Errorf("failed to unmarshal metadata file %s: %w", metaPath, err)
	}

	return &metadata, nil
}

// attemptRetryUpload attempts to upload a file to the receiver
func (s *SpoolingService) attemptRetryUpload(metadata *SpooledFile, forwarder *HTTPForwarder) bool {
	// Read the data file
	data, err := os.ReadFile(metadata.Filename)
	if err != nil {
		log.Errorf("Failed to read retry file %s: %v", metadata.Filename, err)
		return false
	}

	// Extract data hint from filename for retry processing
	dataHint := extractDataHint(metadata.Filename)

	// Create a batch for upload
	batch := &domain.DataBatch{
		ID:            metadata.ID,
		TenantID:      metadata.TenantID,
		DatasetID:     metadata.DatasetID,
		Data:          data,
		LineCount:     metadata.LineCount,
		TotalBytes:    metadata.CompressedSize,
		CreatedAt:     metadata.CreatedAt,
		BearerToken:   metadata.BearerToken,
		DataHint:      dataHint, // Extract from filename to fix malformed filenames
		Filename:      filepath.Base(metadata.Filename), // Preserve original filename
	}

	// Try to upload
	err = forwarder.ForwardBatch(batch)
	if err != nil {
		// Check if this is a permanent failure
		if uploadErr, ok := err.(*UploadError); ok && uploadErr.IsPermanent {
			log.Warnf("Retry upload failed permanently for %s (HTTP %d) - will move to DLQ", metadata.ID, uploadErr.StatusCode)
			// Return false so the retry processor moves it to DLQ
			return false
		}
		// Transient failure - will retry again
		return false
	}
	return true
}

// removeSuccessfulRetryFile removes a successfully uploaded retry file and its metadata
func (s *SpoolingService) removeSuccessfulRetryFile(metadata *SpooledFile) error {
	// Remove data file
	if err := os.Remove(metadata.Filename); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove data file %s: %w", metadata.Filename, err)
	}

	// Remove metadata file (.meta extension)
	// Extract batch ID from data file path - new format: tenant--dataset--timestamp--extension.gz
	dataFile := filepath.Base(metadata.Filename)
	batchID := strings.TrimSuffix(dataFile, ".gz")  // New format: just remove .gz suffix
	retryDir := filepath.Dir(metadata.Filename)
	metaPath := filepath.Join(retryDir, batchID+".meta")

	if err := os.Remove(metaPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove metadata file %s: %w", metaPath, err)
	}

	return nil
}

// updateRetryMetadata updates metadata for a retry file
func (s *SpoolingService) updateRetryMetadata(metadata *SpooledFile) error {
	// Extract batch ID from data file path - new format: tenant--dataset--timestamp--extension.gz
	dataFile := filepath.Base(metadata.Filename)
	batchID := strings.TrimSuffix(dataFile, ".gz")  // New format: just remove .gz suffix
	retryDir := filepath.Dir(metadata.Filename)
	metaPath := filepath.Join(retryDir, batchID+".meta")

	metaData, err := sonic.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	if err := os.WriteFile(metaPath, metaData, 0600); err != nil {
		return fmt.Errorf("failed to write metadata file %s: %w", metaPath, err)
	}

	return nil
}

// moveToDLQ moves a file to the DLQ directory
func (s *SpoolingService) moveToDLQ(metadata *SpooledFile, reason string) error {
	metadata.FailureReason = reason
	metadata.Status = "dlq"
	return s.moveToNewDLQ(*metadata)
}

// cleanupWorker periodically cleans up old files and monitors DLQ
func (s *SpoolingService) cleanupWorker() {
	defer s.wg.Done()

	ticker := time.NewTicker(s.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.shutdown:
			return
		case <-ticker.C:
			s.moveAgedFilesToDLQ()  // Move old undeliverable files to DLQ first
			s.cleanupOldFiles()     // Then cleanup only safe files
			s.monitorDLQAndSpace()
		}
	}
}

// batchProcessor periodically processes raw files into compressed batches
func (s *SpoolingService) batchProcessor() {
	defer s.wg.Done()

	// Process batches every N seconds (configurable)
	ticker := time.NewTicker(time.Duration(s.config.GetQueueProcessingInterval()) * time.Second)
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

			// Count all raw files (any extension)
			var rawFileCount int
			for _, file := range rawFiles {
				if !file.IsDir() {
					rawFileCount++
				}
			}

			// Process if we have raw files (could be configurable threshold)
			if rawFileCount > 0 {
				log.Debugf("Processing %d raw files for tenant=%s dataset=%s", rawFileCount, tenantID, datasetID)

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
				tenantSize -= file.CompressedSize
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
				tenantSize -= file.CompressedSize
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
	// NEVER delete DLQ files - they contain data that couldn't be delivered
	if file.Status == "dlq" {
		return false
	}

	// NEVER delete files that contain undelivered data - move them to DLQ instead
	// The only files safe to delete are those that have been successfully uploaded
	// or are temporary/metadata files that don't contain actual data

	// Only cleanup files that are successfully processed (this should rarely happen
	// since successful files are removed immediately, but serves as safety)
	if file.Status == "success" {
		return true
	}

	// For any other files (pending, retry, failed), DO NOT delete them
	// They should be moved to DLQ by the retry process if they can't be delivered
	log.Debugf("File %s with status '%s' not eligible for cleanup - contains undelivered data",
		file.ID, file.Status)

	return false
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
		if info.IsDir() && (info.Name() == "DLQ" || info.Name() == "dlq") {
			return filepath.SkipDir
		}

		// Extra safety: skip any file in a path containing "dlq"
		if strings.Contains(strings.ToLower(path), "/dlq/") {
			return nil
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
		if err := sonic.Unmarshal(metaData, &file); err != nil {
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
	s.currentSize -= file.CompressedSize

	// Try to remove empty directories
	s.cleanupEmptyDirs(filepath.Dir(dataPath))

	return nil
}

// markAsPermanentlyFailed moves permanently failed files to DLQ

// generateSpoolPaths generates directory and file paths using new filename format
func (s *SpoolingService) generateSpoolPaths(tenantID, datasetID string, data []byte) (string, string, string, error) {
	now := time.Now()

	// Determine file extension based on compression
	var fileExtension string
	if len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b {
		fileExtension = "ndjson"  // Will become: tenant--dataset--timestamp--ndjson.gz
	} else {
		fileExtension = "ndjson"  // Will become: tenant--dataset--timestamp--ndjson.gz (uncompressed data, but named .gz for consistency)
	}

	// Generate filename in new format
	filename := generateProxyFilename(tenantID, datasetID, now, fileExtension)
	id := strings.TrimSuffix(filename, ".gz") // Use filename without .gz as ID for consistency

	var spoolDir string

	switch s.config.Spooling.Organization {
	case "flat":
		// Legacy flat structure - now using new format
		spoolDir = s.directory
		// filename already generated above in new format

	case "tenant_dataset":
		// Tenant/Dataset hierarchy (default) - now using new format
		spoolDir = filepath.Join(s.directory, tenantID, datasetID)
		// filename already generated above in new format

	case "date_tenant":
		// Date-based hierarchy - now using new format
		spoolDir = filepath.Join(s.directory, now.Format("2006"), now.Format("01"), now.Format("02"), tenantID, datasetID)
		// filename already generated above in new format

	case "protocol_tenant":
		// Protocol-aware hierarchy - now using new format
		// Determine protocol from context (simplified - could be enhanced)
		protocol := "data" // Default protocol
		spoolDir = filepath.Join(s.directory, protocol, tenantID, datasetID)
		// filename already generated above in new format

	case "":
		// Fallback to default if organization is somehow empty - now using new format
		spoolDir = filepath.Join(s.directory, tenantID, datasetID)
		// filename already generated above in new format

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
		// Search in protocol directories
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
			totalSize += file.CompressedSize
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
	protocols := []string{"data", "syslog"} // Known protocols

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

		if !info.IsDir() && !strings.HasSuffix(info.Name(), ".meta") {
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

		if !info.IsDir() && !strings.HasSuffix(info.Name(), ".meta") {
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

// safeReadFile safely reads a file with path validation to prevent directory traversal
func (s *SpoolingService) safeReadFile(filePath string) ([]byte, error) {
	// Clean the path to prevent directory traversal
	cleanPath := filepath.Clean(filePath)

	// Ensure the path is within our spooling directory
	if !strings.HasPrefix(cleanPath, s.directory) {
		return nil, fmt.Errorf("file path outside spooling directory: %s", cleanPath)
	}

	// Additional validation: ensure it's a data file we expect
	if strings.HasSuffix(cleanPath, ".meta") {
		return nil, fmt.Errorf("invalid file type for line counting: %s", cleanPath)
	}

	// Safe to read the file now
	return os.ReadFile(cleanPath)
}

// getUncompressedSize returns the uncompressed size of a gzip file
func (s *SpoolingService) getUncompressedSize(filePath string) (int64, error) {
	fileData, err := s.safeReadFile(filePath)
	if err != nil {
		return 0, err
	}

	// Check if file is gzipped
	if len(fileData) < 2 || fileData[0] != 0x1f || fileData[1] != 0x8b {
		// Not gzipped, return the actual file size
		return int64(len(fileData)), nil
	}

	// File is gzipped, decompress to get original size
	reader, err := gzip.NewReader(bytes.NewReader(fileData))
	if err != nil {
		return 0, fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer reader.Close()

	// Count bytes in decompressed data
	var uncompressedSize int64
	buffer := make([]byte, 32*1024) // 32KB buffer
	for {
		n, err := reader.Read(buffer)
		uncompressedSize += int64(n)
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, fmt.Errorf("failed to read decompressed data: %w", err)
		}
	}

	return uncompressedSize, nil
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

	// Count non-empty lines only
	count := 0
	lines := bytes.Split(dataToCount, []byte("\n"))

	for _, line := range lines {
		// Only count lines that have content (not just whitespace)
		if len(bytes.TrimSpace(line)) > 0 {
			count++
		}
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
		tenantStats.SizeBytes += file.CompressedSize
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
		datasetStats.SizeBytes += file.CompressedSize
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
			queueFiles, queueBytes, queueOldest := s.countFilesInDirectory(queueDir, "")
			datasetStats.QueueFiles = queueFiles
			datasetStats.QueueBytes = queueBytes

			if queueOldest != nil && (oldestQueueFile == nil || queueOldest.CreatedAt.Before(oldestQueueFile.CreatedAt)) {
				oldestQueueFile = queueOldest
			}

			// Count DLQ files
			dlqDir := filepath.Join(s.directory, tenantID, datasetID, "dlq")
			dlqFiles, dlqBytes, dlqOldest := s.countFilesInDirectory(dlqDir, "")
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
		// Skip metadata files and directories
		if strings.HasSuffix(file.Name(), ".meta") || file.IsDir() {
			continue
		}

		// If extension is specified, filter by it; otherwise count all data files
		if extension != "" && !strings.HasSuffix(file.Name(), extension) {
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
			if err := sonic.Unmarshal(metaData, &spooledFile); err == nil {
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
				ID:             baseName,
				Filename:       filepath.Join(dirPath, file.Name()),
				CompressedSize: info.Size(),
				CreatedAt:      createdAt,
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

	// Get tenants to process - support wildcard "*"
	var tenants []string
	if tenantID != "" && tenantID != "*" {
		tenants = []string{tenantID}
	} else {
		allTenants, err := s.getTenants()
		if err != nil {
			return nil, fmt.Errorf("failed to get tenants: %w", err)
		}
		tenants = allTenants
	}

	for _, tenant := range tenants {
		// Get datasets to process - support wildcard "*"
		var datasets []string
		if datasetID != "" && datasetID != "*" {
			datasets = []string{datasetID}
		} else {
			allDatasets, err := s.getTenantDatasets(tenant)
			if err != nil {
				continue // Skip this tenant
			}
			datasets = allDatasets
		}

		for _, dataset := range datasets {
			dlqDir := filepath.Join(s.directory, tenant, dataset, "dlq")
			retryDir := filepath.Join(s.directory, tenant, dataset, "retry")

			// Create retry directory if it doesn't exist
			if err := os.MkdirAll(retryDir, 0750); err != nil {
				detail := DLQRetryDetail{
					TenantID:  tenant,
					DatasetID: dataset,
					Success:   false,
					Error:     fmt.Sprintf("failed to create retry directory: %v", err),
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
				// Skip metadata files, only process data files (any extension with .gz or without)
				if strings.HasSuffix(file.Name(), ".meta") {
					continue
				}

				// Extract base filename (everything before the first dot)
				baseName := file.Name()
				if dotIndex := strings.Index(baseName, "."); dotIndex > 0 {
					baseName = baseName[:dotIndex]
				}

				// Move data file from DLQ to retry
				srcPath := filepath.Join(dlqDir, file.Name())
				dstPath := filepath.Join(retryDir, file.Name())
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

				// Move and reset metadata file
				srcMetaPath := filepath.Join(dlqDir, baseName+".meta")
				dstMetaPath := filepath.Join(retryDir, baseName+".meta")

				if _, err := os.Stat(srcMetaPath); err == nil {
					// Read and update metadata
					// #nosec G304 - srcMetaPath is constructed from controlled dlqDir and validated baseName
					metaData, err := os.ReadFile(srcMetaPath)
					if err == nil {
						var spooledFile SpooledFile
						if err := sonic.Unmarshal(metaData, &spooledFile); err == nil {
							// Reset for retry - give files fresh retry attempts
							spooledFile.Status = "retry"
							spooledFile.RetryCount = 0
							spooledFile.LastRetry = time.Now()
							spooledFile.FailureReason = "Retrieved from DLQ for retry"
							spooledFile.Filename = dstPath

							// Write updated metadata
							updatedMetaData, err := sonic.Marshal(spooledFile)
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

	// Trigger immediate retry processing if any files were moved
	if result.FilesRetried > 0 {
		select {
		case s.retryTrigger <- struct{}{}:
		default:
			// Channel full, retry will happen on next scheduled interval
		}
	}

	return result, nil
}

// ListDLQFiles returns a list of all files in the DLQ, optionally filtered by tenant and dataset
func (s *SpoolingService) ListDLQFiles(tenantID, datasetID string) ([]SpooledFile, error) {
	if !s.config.Spooling.Enabled {
		return nil, fmt.Errorf("spooling is disabled")
	}

	const maxFiles = 100 // Limit to prevent DoS attacks
	var dlqFiles []SpooledFile

	tenants := []string{}
	if tenantID != "" {
		tenants = append(tenants, tenantID)
	} else {
		// Get all tenants
		allTenants, err := s.getTenants()
		if err != nil {
			return nil, fmt.Errorf("failed to get tenants: %w", err)
		}
		tenants = allTenants
	}

	for _, tenant := range tenants {
		datasets := []string{}
		if datasetID != "" {
			datasets = append(datasets, datasetID)
		} else {
			// Get all datasets for this tenant
			allDatasets, err := s.getTenantDatasets(tenant)
			if err != nil {
				log.Debugf("Failed to get datasets for tenant %s: %v", tenant, err)
				continue
			}
			datasets = allDatasets
		}

		for _, dataset := range datasets {
			// Get DLQ directory path for this tenant/dataset
			dlqDir := filepath.Join(s.directory, tenant, dataset, "dlq")

			if _, err := os.Stat(dlqDir); os.IsNotExist(err) {
				continue // DLQ directory doesn't exist for this tenant/dataset
			}

			// Read DLQ directory
			entries, err := os.ReadDir(dlqDir)
			if err != nil {
				log.Debugf("Failed to read DLQ directory %s: %v", dlqDir, err)
				continue
			}

			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}

				// Only process .meta files
				if !strings.HasSuffix(entry.Name(), ".meta") {
					continue
				}

				// Validate filename to prevent directory traversal
				if strings.Contains(entry.Name(), "..") || strings.Contains(entry.Name(), "/") || strings.Contains(entry.Name(), "\\") {
					log.Warnf("Suspicious filename detected, skipping: %s", entry.Name())
					continue
				}

				metaPath := filepath.Join(dlqDir, entry.Name())

				// Additional security check: ensure the resolved path is still within dlqDir
				cleanPath := filepath.Clean(metaPath)
				if !strings.HasPrefix(cleanPath, filepath.Clean(dlqDir)+string(filepath.Separator)) &&
					cleanPath != filepath.Clean(dlqDir) {
					log.Warnf("Path traversal attempt detected, skipping: %s", entry.Name())
					continue
				}

				metaData, err := os.ReadFile(cleanPath)
				if err != nil {
					log.Debugf("Failed to read metadata file %s: %v", metaPath, err)
					continue
				}

				var spooledFile SpooledFile
				if err := sonic.Unmarshal(metaData, &spooledFile); err != nil {
					log.Debugf("Failed to unmarshal metadata file %s: %v", metaPath, err)
					continue
				}

				// Verify the file still exists
				if _, err := os.Stat(spooledFile.Filename); os.IsNotExist(err) {
					log.Debugf("DLQ file %s does not exist, skipping", spooledFile.Filename)
					continue
				}

				dlqFiles = append(dlqFiles, spooledFile)

				// Limit to prevent DoS attacks
				if len(dlqFiles) >= maxFiles {
					log.Debugf("DLQ file list truncated to %d files (limit reached)", maxFiles)
					return dlqFiles, nil
				}
			}
		}
	}

	return dlqFiles, nil
}

// MoveQueueToRetry moves a file from queue directory to retry directory with failure reason
func (s *SpoolingService) MoveQueueToRetry(tenantID, datasetID, batchID, failureReason string, triggerReason string) error {
	if !s.config.Spooling.Enabled {
		return nil
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Create retry directory
	retryDir := filepath.Join(s.directory, tenantID, datasetID, "retry")
	if err := os.MkdirAll(retryDir, 0750); err != nil {
		return fmt.Errorf("failed to create retry directory: %w", err)
	}

	// Source paths in queue (no metadata in queue)
	queueDir := filepath.Join(s.directory, tenantID, datasetID, "queue")

	// Find the batch file with new format: batch.ID = "tenant--dataset--timestamp"
	// Queue filename = "tenant--dataset--timestamp--extension.gz"
	// So we need to find files that start with batchID + "--" (not batchID + ".")
	var srcDataFile string
	files, err := os.ReadDir(queueDir)
	if err != nil {
		return fmt.Errorf("failed to read queue directory: %w", err)
	}

	for _, file := range files {
		fileName := file.Name()
		// New format: batchID.gz (batchID already includes extension)
		if fileName == batchID+".gz" {
			srcDataFile = filepath.Join(queueDir, fileName)
			break
		}
	}

	if srcDataFile == "" {
		return fmt.Errorf("batch file %s not found in queue", batchID)
	}

	// Destination paths in retry
	dstDataFile := filepath.Join(retryDir, filepath.Base(srcDataFile))
	dstMetaFile := filepath.Join(retryDir, batchID+".meta")

	// Move data file
	if err := os.Rename(srcDataFile, dstDataFile); err != nil {
		return fmt.Errorf("failed to move data file to retry: %w", err)
	}

	// Create new metadata for retry (queue files don't have metadata)
	fileInfo, err := os.Stat(dstDataFile)
	if err != nil {
		log.Warnf("Failed to get file info for %s: %v", dstDataFile, err)
	}

	// Read file to count lines
	var lineCount int
	if fileData, err := s.safeReadFile(dstDataFile); err != nil {
		log.Warnf("Failed to read file for line counting %s: %v", dstDataFile, err)
		lineCount = 0
	} else {
		lineCount = s.countLines(fileData)
	}

	// Get compressed size
	var compressedSize int64
	if fileInfo != nil {
		compressedSize = fileInfo.Size()
	}

	// Get uncompressed size
	uncompressedSize, err := s.getUncompressedSize(dstDataFile)
	if err != nil {
		log.Warnf("Failed to get uncompressed size for %s: %v", dstDataFile, err)
		uncompressedSize = compressedSize // Fallback to compressed size
	}

	now := time.Now()
	metadata := SpooledFile{
		ID:               batchID,
		TenantID:         tenantID,
		DatasetID:        datasetID,
		BearerToken:      "", // Will be filled from batch context if available
		Filename:         dstDataFile,
		CompressedSize:   compressedSize,      // Compressed size on disk
		UncompressedSize: uncompressedSize,    // Original data size before compression
		LineCount:        lineCount,           // Count lines from actual file data
		CreatedAt:        now,
		LastRetry:        now,
		RetryCount:       1, // First retry attempt
		Status:           "retry",
		FailureReason:    failureReason,
		TriggerReason:    triggerReason,
	}

	// Write metadata to retry directory
	metaData, err := sonic.Marshal(metadata)
	if err == nil {
		if err := os.WriteFile(dstMetaFile, metaData, 0600); err != nil {
			log.Warnf("Failed to write retry metadata for %s: %v", batchID, err)
		}
	}

	actualFilename := filepath.Base(srcDataFile)
	log.Infof("Moved file %s to retry due to: %s", actualFilename, failureReason)
	return nil
}

// MoveQueueToDLQ moves a batch file from queue directly to DLQ for permanent failures
func (s *SpoolingService) MoveQueueToDLQ(tenantID, datasetID, batchID, failureReason string) error {
	if !s.config.Spooling.Enabled {
		return nil
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Create DLQ directory
	dlqDir := filepath.Join(s.directory, tenantID, datasetID, "dlq")
	if err := os.MkdirAll(dlqDir, 0750); err != nil {
		return fmt.Errorf("failed to create DLQ directory: %w", err)
	}

	// Source paths in queue (no metadata in queue)
	queueDir := filepath.Join(s.directory, tenantID, datasetID, "queue")

	// Find the batch file
	var srcDataFile string
	files, err := os.ReadDir(queueDir)
	if err != nil {
		return fmt.Errorf("failed to read queue directory: %w", err)
	}

	for _, file := range files {
		fileName := file.Name()
		// New format: batchID.gz (batchID already includes extension)
		if fileName == batchID+".gz" {
			srcDataFile = filepath.Join(queueDir, fileName)
			break
		}
	}

	if srcDataFile == "" {
		return fmt.Errorf("batch file %s not found in queue", batchID)
	}

	// Destination paths in DLQ
	dstDataFile := filepath.Join(dlqDir, filepath.Base(srcDataFile))
	dstMetaFile := filepath.Join(dlqDir, batchID+".meta")

	// Move data file
	if err := os.Rename(srcDataFile, dstDataFile); err != nil {
		return fmt.Errorf("failed to move data file to DLQ: %w", err)
	}

	// Create metadata for DLQ (queue files don't have metadata)
	fileInfo, err := os.Stat(dstDataFile)
	if err != nil {
		log.Warnf("Failed to get file info for %s: %v", dstDataFile, err)
	}

	// Read file to count lines
	var lineCount int
	if fileData, err := s.safeReadFile(dstDataFile); err != nil {
		log.Warnf("Failed to read file for line counting %s: %v", dstDataFile, err)
		lineCount = 0
	} else {
		lineCount = s.countLines(fileData)
	}

	// Get compressed size
	var compressedSize int64
	if fileInfo != nil {
		compressedSize = fileInfo.Size()
	}

	// Get uncompressed size
	uncompressedSize, err := s.getUncompressedSize(dstDataFile)
	if err != nil {
		log.Warnf("Failed to get uncompressed size for %s: %v", dstDataFile, err)
		uncompressedSize = compressedSize // Fallback to compressed size
	}

	now := time.Now()
	metadata := SpooledFile{
		ID:               batchID,
		TenantID:         tenantID,
		DatasetID:        datasetID,
		BearerToken:      "", // Will be filled from batch context if available
		Filename:         dstDataFile,
		CompressedSize:   compressedSize,      // Compressed size on disk
		UncompressedSize: uncompressedSize,    // Original data size before compression
		LineCount:        lineCount,           // Count lines from actual file data
		CreatedAt:        now,
		LastRetry:        now,
		RetryCount:       0, // No retries for permanent failures
		Status:           "dlq",
		FailureReason:    failureReason,
		TriggerReason:    "permanent_failure",
	}

	// Write metadata to DLQ directory
	metaData, err := sonic.Marshal(metadata)
	if err == nil {
		if err := os.WriteFile(dstMetaFile, metaData, 0600); err != nil {
			log.Warnf("Failed to write DLQ metadata for %s: %v", batchID, err)
		}
	}

	actualFilename := filepath.Base(srcDataFile)
	log.Infof("Moved file %s to DLQ due to permanent failure: %s", actualFilename, failureReason)
	return nil
}

// RemoveFromQueue removes a successfully uploaded batch from the queue directory
func (s *SpoolingService) RemoveFromQueue(tenantID, datasetID, batchID string) error {
	if !s.config.Spooling.Enabled {
		return nil
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Remove data file from queue (with any extension: .raw.gz, .csv.gz, .ndjson.gz, etc.)
	queueDir := filepath.Join(s.directory, tenantID, datasetID, "queue")

	var dataFileRemoved bool
	files, err := os.ReadDir(queueDir)
	if err != nil {
		log.Warnf("Failed to read queue directory for batch %s: %v", batchID, err)
	} else {
		for _, file := range files {
			fileName := file.Name()
			// New format: batchID.gz (batchID already includes extension)
			if fileName == batchID+".gz" {
				dataFile := filepath.Join(queueDir, fileName)
				if err := os.Remove(dataFile); err == nil {
					dataFileRemoved = true
					break
				}
			}
		}
	}

	if !dataFileRemoved {
		log.Warnf("No data file found for batch %s in queue", batchID)
	}

	// No metadata to remove - queue files don't have metadata

	if dataFileRemoved {
		log.Debugf("Removed file for batch %s from queue after successful upload", batchID)
	}
	return nil
}

// processOrphanedQueueFiles moves any existing queue files to retry/ on startup
func (s *SpoolingService) processOrphanedQueueFiles() error {
	if !s.config.Spooling.Enabled {
		return nil
	}

	tenants, err := s.getTenants()
	if err != nil {
		return fmt.Errorf("failed to get tenants: %w", err)
	}

	totalOrphanedFiles := 0

	for _, tenantID := range tenants {
		datasets, err := s.getTenantDatasets(tenantID)
		if err != nil {
			log.Warnf("Failed to get datasets for tenant %s: %v", tenantID, err)
			continue
		}

		for _, datasetID := range datasets {
			queueDir := filepath.Join(s.directory, tenantID, datasetID, "queue")

			// Check if queue directory exists
			if _, err := os.Stat(queueDir); os.IsNotExist(err) {
				continue
			}

			// Read queue directory
			entries, err := os.ReadDir(queueDir)
			if err != nil {
				log.Warnf("Failed to read queue directory %s: %v", queueDir, err)
				continue
			}

			orphanedCount := 0
			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}

				// Process all data files (skip metadata files)
				if !strings.HasSuffix(entry.Name(), ".meta") {
					// Extract batch ID from filename (everything before the first dot)
					filename := entry.Name()
					var batchID string
					if dotIndex := strings.Index(filename, "."); dotIndex > 0 {
						batchID = filename[:dotIndex]
					} else {
						batchID = filename
					}

					// Move to retry with retry count 0 (fresh start)
					if err := s.moveOrphanedQueueFileToRetry(tenantID, datasetID, batchID, queueDir); err != nil {
						log.Errorf("Failed to move orphaned queue file %s to retry: %v", filename, err)
						continue
					}

					orphanedCount++
				}
			}

			if orphanedCount > 0 {
				log.Infof("Moved %d orphaned queue files to retry for tenant=%s dataset=%s", orphanedCount, tenantID, datasetID)
				totalOrphanedFiles += orphanedCount
			}
		}
	}

	if totalOrphanedFiles > 0 {
		log.Infof("🔄 Startup recovery: Moved %d orphaned queue files to retry (fresh retry count)", totalOrphanedFiles)
	}

	return nil
}

// moveOrphanedQueueFileToRetry moves a single orphaned queue file to retry/ with fresh retry count
func (s *SpoolingService) moveOrphanedQueueFileToRetry(tenantID, datasetID, batchID, queueDir string) error {
	// Create retry directory
	retryDir := filepath.Join(s.directory, tenantID, datasetID, "retry")
	if err := os.MkdirAll(retryDir, 0750); err != nil {
		return fmt.Errorf("failed to create retry directory: %w", err)
	}

	// Find the data file (with any extension: .raw.gz, .csv.gz, .ndjson.gz, etc.)
	var srcDataFile string
	files, err := os.ReadDir(queueDir)
	if err != nil {
		return fmt.Errorf("failed to read queue directory: %w", err)
	}

	for _, file := range files {
		if strings.HasPrefix(file.Name(), batchID+".") {
			srcDataFile = filepath.Join(queueDir, file.Name())
			break
		}
	}

	if srcDataFile == "" {
		return fmt.Errorf("data file not found for batch %s", batchID)
	}

	// Destination paths
	dstDataFile := filepath.Join(retryDir, filepath.Base(srcDataFile))
	dstMetaFile := filepath.Join(retryDir, batchID+".meta")

	// Move data file
	if err := os.Rename(srcDataFile, dstDataFile); err != nil {
		return fmt.Errorf("failed to move data file: %w", err)
	}

	// Get file info for metadata
	fileInfo, err := os.Stat(dstDataFile)
	if err != nil {
		log.Warnf("Failed to get file info for %s: %v", dstDataFile, err)
	}

	// Read file to count lines
	var lineCount int
	if fileData, err := s.safeReadFile(dstDataFile); err != nil {
		log.Warnf("Failed to read file for line counting %s: %v", dstDataFile, err)
		lineCount = 0
	} else {
		lineCount = s.countLines(fileData)
	}

	// Get compressed size
	var compressedSize int64
	if fileInfo != nil {
		compressedSize = fileInfo.Size()
	}

	// Get uncompressed size
	uncompressedSize, err := s.getUncompressedSize(dstDataFile)
	if err != nil {
		log.Warnf("Failed to get uncompressed size for %s: %v", dstDataFile, err)
		uncompressedSize = compressedSize // Fallback to compressed size
	}

	// Create metadata with retry count 0 (fresh start)
	now := time.Now()
	metadata := SpooledFile{
		ID:               batchID,
		TenantID:         tenantID,
		DatasetID:        datasetID,
		BearerToken:      "", // Will be filled from config during retry
		Filename:         dstDataFile,
		CompressedSize:   compressedSize,      // Compressed size on disk
		UncompressedSize: uncompressedSize,    // Original data size before compression
		LineCount:        lineCount,           // Count lines from actual file data
		CreatedAt:        now,                 // Use current time as creation time
		LastRetry:        time.Time{},         // No retry attempted yet
		RetryCount:       0,                   // Fresh start - gets full 4 retry attempts
		Status:           "retry",
		FailureReason:    "Recovered from orphaned queue file on startup",
		TriggerReason:    "service_restart", // Special case for recovered files
	}

	// Write metadata to retry directory
	metaData, err := sonic.Marshal(metadata)
	if err != nil {
		// Clean up moved data file on metadata error
		os.Remove(dstDataFile)
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	if err := os.WriteFile(dstMetaFile, metaData, 0600); err != nil {
		// Clean up moved data file on metadata error
		os.Remove(dstDataFile)
		return fmt.Errorf("failed to write metadata: %w", err)
	}

	return nil
}

// moveAgedFilesToDLQ moves old undeliverable files to DLQ before cleanup
func (s *SpoolingService) moveAgedFilesToDLQ() {
	if !s.config.Spooling.Enabled {
		return
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()

	files, err := s.getSpooledFiles()
	if err != nil {
		log.Errorf("Failed to get spooled files for DLQ aging: %v", err)
		return
	}

	movedCount := 0
	now := time.Now()

	for _, file := range files {
		// Skip files already in DLQ
		if file.Status == "dlq" {
			continue
		}

		shouldMoveToDLQ := false
		reason := ""

		// Move files that are very old (beyond reasonable retry period)
		maxAge := time.Duration(s.retryAttempts) * s.retryInterval * 3 // 3x retry period
		if time.Since(file.CreatedAt) > maxAge {
			shouldMoveToDLQ = true
			reason = fmt.Sprintf("File aged out after %v (created %v ago)", maxAge, time.Since(file.CreatedAt))
		}

		// Move files that exceeded max configured age
		if s.config.Spooling.MaxAgeDays > 0 {
			configMaxAge := time.Duration(s.config.Spooling.MaxAgeDays) * 24 * time.Hour
			if time.Since(file.CreatedAt) > configMaxAge {
				shouldMoveToDLQ = true
				reason = fmt.Sprintf("File exceeded max age of %d days", s.config.Spooling.MaxAgeDays)
			}
		}

		if shouldMoveToDLQ {
			log.Warnf("Moving aged file %s to DLQ: %s", file.ID, reason)

			// Update file status and move to DLQ
			file.Status = "dlq"
			file.FailureReason = reason
			file.LastRetry = now

			if err := s.moveToNewDLQ(file); err != nil {
				log.Errorf("Failed to move aged file %s to DLQ: %v", file.ID, err)
			} else {
				movedCount++

				// Send SOC alert for aged-out data
				if s.config.SOCAlertClient != nil {
					s.config.SOCAlertClient.SendAlert("medium", "Data Aged to DLQ",
						fmt.Sprintf("File %s moved to DLQ due to age", file.ID),
						fmt.Sprintf("Tenant: %s, Dataset: %s, Reason: %s", file.TenantID, file.DatasetID, reason))
				}
			}
		}
	}

	if movedCount > 0 {
		log.Infof("Moved %d aged files to DLQ for preservation", movedCount)
	}
}

// Note: processQueueRetries removed - queue files don't have metadata and are processed immediately



// monitorDLQAndSpace monitors DLQ growth and disk space usage
func (s *SpoolingService) monitorDLQAndSpace() {
	dlqStats, err := s.GetDLQStats()
	if err != nil {
		log.Errorf("Failed to get DLQ stats for monitoring: %v", err)
		return
	}

	// Alert thresholds
	const (
		dlqSizeThresholdMB  = 500  // Alert when DLQ exceeds 500MB
		dlqCountThreshold   = 1000 // Alert when DLQ exceeds 1000 files
		diskUsageThreshold  = 0.85 // Alert when disk usage exceeds 85%
	)

	// Monitor DLQ size growth
	dlqSizeMB := float64(dlqStats.TotalBytesInDLQ) / (1024 * 1024)
	if dlqSizeMB > dlqSizeThresholdMB {
		s.sendDLQAlert("high", "DLQ Size Alert",
			fmt.Sprintf("DLQ size has grown to %.1f MB (%d files)", dlqSizeMB, dlqStats.TotalFilesInDLQ),
			fmt.Sprintf("DLQ contains %d files totaling %.1f MB. Manual intervention may be required.",
				dlqStats.TotalFilesInDLQ, dlqSizeMB))
	}

	// Monitor DLQ file count
	if dlqStats.TotalFilesInDLQ > dlqCountThreshold {
		s.sendDLQAlert("high", "DLQ File Count Alert",
			fmt.Sprintf("DLQ contains %d files (threshold: %d)", dlqStats.TotalFilesInDLQ, dlqCountThreshold),
			"High number of failed deliveries detected. Check receiver connectivity and investigate failed batches.")
	}

	// Monitor overall disk space
	s.monitorDiskSpace()

	// Log DLQ stats periodically (every hour = 12 cleanup cycles)
	if dlqStats.TotalFilesInDLQ > 0 {
		log.Infof("DLQ Status: %d files (%.1f MB) in queue, %d files (%.1f MB) in DLQ",
			dlqStats.TotalFilesInQueue, float64(dlqStats.TotalBytesInQueue)/(1024*1024),
			dlqStats.TotalFilesInDLQ, dlqSizeMB)
	}
}

// monitorDiskSpace monitors overall disk space and sends alerts when low
func (s *SpoolingService) monitorDiskSpace() {
	// Get disk usage for the spool directory
	usage, err := s.getDiskUsage(s.directory)
	if err != nil {
		log.Warnf("Failed to get disk usage for %s: %v", s.directory, err)
		return
	}

	// Alert thresholds
	const (
		criticalThreshold = 0.95 // 95% full
		warningThreshold  = 0.85 // 85% full
	)

	usagePercent := float64(usage.Used) / float64(usage.Total)

	if usagePercent > criticalThreshold {
		s.sendDLQAlert("critical", "Critical Disk Space Alert",
			fmt.Sprintf("Disk space critically low: %.1f%% used", usagePercent*100),
			fmt.Sprintf("Disk: %.1f GB used of %.1f GB total. IMMEDIATE ACTION REQUIRED: Archive or move DLQ files to prevent data loss.",
				float64(usage.Used)/(1024*1024*1024), float64(usage.Total)/(1024*1024*1024)))
	} else if usagePercent > warningThreshold {
		s.sendDLQAlert("high", "High Disk Space Alert",
			fmt.Sprintf("Disk space warning: %.1f%% used", usagePercent*100),
			fmt.Sprintf("Disk: %.1f GB used of %.1f GB total. Consider archiving old DLQ files.",
				float64(usage.Used)/(1024*1024*1024), float64(usage.Total)/(1024*1024*1024)))
	}
}

// sendDLQAlert sends SOC alerts for DLQ and disk space issues
func (s *SpoolingService) sendDLQAlert(severity, title, message, details string) {
	if s.config.SOCAlertClient != nil {
		s.config.SOCAlertClient.SendAlert(severity, title, message, details)
	}

	// Also log the alert
	switch severity {
	case "critical":
		log.Errorf("DLQ ALERT [%s]: %s - %s", severity, title, message)
	case "high":
		log.Warnf("DLQ ALERT [%s]: %s - %s", severity, title, message)
	default:
		log.Infof("DLQ ALERT [%s]: %s - %s", severity, title, message)
	}
}

// DiskUsage represents disk space information
type DiskUsage struct {
	Total uint64
	Used  uint64
	Free  uint64
}

// getDiskUsage returns disk usage statistics for the given path
func (s *SpoolingService) getDiskUsage(path string) (*DiskUsage, error) {
	// This is a simplified implementation - in production you might want to use syscall.Statfs
	// For now, we'll use a basic approach that works cross-platform

	// Calculate current spool directory size
	currentSize, err := s.getDirectorySize(path)
	if err != nil {
		return nil, fmt.Errorf("failed to get directory size: %w", err)
	}

	// Use max size as a proxy for "total space allocated"
	// In production, you'd want actual filesystem stats

	// Handle potential negative values and overflow
	var totalSize, usedSize, freeSize uint64

	if s.maxSize >= 0 {
		totalSize = uint64(s.maxSize)
	}

	if currentSize >= 0 {
		usedSize = uint64(currentSize)
	}

	// Calculate free space safely
	if totalSize >= usedSize {
		freeSize = totalSize - usedSize
	} else {
		freeSize = 0 // No free space if used exceeds total
	}

	return &DiskUsage{
		Total: totalSize,
		Used:  usedSize,
		Free:  freeSize,
	}, nil
}

// getFileExtensionForDataHint returns appropriate file extension based on data hint
func getFileExtensionForDataHint(dataHint string) string {
	switch dataHint {
	case "ndjson", "json":
		return "ndjson"
	case "syslog":
		return "log"
	case "csv":
		return "csv"
	case "tsv":
		return "tsv"
	case "apache", "nginx", "iis", "squid":
		return "log"
	case "raw":
		return "raw"
	default:
		// For unknown formats, use the data hint as extension if valid, otherwise default to raw
		if dataHint != "" && len(dataHint) <= 10 {
			return dataHint
		}
		return "raw"
	}
}

// extractLineCountFromFilename extracts the line count from filenames with pattern: {timestamp}_{bytecount}_{linecount}.{extension}
func extractLineCountFromFilename(filename string) int {
	// Remove extension by finding the last dot
	lastDot := strings.LastIndex(filename, ".")
	var nameWithoutExt string
	if lastDot > 0 {
		nameWithoutExt = filename[:lastDot]
	} else {
		nameWithoutExt = filename
	}

	// Split by underscore: [timestamp, bytecount, linecount]
	parts := strings.Split(nameWithoutExt, "_")
	if len(parts) != 3 {
		// Old format without line count, return 0
		return 0
	}

	// Parse the line count (third part)
	lineCount, err := strconv.Atoi(parts[2])
	if err != nil {
		log.Debugf("Failed to parse line count from filename %s: %v", filename, err)
		return 0
	}

	return lineCount
}

// extractDataHintFromFilename extracts the data hint (file extension) from filenames
func extractDataHintFromFilename(filename string) string {
	// Find the last dot to get extension
	lastDot := strings.LastIndex(filename, ".")
	if lastDot < 0 || lastDot == len(filename)-1 {
		return "txt" // default if no extension
	}

	extension := filename[lastDot+1:]

	// Handle some common mappings back to data hints
	switch extension {
	case "ndjson":
		return "ndjson"
	case "log":
		return "syslog" // Most log files are syslog format
	case "csv":
		return "csv"
	case "tsv":
		return "tsv"
	case "txt":
		return "raw"
	default:
		// For other extensions, use the extension as the data hint
		return extension
	}
}

// isLineBasedFormat returns true if the data hint represents a line-based format that should have newlines between records
func isLineBasedFormat(dataHint string) bool {
	switch dataHint {
	case "ndjson", "csv", "tsv", "syslog", "apache", "nginx", "iis", "squid":
		return true
	default:
		return false
	}
}



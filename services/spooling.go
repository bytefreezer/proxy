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
	s.wg.Add(2)
	go s.retryWorker()
	go s.cleanupWorker()

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

// SpoolData stores data locally when upload fails
func (s *SpoolingService) SpoolData(tenantID, datasetID string, data []byte, failureReason string) error {
	if !s.config.Spooling.Enabled {
		return nil
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()

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

// processRetries attempts to retry failed uploads
func (s *SpoolingService) processRetries() {
	files, err := s.getSpooledFiles()
	if err != nil {
		log.Errorf("Failed to get spooled files for retry: %v", err)
		return
	}

	if len(files) == 0 {
		return
	}

	log.Debugf("Processing %d spooled files for retry", len(files))

	forwarder := NewHTTPForwarder(s.config)
	successCount := 0
	failureCount := 0

	for _, file := range files {
		// Skip files that are permanently failed
		if file.Status == "failed" {
			continue
		}

		// Check if it's time to retry
		if time.Since(file.LastRetry) < s.retryInterval {
			continue
		}

		// Check retry limit
		if file.RetryCount >= s.retryAttempts {
			// Send SOC alert for max retries reached
			if s.config.SOCAlertClient != nil {
				s.config.SOCAlertClient.SendAlert(
					"high",
					"Spooled File Max Retries Reached",
					"A spooled file has exceeded the maximum retry attempts - file preserved for manual recovery",
					fmt.Sprintf("File: %s, Tenant: %s, Dataset: %s, Attempts: %d, Path: %s",
						file.ID, file.TenantID, file.DatasetID, file.RetryCount, filepath.Join(s.directory, file.Filename)),
				)
			}

			// Keep file but don't retry anymore - mark as permanently failed
			s.markAsPermanentlyFailed(file)
			failureCount++
			continue
		}

		// Load file data using findFilePaths to handle hierarchical organization
		dataPath, _, err := s.findFilePaths(file)
		if err != nil {
			log.Errorf("Failed to find file paths for %s: %v", file.Filename, err)
			continue
		}
		// #nosec G304 - path is validated by findFilePaths function above
		data, err := os.ReadFile(dataPath)
		if err != nil {
			if os.IsNotExist(err) {
				// Data file is missing, remove orphaned metadata to prevent repeated errors
				log.Warnf("Data file %s missing, removing orphaned metadata %s", file.Filename, file.ID)
				if removeErr := s.removeSpooledFile(file); removeErr != nil {
					log.Errorf("Failed to remove orphaned metadata for %s: %v", file.ID, removeErr)
				}
			} else {
				log.Errorf("Failed to read spooled file %s: %v", file.Filename, err)
			}
			continue
		}

		// Create batch for retry
		batch := &domain.DataBatch{
			ID:        file.ID,
			TenantID:  file.TenantID,
			DatasetID: file.DatasetID,
			Data:      data,
			CreatedAt: file.CreatedAt,
		}

		// Attempt upload
		if err := forwarder.ForwardBatch(batch); err != nil {
			// Update retry count and last retry time
			s.updateRetryMetadata(file, err.Error())
			failureCount++
			log.Debugf("Retry failed for %s: %v", file.ID, err)
		} else {
			// Success - remove files (only case where files are deleted)
			s.removeSpooledFile(file)
			successCount++
			log.Debugf("Retry succeeded for %s", file.ID)
		}
	}

	if successCount > 0 || failureCount > 0 {
		log.Infof("Spooling retry results: %d succeeded, %d failed", successCount, failureCount)
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

// cleanupOldFiles removes old spooled files to free space
func (s *SpoolingService) cleanupOldFiles() error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

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

// updateRetryMetadata updates the retry metadata for a file
func (s *SpoolingService) updateRetryMetadata(file SpooledFile, failureReason string) {
	file.RetryCount++
	file.LastRetry = time.Now()
	file.FailureReason = failureReason
	file.Status = "retrying"

	metaPath := filepath.Join(s.directory, fmt.Sprintf("%s.meta", file.ID))
	metaData, err := json.Marshal(file)
	if err != nil {
		log.Warnf("Failed to marshal updated metadata for %s: %v", file.ID, err)
		return
	}

	if err := os.WriteFile(metaPath, metaData, 0600); err != nil {
		log.Warnf("Failed to write updated metadata for %s: %v", file.ID, err)
	}
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

// markAsPermanentlyFailed marks a file as permanently failed but preserves it
func (s *SpoolingService) markAsPermanentlyFailed(file SpooledFile) {
	file.Status = "failed"
	file.LastRetry = time.Now()
	file.FailureReason = "Exceeded maximum retry attempts - manual recovery required"

	metaPath := filepath.Join(s.directory, fmt.Sprintf("%s.meta", file.ID))
	metaData, err := json.Marshal(file)
	if err != nil {
		log.Warnf("Failed to marshal permanently failed metadata for %s: %v", file.ID, err)
		return
	}

	if err := os.WriteFile(metaPath, metaData, 0600); err != nil {
		log.Warnf("Failed to write permanently failed metadata for %s: %v", file.ID, err)
	} else {
		log.Infof("Marked file as permanently failed: %s (preserved for manual recovery)", file.ID)
	}
}

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

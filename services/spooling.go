package services

import (
	"encoding/json"
	"fmt"
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
	CreatedAt     time.Time `json:"created_at"`
	LastRetry     time.Time `json:"last_retry"`
	RetryCount    int       `json:"retry_count"`
	Status        string    `json:"status"` // "pending", "retrying", "failed", "success"
	FailureReason string    `json:"failure_reason,omitempty"`
}

// NewSpoolingService creates a new spooling service
func NewSpoolingService(cfg *config.Config) *SpoolingService {
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
	if err := os.MkdirAll(s.directory, 0755); err != nil {
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

	// Check size limit
	dataSize := int64(len(data))
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

	// Generate unique ID and filename
	id := fmt.Sprintf("%d_%s_%s", time.Now().UnixNano(), tenantID, datasetID)
	filename := fmt.Sprintf("%s.ndjson", id)
	filePath := filepath.Join(s.directory, filename)
	metaFilepath := filepath.Join(s.directory, fmt.Sprintf("%s.meta", id))

	// Write data file
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write spooled data file: %w", err)
	}

	// Write metadata file
	metadata := SpooledFile{
		ID:            id,
		TenantID:      tenantID,
		DatasetID:     datasetID,
		Filename:      filename,
		Size:          dataSize,
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

	if err := os.WriteFile(metaFilepath, metaData, 0644); err != nil {
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

		// Load file data
		dataPath := filepath.Join(s.directory, file.Filename)
		data, err := os.ReadFile(dataPath)
		if err != nil {
			log.Errorf("Failed to read spooled file %s: %v", file.Filename, err)
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

	// Sort by age (oldest first)
	sort.Slice(files, func(i, j int) bool {
		return files[i].CreatedAt.Before(files[j].CreatedAt)
	})

	cleaned := 0
	for _, file := range files {
		// Remove files that exceeded retry attempts or are too old
		maxAge := time.Duration(s.retryAttempts) * s.retryInterval * 2
		if file.RetryCount >= s.retryAttempts || time.Since(file.CreatedAt) > maxAge {
			if err := s.removeSpooledFile(file); err != nil {
				log.Warnf("Failed to remove old spooled file %s: %v", file.ID, err)
			} else {
				cleaned++
			}
		}
	}

	if cleaned > 0 {
		log.Infof("Cleaned up %d old spooled files", cleaned)
	}

	return nil
}

// getSpooledFiles returns all spooled files with metadata
func (s *SpoolingService) getSpooledFiles() ([]SpooledFile, error) {
	entries, err := os.ReadDir(s.directory)
	if err != nil {
		return nil, fmt.Errorf("failed to read spooling directory: %w", err)
	}

	var files []SpooledFile
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".meta") {
			continue
		}

		metaPath := filepath.Join(s.directory, entry.Name())
		metaData, err := os.ReadFile(metaPath)
		if err != nil {
			log.Warnf("Failed to read metadata file %s: %v", entry.Name(), err)
			continue
		}

		var file SpooledFile
		if err := json.Unmarshal(metaData, &file); err != nil {
			log.Warnf("Failed to unmarshal metadata file %s: %v", entry.Name(), err)
			continue
		}

		files = append(files, file)
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

	if err := os.WriteFile(metaPath, metaData, 0644); err != nil {
		log.Warnf("Failed to write updated metadata for %s: %v", file.ID, err)
	}
}

// removeSpooledFile removes both data and metadata files
func (s *SpoolingService) removeSpooledFile(file SpooledFile) error {
	dataPath := filepath.Join(s.directory, file.Filename)
	metaPath := filepath.Join(s.directory, fmt.Sprintf("%s.meta", file.ID))

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

	if err := os.WriteFile(metaPath, metaData, 0644); err != nil {
		log.Warnf("Failed to write permanently failed metadata for %s: %v", file.ID, err)
	} else {
		log.Infof("Marked file as permanently failed: %s (preserved for manual recovery)", file.ID)
	}
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

// GetStats returns spooling statistics
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

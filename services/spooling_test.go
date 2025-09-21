package services

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/n0needt0/bytefreezer-proxy/config"
)

func TestSpoolingService_StoreRawMessage(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "spooling_test_")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	cfg := &config.Config{
		Spooling: config.Spooling{
			Enabled:   true,
			Directory: tempDir,
		},
	}

	service := NewSpoolingService(cfg)

	testData := []byte(`{"message": "test message", "timestamp": "2024-01-15T10:30:45Z"}`)
	err = service.StoreRawMessage("test-tenant", "test-dataset", "test-token", testData)
	if err != nil {
		t.Fatalf("Failed to store raw message: %v", err)
	}

	// Verify directory structure was created
	rawDir := filepath.Join(tempDir, "test-tenant", "test-dataset", "raw")
	if _, err := os.Stat(rawDir); os.IsNotExist(err) {
		t.Errorf("Raw directory was not created: %s", rawDir)
	}

	// Verify file was created
	files, err := os.ReadDir(rawDir)
	if err != nil {
		t.Fatalf("Failed to read raw directory: %v", err)
	}

	if len(files) != 1 {
		t.Errorf("Expected 1 file, got %d", len(files))
	}

	// Verify file content
	if len(files) > 0 {
		filePath := filepath.Join(rawDir, files[0].Name())
		content, err := os.ReadFile(filePath)
		if err != nil {
			t.Fatalf("Failed to read stored file: %v", err)
		}

		if string(content) != string(testData) {
			t.Errorf("File content mismatch. Expected: %s, Got: %s", testData, content)
		}
	}
}

func TestSpoolingService_BatchRawFiles(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "spooling_test_")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	cfg := &config.Config{
		Spooling: config.Spooling{
			Enabled:   true,
			Directory: tempDir,
		},
	}

	service := NewSpoolingService(cfg)

	// Create test raw files
	rawDir := filepath.Join(tempDir, "test-tenant", "test-dataset", "raw")
	if err := os.MkdirAll(rawDir, 0750); err != nil {
		t.Fatalf("Failed to create raw directory: %v", err)
	}

	testMessages := []string{
		`{"message": "test message 1", "timestamp": "2024-01-15T10:30:45Z"}`,
		`{"message": "test message 2", "timestamp": "2024-01-15T10:30:46Z"}`,
		`{"message": "test message 3", "timestamp": "2024-01-15T10:30:47Z"}`,
	}

	for i, msg := range testMessages {
		filename := filepath.Join(rawDir, "msg_"+string(rune('0'+i+1))+".ndjson")
		if err := os.WriteFile(filename, []byte(msg), 0600); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}
	}

	// Run batch processing
	err = service.BatchRawFiles("test-tenant", "test-dataset", "test-token")
	if err != nil {
		t.Fatalf("Failed to batch raw files: %v", err)
	}

	// Verify queue directory was created with compressed file
	queueDir := filepath.Join(tempDir, "test-tenant", "test-dataset", "queue")
	queueFiles, err := os.ReadDir(queueDir)
	if err != nil {
		t.Fatalf("Failed to read queue directory: %v", err)
	}

	if len(queueFiles) != 1 {
		t.Errorf("Expected 1 queue file, got %d", len(queueFiles))
	}

	// Verify compressed file extension
	if len(queueFiles) > 0 && !strings.HasSuffix(queueFiles[0].Name(), ".gz") {
		t.Errorf("Expected .gz file, got: %s", queueFiles[0].Name())
	}

	// Verify no metadata directory created for queue files (metadata only created when files move to retry/dlq)
	metaDir := filepath.Join(tempDir, "test-tenant", "test-dataset", "meta")
	_, err = os.ReadDir(metaDir)
	if !os.IsNotExist(err) {
		t.Errorf("Meta directory should not exist for queue files, but directory was found")
	}

	// Verify raw files were deleted
	rawFilesAfter, err := os.ReadDir(rawDir)
	if err != nil {
		t.Fatalf("Failed to read raw directory after batching: %v", err)
	}

	if len(rawFilesAfter) != 0 {
		t.Errorf("Expected 0 raw files after batching, got %d", len(rawFilesAfter))
	}
}

func TestSpoolingService_GetDLQStats(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "spooling_test_")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	cfg := &config.Config{
		Spooling: config.Spooling{
			Enabled:   true,
			Directory: tempDir,
		},
	}

	service := NewSpoolingService(cfg)

	// Create test directory structure with files (only .ndjson.gz files are counted)
	testStructure := map[string]string{
		"tenant1/dataset1/queue/batch1.ndjson.gz": "test compressed data 1",
		"tenant1/dataset1/queue/batch2.ndjson.gz": "test compressed data 2",
		"tenant1/dataset2/queue/batch3.ndjson.gz": "test compressed data 3",
		"tenant1/dataset1/dlq/failed1.ndjson.gz":  "failed compressed data 1",
		"tenant1/dataset2/dlq/failed2.ndjson.gz":  "failed compressed data 2",
		"tenant2/dataset3/queue/batch4.ndjson.gz": "test compressed data 4",
		"tenant2/dataset3/dlq/failed3.ndjson.gz":  "failed compressed data 3",
	}

	for path, content := range testStructure {
		fullPath := filepath.Join(tempDir, path)
		dir := filepath.Dir(fullPath)
		if err := os.MkdirAll(dir, 0750); err != nil {
			t.Fatalf("Failed to create directory %s: %v", dir, err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0600); err != nil {
			t.Fatalf("Failed to create file %s: %v", fullPath, err)
		}
	}

	// Get DLQ stats
	stats, err := service.GetDLQStats()
	if err != nil {
		t.Fatalf("Failed to get DLQ stats: %v", err)
	}

	// Verify total counts
	if stats.TotalFilesInQueue != 4 {
		t.Errorf("Expected 4 files in queue, got %d", stats.TotalFilesInQueue)
	}

	if stats.TotalFilesInDLQ != 3 {
		t.Errorf("Expected 3 files in DLQ, got %d", stats.TotalFilesInDLQ)
	}

	// Verify tenant stats
	if len(stats.TenantStats) != 2 {
		t.Errorf("Expected 2 tenants, got %d", len(stats.TenantStats))
	}

	tenant1Stats := stats.TenantStats["tenant1"]
	if tenant1Stats == nil {
		t.Fatal("Expected tenant1 stats to exist")
	}

	if tenant1Stats.QueueFiles != 3 {
		t.Errorf("Expected 3 queue files for tenant1, got %d", tenant1Stats.QueueFiles)
	}

	if tenant1Stats.DLQFiles != 2 {
		t.Errorf("Expected 2 DLQ files for tenant1, got %d", tenant1Stats.DLQFiles)
	}

	// Verify dataset stats
	if len(tenant1Stats.DatasetStats) != 2 {
		t.Errorf("Expected 2 datasets for tenant1, got %d", len(tenant1Stats.DatasetStats))
	}
}

func TestSpoolingService_RetryDLQFiles(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "spooling_test_")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	cfg := &config.Config{
		Spooling: config.Spooling{
			Enabled:   true,
			Directory: tempDir,
		},
	}

	service := NewSpoolingService(cfg)

	// Create DLQ structure with files and metadata
	dlqDir := filepath.Join(tempDir, "test-tenant", "test-dataset", "dlq")
	if err := os.MkdirAll(dlqDir, 0750); err != nil {
		t.Fatalf("Failed to create DLQ directory: %v", err)
	}

	// Create test data file
	testDataFile := filepath.Join(dlqDir, "failed_batch.ndjson.gz")
	testData := "test compressed data"
	if err := os.WriteFile(testDataFile, []byte(testData), 0600); err != nil {
		t.Fatalf("Failed to create test data file: %v", err)
	}

	// Create test metadata file
	testMetadata := SpooledFile{
		ID:            "failed_batch",
		TenantID:      "test-tenant",
		DatasetID:     "test-dataset",
		BearerToken:   "test-token",
		Filename:      testDataFile,
		CompressedSize:   int64(len(testData)),
		UncompressedSize: int64(len(testData)),
		LineCount:     1,
		CreatedAt:     time.Now(),
		LastRetry:     time.Now(),
		RetryCount:    4,
		Status:        "dlq",
		FailureReason: "Exceeded retry limit",
	}

	metaData, err := json.Marshal(testMetadata)
	if err != nil {
		t.Fatalf("Failed to marshal metadata: %v", err)
	}

	testMetaFile := filepath.Join(dlqDir, "failed_batch.meta")
	if err := os.WriteFile(testMetaFile, metaData, 0600); err != nil {
		t.Fatalf("Failed to create test metadata file: %v", err)
	}

	// Run retry operation
	result, err := service.RetryDLQFiles("test-tenant", "test-dataset")
	if err != nil {
		t.Fatalf("Failed to retry DLQ files: %v", err)
	}

	// Verify retry result
	if result.FilesRetried != 1 {
		t.Errorf("Expected 1 file retried, got %d", result.FilesRetried)
	}

	if len(result.Details) != 1 {
		t.Errorf("Expected 1 detail entry, got %d", len(result.Details))
	}

	if len(result.Details) > 0 {
		detail := result.Details[0]
		if detail.FileID != "failed_batch" {
			t.Errorf("Expected file ID 'failed_batch', got '%s'", detail.FileID)
		}

		if !detail.Success {
			t.Errorf("Expected retry to succeed, got failure: %s", detail.Error)
		}
	}

	// Verify file was moved to retry directory
	retryDir := filepath.Join(tempDir, "test-tenant", "test-dataset", "retry")
	retryFiles, err := os.ReadDir(retryDir)
	if err != nil {
		t.Fatalf("Failed to read retry directory: %v", err)
	}

	// Should have both data file and metadata file
	if len(retryFiles) != 2 {
		t.Errorf("Expected 2 files in retry (data + meta), got %d", len(retryFiles))
	}

	// Verify metadata was moved to retry and reset
	metaFilePath := filepath.Join(retryDir, "failed_batch.meta")
	metaContent, err := os.ReadFile(metaFilePath)
	if err != nil {
		t.Fatalf("Failed to read metadata file: %v", err)
	}

	var spooledFile SpooledFile
	if err := json.Unmarshal(metaContent, &spooledFile); err != nil {
		t.Fatalf("Failed to unmarshal metadata: %v", err)
	}

	// Verify metadata was reset for retry
	if spooledFile.Status != "retry" {
		t.Errorf("Expected status 'retry' after DLQ retry, got '%s'", spooledFile.Status)
	}

	if spooledFile.RetryCount != 0 {
		t.Errorf("Expected retry count 0 after DLQ retry, got %d", spooledFile.RetryCount)
	}

	if spooledFile.FailureReason != "Retrieved from DLQ for retry" {
		t.Errorf("Expected failure reason to be reset, got '%s'", spooledFile.FailureReason)
	}

	// Verify DLQ files were removed
	dlqFilesAfter, err := os.ReadDir(dlqDir)
	if err == nil && len(dlqFilesAfter) != 0 {
		t.Errorf("Expected DLQ directory to be empty after retry, got %d files", len(dlqFilesAfter))
	}
}

func TestSpoolingService_StoreBatchToQueue(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "spooling_test_")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	cfg := &config.Config{
		Spooling: config.Spooling{
			Enabled:   true,
			Directory: tempDir,
		},
	}

	service := NewSpoolingService(cfg)

	testData := []byte(`{"message": "test message", "timestamp": "2024-01-15T10:30:45Z"}`)
	err = service.StoreBatchToQueue("test-tenant", "test-dataset", "test-token", testData, "Test failure reason", "test-batch-123", "timeout")
	if err != nil {
		t.Fatalf("Failed to store batch to queue: %v", err)
	}

	// Verify queue directory was created with file
	queueDir := filepath.Join(tempDir, "test-tenant", "test-dataset", "queue")
	queueFiles, err := os.ReadDir(queueDir)
	if err != nil {
		t.Fatalf("Failed to read queue directory: %v", err)
	}

	if len(queueFiles) != 1 {
		t.Errorf("Expected 1 file in queue, got %d", len(queueFiles))
	}

	// Verify no metadata was created for queue files (new architecture)
	metaDir := filepath.Join(tempDir, "test-tenant", "test-dataset", "meta")
	_, err = os.ReadDir(metaDir)
	if !os.IsNotExist(err) {
		t.Errorf("Meta directory should not exist for queue files, but got: %v", err)
	}

	// Queue files are meant for immediate processing and don't need metadata tracking
}

func TestSpoolingService_CountFilesInDirectory(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "spooling_test_")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	cfg := &config.Config{
		Spooling: config.Spooling{
			Enabled:   true,
			Directory: tempDir,
		},
	}

	service := NewSpoolingService(cfg)

	// Create test directory with various files
	testDir := filepath.Join(tempDir, "test")
	if err := os.MkdirAll(testDir, 0750); err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}

	// Create test files
	testFiles := map[string]string{
		"file1.ndjson.gz": "test data 1",
		"file2.ndjson.gz": "test data 2 longer content",
		"file3.txt":       "should be ignored",
		"file4.ndjson":    "should be ignored",
	}

	for filename, content := range testFiles {
		filePath := filepath.Join(testDir, filename)
		if err := os.WriteFile(filePath, []byte(content), 0600); err != nil {
			t.Fatalf("Failed to create test file %s: %v", filename, err)
		}
	}

	// Count files with .ndjson.gz extension
	count, totalBytes, oldestFile := service.countFilesInDirectory(testDir, ".ndjson.gz")

	if count != 2 {
		t.Errorf("Expected 2 files, got %d", count)
	}

	expectedBytes := int64(len("test data 1") + len("test data 2 longer content"))
	if totalBytes != expectedBytes {
		t.Errorf("Expected %d bytes, got %d", expectedBytes, totalBytes)
	}

	if oldestFile == nil {
		t.Error("Expected oldest file to be found")
	}
}

func TestSpoolingService_TriggerReasonMetadata(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "spooling_test_trigger_")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	cfg := &config.Config{
		Spooling: config.Spooling{
			Enabled:   true,
			Directory: tempDir,
		},
	}

	service := NewSpoolingService(cfg)

	testCases := []struct {
		name          string
		triggerReason string
		failureReason string
	}{
		{"timeout_trigger", "timeout", "batch timeout reached"},
		{"size_trigger", "size_limit_reached", "batch size limit reached"},
		{"shutdown_trigger", "service_shutdown", "service graceful shutdown"},
		{"single_message", "single_message", "individual message processing"},
		{"restart_recovery", "service_restart", "recovered from orphaned queue"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Test MoveQueueToRetry with trigger reason
			batchID := "test-batch-" + tc.name
			tenantID := "test-tenant"
			datasetID := "test-dataset"

			// First create a queue file
			queueDir := filepath.Join(tempDir, tenantID, datasetID, "queue")
			if err := os.MkdirAll(queueDir, 0750); err != nil {
				t.Fatalf("Failed to create queue directory: %v", err)
			}

			queueFile := filepath.Join(queueDir, batchID+".ndjson.gz")
			testData := []byte(`{"message": "test data", "trigger": "` + tc.triggerReason + `"}`)
			if err := os.WriteFile(queueFile, testData, 0600); err != nil {
				t.Fatalf("Failed to create queue file: %v", err)
			}

			// Move to retry with trigger reason
			err := service.MoveQueueToRetry(tenantID, datasetID, batchID, tc.failureReason, tc.triggerReason)
			if err != nil {
				t.Fatalf("Failed to move queue to retry: %v", err)
			}

			// Verify metadata file was created with trigger reason
			retryDir := filepath.Join(tempDir, tenantID, datasetID, "retry")
			metaFile := filepath.Join(retryDir, batchID+".meta")

			if _, err := os.Stat(metaFile); os.IsNotExist(err) {
				t.Fatalf("Metadata file not created: %s", metaFile)
			}

			// Read and verify metadata content
			metaContent, err := os.ReadFile(metaFile)
			if err != nil {
				t.Fatalf("Failed to read metadata file: %v", err)
			}

			var metadata SpooledFile
			if err := json.Unmarshal(metaContent, &metadata); err != nil {
				t.Fatalf("Failed to unmarshal metadata: %v", err)
			}

			// Verify trigger reason is correctly stored
			if metadata.TriggerReason != tc.triggerReason {
				t.Errorf("Expected trigger_reason %s, got %s", tc.triggerReason, metadata.TriggerReason)
			}

			if metadata.FailureReason != tc.failureReason {
				t.Errorf("Expected failure_reason %s, got %s", tc.failureReason, metadata.FailureReason)
			}

			if metadata.Status != "retry" {
				t.Errorf("Expected status 'retry', got %s", metadata.Status)
			}

			if metadata.RetryCount != 1 {
				t.Errorf("Expected retry_count 1, got %d", metadata.RetryCount)
			}
		})
	}
}

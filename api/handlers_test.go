package api

import (
	"context"
	"github.com/bytedance/sonic"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bytefreezer/proxy/config"
	"github.com/bytefreezer/proxy/services"
)

func TestAPI_GetDLQStats(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "api_test_")
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

	spoolingService := services.NewSpoolingService(cfg)
	servicesContainer := &services.Services{
		SpoolingService: spoolingService,
	}

	api := NewAPI(servicesContainer, cfg)

	// Create test data structure
	testStructure := map[string]string{
		"tenant1/dataset1/queue/tenant1--dataset1--20240115103045--raw.gz": "test data 1",
		"tenant1/dataset1/queue/tenant1--dataset1--20240115103047--raw.gz": "test data 2",
		"tenant1/dataset1/dlq/tenant1--dataset1--20240115103050--raw.gz":   "failed data 1",
		"tenant2/dataset2/queue/tenant2--dataset2--20240115103052--raw.gz": "test data 3",
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

	// Test GetDLQStats
	handler := api.GetDLQStats()

	var input struct{}
	var output DLQStatsResponse

	err = handler.Interact(context.Background(), input, &output)
	if err != nil {
		t.Fatalf("GetDLQStats failed: %v", err)
	}

	// Verify response
	if !output.SpoolingEnabled {
		t.Error("Expected spooling to be enabled")
	}

	if output.TotalFilesInQueue != 3 {
		t.Errorf("Expected 3 files in queue, got %d", output.TotalFilesInQueue)
	}

	if output.TotalFilesInDLQ != 1 {
		t.Errorf("Expected 1 file in DLQ, got %d", output.TotalFilesInDLQ)
	}

	if len(output.TenantStats) != 2 {
		t.Errorf("Expected 2 tenants, got %d", len(output.TenantStats))
	}

	if output.SpoolDirectory != tempDir {
		t.Errorf("Expected spool directory '%s', got '%s'", tempDir, output.SpoolDirectory)
	}

	// Verify tenant stats
	tenant1Stats := output.TenantStats["tenant1"]
	if tenant1Stats == nil {
		t.Fatal("Expected tenant1 stats to exist")
	}

	if tenant1Stats.QueueFiles != 2 {
		t.Errorf("Expected 2 queue files for tenant1, got %d", tenant1Stats.QueueFiles)
	}

	if tenant1Stats.DLQFiles != 1 {
		t.Errorf("Expected 1 DLQ file for tenant1, got %d", tenant1Stats.DLQFiles)
	}
}

func TestAPI_GetDLQStats_SpoolingDisabled(t *testing.T) {
	cfg := &config.Config{
		Spooling: config.Spooling{
			Enabled:   false,
			Directory: "/tmp/test",
		},
	}

	spoolingService := services.NewSpoolingService(cfg)
	servicesContainer := &services.Services{
		SpoolingService: spoolingService,
	}

	api := NewAPI(servicesContainer, cfg)

	handler := api.GetDLQStats()

	var input struct{}
	var output DLQStatsResponse

	err := handler.Interact(context.Background(), input, &output)
	if err != nil {
		t.Fatalf("GetDLQStats failed: %v", err)
	}

	// Verify response for disabled spooling
	if output.SpoolingEnabled {
		t.Error("Expected spooling to be disabled")
	}

	if output.TotalFilesInQueue != 0 {
		t.Errorf("Expected 0 files in queue when spooling disabled, got %d", output.TotalFilesInQueue)
	}

	if output.TotalFilesInDLQ != 0 {
		t.Errorf("Expected 0 files in DLQ when spooling disabled, got %d", output.TotalFilesInDLQ)
	}

	if len(output.TenantStats) != 0 {
		t.Errorf("Expected 0 tenants when spooling disabled, got %d", len(output.TenantStats))
	}
}

func TestAPI_RetryDLQFiles(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "api_test_")
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

	spoolingService := services.NewSpoolingService(cfg)
	servicesContainer := &services.Services{
		SpoolingService: spoolingService,
	}

	api := NewAPI(servicesContainer, cfg)

	// Create directory structure that matches the expected hierarchy
	// Need to create the tenant directory first so getTenantDatasets can find it
	tenantDir := filepath.Join(tempDir, "test-tenant")
	testDatasetDir := filepath.Join(tenantDir, "test-dataset")
	dlqDir := filepath.Join(testDatasetDir, "dlq")

	// Create all necessary directories
	if err := os.MkdirAll(testDatasetDir, 0750); err != nil {
		t.Fatalf("Failed to create dataset directory: %v", err)
	}
	if err := os.MkdirAll(dlqDir, 0750); err != nil {
		t.Fatalf("Failed to create DLQ directory: %v", err)
	}

	// Create test data file
	testDataFile := filepath.Join(dlqDir, "tenant1--dataset1--20240115103045--raw.gz")
	testData := "test compressed data"
	if err := os.WriteFile(testDataFile, []byte(testData), 0600); err != nil {
		t.Fatalf("Failed to create test data file: %v", err)
	}

	// Create test metadata file - ID should match the filename without .gz
	testMetadata := services.SpooledFile{
		ID:               "tenant1--dataset1--20240115103045--raw",
		TenantID:         "test-tenant",
		DatasetID:        "test-dataset",
		BearerToken:      "test-token",
		Filename:         testDataFile,
		CompressedSize:   int64(len(testData)),
		UncompressedSize: int64(len(testData)),
		LineCount:        1,
		CreatedAt:        time.Now(),
		LastRetry:        time.Now(),
		RetryCount:       4,
		Status:           "dlq",
		FailureReason:    "Exceeded retry limit",
	}

	metaData, err := sonic.Marshal(testMetadata)
	if err != nil {
		t.Fatalf("Failed to marshal metadata: %v", err)
	}

	testMetaFile := filepath.Join(dlqDir, "tenant1--dataset1--20240115103045--raw.meta")
	if err := os.WriteFile(testMetaFile, metaData, 0600); err != nil {
		t.Fatalf("Failed to create test metadata file: %v", err)
	}

	// Test RetryDLQFiles - retry all files
	handler := api.RetryDLQFiles()

	input := &DLQRetryRequest{}
	var output DLQRetryResponse

	err = handler.Interact(context.Background(), input, &output)
	if err != nil {
		t.Fatalf("RetryDLQFiles failed: %v", err)
	}

	// Verify response
	if !output.Success {
		t.Errorf("Expected retry to succeed, got failure: %s", output.Message)
	}

	if output.FilesRetried != 1 {
		t.Errorf("Expected 1 file retried, got %d", output.FilesRetried)
	}

	if len(output.Details) != 1 {
		t.Errorf("Expected 1 detail entry, got %d", len(output.Details))
	}

	if len(output.Details) > 0 {
		detail := output.Details[0]
		if detail.FileID != "tenant1--dataset1--20240115103045--raw" {
			t.Errorf("Expected file ID 'tenant1--dataset1--20240115103045--raw', got '%s'", detail.FileID)
		}

		if detail.TenantID != "test-tenant" {
			t.Errorf("Expected tenant ID 'test-tenant', got '%s'", detail.TenantID)
		}

		if detail.DatasetID != "test-dataset" {
			t.Errorf("Expected dataset ID 'test-dataset', got '%s'", detail.DatasetID)
		}

		if !detail.Success {
			t.Errorf("Expected detail to show success, got failure: %s", detail.Error)
		}
	}

	// Verify message contains expected content
	expectedMessage := "Successfully retried 1 files from DLQ"
	if output.Message != expectedMessage {
		t.Errorf("Expected message '%s', got '%s'", expectedMessage, output.Message)
	}
}

func TestAPI_RetryDLQFiles_SpecificTenant(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "api_test_")
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

	spoolingService := services.NewSpoolingService(cfg)
	servicesContainer := &services.Services{
		SpoolingService: spoolingService,
	}

	api := NewAPI(servicesContainer, cfg)

	// Create DLQ files for multiple tenants
	tenants := []string{"tenant-a", "tenant-b"}
	datasets := []string{"dataset-1", "dataset-2"}

	for _, tenant := range tenants {
		for _, dataset := range datasets {
			// Create tenant/dataset structure so getTenantDatasets can discover them
			datasetDir := filepath.Join(tempDir, tenant, dataset)
			dlqDir := filepath.Join(datasetDir, "dlq")

			if err := os.MkdirAll(datasetDir, 0750); err != nil {
				t.Fatalf("Failed to create dataset directory: %v", err)
			}
			if err := os.MkdirAll(dlqDir, 0750); err != nil {
				t.Fatalf("Failed to create DLQ directory: %v", err)
			}

			// Create test files
			filename := "tenant1--dataset1--20240115103045--raw.gz"
			filePath := filepath.Join(dlqDir, filename)
			if err := os.WriteFile(filePath, []byte("test data"), 0600); err != nil {
				t.Fatalf("Failed to create test file: %v", err)
			}

			metaFilename := "tenant1--dataset1--20240115103045--raw.meta"
			metaPath := filepath.Join(dlqDir, metaFilename)
			metadata := services.SpooledFile{
				ID:        "tenant1--dataset1--20240115103045--raw",
				TenantID:  tenant,
				DatasetID: dataset,
				Filename:  filePath,
				Status:    "dlq",
			}
			metaData, _ := sonic.Marshal(metadata)
			if err := os.WriteFile(metaPath, metaData, 0600); err != nil {
				t.Fatalf("Failed to create test metadata file: %v", err)
			}
		}
	}

	// Test RetryDLQFiles for specific tenant
	handler := api.RetryDLQFiles()

	input := &DLQRetryRequest{
		TenantID: "tenant-a",
	}
	var output DLQRetryResponse

	err = handler.Interact(context.Background(), input, &output)
	if err != nil {
		t.Fatalf("RetryDLQFiles failed: %v", err)
	}

	// Verify response
	if !output.Success {
		t.Errorf("Expected retry to succeed, got failure: %s", output.Message)
	}

	if output.FilesRetried != 2 {
		t.Errorf("Expected 2 files retried for tenant-a, got %d", output.FilesRetried)
	}

	expectedMessage := "Successfully retried 2 files for tenant=tenant-a"
	if output.Message != expectedMessage {
		t.Errorf("Expected message '%s', got '%s'", expectedMessage, output.Message)
	}

	// Verify only tenant-a files were processed
	for _, detail := range output.Details {
		if detail.TenantID != "tenant-a" {
			t.Errorf("Expected only tenant-a files, got tenant %s", detail.TenantID)
		}
	}
}

func TestAPI_RetryDLQFiles_SpecificTenantAndDataset(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "api_test_")
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

	spoolingService := services.NewSpoolingService(cfg)
	servicesContainer := &services.Services{
		SpoolingService: spoolingService,
	}

	api := NewAPI(servicesContainer, cfg)

	// Create DLQ files for specific tenant and dataset
	datasetDir := filepath.Join(tempDir, "specific-tenant", "specific-dataset")
	dlqDir := filepath.Join(datasetDir, "dlq")

	if err := os.MkdirAll(datasetDir, 0750); err != nil {
		t.Fatalf("Failed to create dataset directory: %v", err)
	}
	if err := os.MkdirAll(dlqDir, 0750); err != nil {
		t.Fatalf("Failed to create DLQ directory: %v", err)
	}

	// Create test file
	filePath := filepath.Join(dlqDir, "tenant1--dataset1--20240115103045--raw.gz")
	if err := os.WriteFile(filePath, []byte("test data"), 0600); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	metaPath := filepath.Join(dlqDir, "tenant1--dataset1--20240115103045--raw.meta")
	metadata := services.SpooledFile{
		ID:        "tenant1--dataset1--20240115103045--raw",
		TenantID:  "specific-tenant",
		DatasetID: "specific-dataset",
		Filename:  filePath,
		Status:    "dlq",
	}
	metaData, _ := sonic.Marshal(metadata)
	if err := os.WriteFile(metaPath, metaData, 0600); err != nil {
		t.Fatalf("Failed to create test metadata file: %v", err)
	}

	// Test RetryDLQFiles for specific tenant and dataset
	handler := api.RetryDLQFiles()

	input := &DLQRetryRequest{
		TenantID:  "specific-tenant",
		DatasetID: "specific-dataset",
	}
	var output DLQRetryResponse

	err = handler.Interact(context.Background(), input, &output)
	if err != nil {
		t.Fatalf("RetryDLQFiles failed: %v", err)
	}

	// Verify response
	if !output.Success {
		t.Errorf("Expected retry to succeed, got failure: %s", output.Message)
	}

	if output.FilesRetried != 1 {
		t.Errorf("Expected 1 file retried, got %d", output.FilesRetried)
	}

	expectedMessage := "Successfully retried 1 files for tenant=specific-tenant dataset=specific-dataset"
	if output.Message != expectedMessage {
		t.Errorf("Expected message '%s', got '%s'", expectedMessage, output.Message)
	}

	// Verify correct file was processed
	if len(output.Details) != 1 {
		t.Errorf("Expected 1 detail entry, got %d", len(output.Details))
	}

	if len(output.Details) > 0 {
		detail := output.Details[0]
		if detail.TenantID != "specific-tenant" {
			t.Errorf("Expected tenant 'specific-tenant', got '%s'", detail.TenantID)
		}
		if detail.DatasetID != "specific-dataset" {
			t.Errorf("Expected dataset 'specific-dataset', got '%s'", detail.DatasetID)
		}
	}
}

func TestAPI_RetryDLQFiles_SpoolingDisabled(t *testing.T) {
	cfg := &config.Config{
		Spooling: config.Spooling{
			Enabled:   false,
			Directory: "/tmp/test",
		},
	}

	spoolingService := services.NewSpoolingService(cfg)
	servicesContainer := &services.Services{
		SpoolingService: spoolingService,
	}

	api := NewAPI(servicesContainer, cfg)

	handler := api.RetryDLQFiles()

	input := &DLQRetryRequest{}
	var output DLQRetryResponse

	err := handler.Interact(context.Background(), input, &output)
	if err != nil {
		t.Fatalf("RetryDLQFiles failed: %v", err)
	}

	// Verify response for disabled spooling
	if output.Success {
		t.Error("Expected retry to fail when spooling is disabled")
	}

	if output.Message != "Spooling is disabled" {
		t.Errorf("Expected message 'Spooling is disabled', got '%s'", output.Message)
	}

	if output.FilesRetried != 0 {
		t.Errorf("Expected 0 files retried when spooling disabled, got %d", output.FilesRetried)
	}
}

// Helper function to test JSON marshaling/unmarshaling of API types
func TestDLQStatsResponse_JSON(t *testing.T) {
	response := DLQStatsResponse{
		SpoolingEnabled:   true,
		TotalFilesInQueue: 5,
		TotalFilesInDLQ:   3,
		TotalBytesInQueue: 1024,
		TotalBytesInDLQ:   512,
		TenantStats: map[string]*TenantDLQStats{
			"test-tenant": {
				QueueFiles: 3,
				DLQFiles:   2,
				QueueBytes: 768,
				DLQBytes:   256,
				DatasetStats: map[string]*DatasetDLQStats{
					"test-dataset": {
						QueueFiles: 3,
						DLQFiles:   2,
						QueueBytes: 768,
						DLQBytes:   256,
					},
				},
			},
		},
		OldestQueueFile: &FileInfo{
			ID:               "test-file",
			TenantID:         "test-tenant",
			DatasetID:        "test-dataset",
			CompressedSize:   100,
			UncompressedSize: 100,
			CreatedAt:        time.Now(),
		},
		SpoolDirectory: "/test/spool",
	}

	// Test JSON marshaling
	jsonData, err := sonic.Marshal(response)
	if err != nil {
		t.Fatalf("Failed to marshal DLQStatsResponse: %v", err)
	}

	// Test JSON unmarshaling
	var unmarshaled DLQStatsResponse
	err = sonic.Unmarshal(jsonData, &unmarshaled)
	if err != nil {
		t.Fatalf("Failed to unmarshal DLQStatsResponse: %v", err)
	}

	// Verify key fields
	if unmarshaled.SpoolingEnabled != response.SpoolingEnabled {
		t.Error("SpoolingEnabled mismatch after JSON round-trip")
	}

	if unmarshaled.TotalFilesInQueue != response.TotalFilesInQueue {
		t.Error("TotalFilesInQueue mismatch after JSON round-trip")
	}

	if len(unmarshaled.TenantStats) != len(response.TenantStats) {
		t.Error("TenantStats length mismatch after JSON round-trip")
	}
}

func TestDLQRetryResponse_JSON(t *testing.T) {
	response := DLQRetryResponse{
		Success:      true,
		Message:      "Successfully retried 2 files",
		FilesRetried: 2,
		Details: []DLQRetryDetail{
			{
				FileID:    "file1",
				TenantID:  "tenant1",
				DatasetID: "dataset1",
				Success:   true,
			},
			{
				FileID:    "file2",
				TenantID:  "tenant1",
				DatasetID: "dataset2",
				Success:   false,
				Error:     "Permission denied",
			},
		},
	}

	// Test JSON marshaling
	jsonData, err := sonic.Marshal(response)
	if err != nil {
		t.Fatalf("Failed to marshal DLQRetryResponse: %v", err)
	}

	// Test JSON unmarshaling
	var unmarshaled DLQRetryResponse
	err = sonic.Unmarshal(jsonData, &unmarshaled)
	if err != nil {
		t.Fatalf("Failed to unmarshal DLQRetryResponse: %v", err)
	}

	// Verify key fields
	if unmarshaled.Success != response.Success {
		t.Error("Success mismatch after JSON round-trip")
	}

	if unmarshaled.FilesRetried != response.FilesRetried {
		t.Error("FilesRetried mismatch after JSON round-trip")
	}

	if len(unmarshaled.Details) != len(response.Details) {
		t.Error("Details length mismatch after JSON round-trip")
	}

	if len(unmarshaled.Details) > 0 {
		if unmarshaled.Details[0].FileID != response.Details[0].FileID {
			t.Error("Details[0].FileID mismatch after JSON round-trip")
		}
	}
}

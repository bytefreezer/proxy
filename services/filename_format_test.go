package services

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/n0needt0/bytefreezer-proxy/domain"
)

func TestGenerateProxyFilename(t *testing.T) {
	testTime := time.Date(2025, 1, 15, 10, 30, 45, 123456789, time.UTC)
	expectedNanos := testTime.UnixNano()

	testCases := []struct {
		name      string
		tenantID  string
		datasetID string
		extension string
		expected  string
	}{
		{
			name:      "standard raw format",
			tenantID:  "acme",
			datasetID: "logs",
			extension: "raw",
			expected:  fmt.Sprintf("acme--logs--%d--raw.gz", expectedNanos),
		},
		{
			name:      "csv format",
			tenantID:  "company",
			datasetID: "metrics",
			extension: "csv",
			expected:  fmt.Sprintf("company--metrics--%d--csv.gz", expectedNanos),
		},
		{
			name:      "ndjson format",
			tenantID:  "tenant1",
			datasetID: "dataset1",
			extension: "ndjson",
			expected:  fmt.Sprintf("tenant1--dataset1--%d--ndjson.gz", expectedNanos),
		},
		{
			name:      "special characters in tenant",
			tenantID:  "test_tenant",
			datasetID: "test-dataset",
			extension: "json",
			expected:  fmt.Sprintf("test_tenant--test-dataset--%d--json.gz", expectedNanos),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := generateProxyFilename(tc.tenantID, tc.datasetID, testTime, tc.extension)
			if result != tc.expected {
				t.Errorf("Expected %s, got %s", tc.expected, result)
			}

			// Verify format structure
			parts := strings.Split(strings.TrimSuffix(result, ".gz"), "--")
			if len(parts) != 4 {
				t.Errorf("Expected 4 parts separated by --, got %d parts: %v", len(parts), parts)
			}

			if parts[0] != tc.tenantID {
				t.Errorf("Expected tenant %s, got %s", tc.tenantID, parts[0])
			}

			if parts[1] != tc.datasetID {
				t.Errorf("Expected dataset %s, got %s", tc.datasetID, parts[1])
			}

			if parts[2] != fmt.Sprintf("%d", testTime.UnixNano()) {
				t.Errorf("Expected timestamp %d, got %s", testTime.UnixNano(), parts[2])
			}

			if parts[3] != tc.extension {
				t.Errorf("Expected extension %s, got %s", tc.extension, parts[3])
			}
		})
	}
}

func TestExtractDataHint(t *testing.T) {
	testCases := []struct {
		name     string
		filename string
		expected string
	}{
		{
			name:     "new format - raw",
			filename: "acme--logs--1736938245123456789--raw.gz",
			expected: "raw",
		},
		{
			name:     "new format - csv",
			filename: "company--metrics--1736938245123456789--csv.gz",
			expected: "csv",
		},
		{
			name:     "new format - ndjson",
			filename: "tenant1--dataset1--1736938245123456789--ndjson.gz",
			expected: "ndjson",
		},
		{
			name:     "old format - raw",
			filename: "batch_123456789.raw.gz",
			expected: "raw",
		},
		{
			name:     "old format - csv",
			filename: "batch_987654321.csv.gz",
			expected: "csv",
		},
		{
			name:     "malformed - missing extension",
			filename: "batch_123456789..gz",
			expected: "",
		},
		{
			name:     "malformed - no extension",
			filename: "batch_123456789.gz",
			expected: "",
		},
		{
			name:     "with path",
			filename: "/path/to/file/acme--logs--123456789--json.gz",
			expected: "json",
		},
		{
			name:     "no .gz suffix",
			filename: "acme--logs--123456789--xml",
			expected: "xml",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := extractDataHint(tc.filename)
			if result != tc.expected {
				t.Errorf("Expected %s, got %s", tc.expected, result)
			}
		})
	}
}

func TestForwarderWithNewFilenameFormat(t *testing.T) {
	// Test that forwarder generates the correct filename format
	testTime := time.Date(2025, 1, 15, 10, 30, 45, 123456789, time.UTC)

	batch := &domain.DataBatch{
		ID:            "batch_12345",
		TenantID:      "test-tenant",
		DatasetID:     "test-dataset",
		DataHint:      "raw",
		CreatedAt:     testTime,
		Data:          []byte("test data"),
		LineCount:     1,
		TotalBytes:    9,
	}

	expectedFilename := fmt.Sprintf("test-tenant--test-dataset--%d--raw.gz", testTime.UnixNano())
	actualFilename := generateProxyFilename(batch.TenantID, batch.DatasetID, batch.CreatedAt, batch.DataHint)

	if actualFilename != expectedFilename {
		t.Errorf("Expected filename %s, got %s", expectedFilename, actualFilename)
	}

	// Verify we can extract the data hint back
	extractedHint := extractDataHint(actualFilename)
	if extractedHint != batch.DataHint {
		t.Errorf("Expected extracted data hint %s, got %s", batch.DataHint, extractedHint)
	}
}

func TestFilenameFormatBackwardCompatibility(t *testing.T) {
	// Test that we can still extract extensions from old format files
	oldFormatTests := []struct {
		filename  string
		extension string
	}{
		{"batch_20250115103045.raw.gz", "raw"},
		{"batch_20250115103045.csv.gz", "csv"},
		{"tenant1--dataset1--20250115103045--ndjson.gz", "ndjson"},
		{"batch_20250115103045.json.gz", "json"},
	}

	for _, test := range oldFormatTests {
		t.Run(fmt.Sprintf("old_format_%s", test.extension), func(t *testing.T) {
			result := extractDataHint(test.filename)
			if result != test.extension {
				t.Errorf("Expected %s, got %s for filename %s", test.extension, result, test.filename)
			}
		})
	}
}
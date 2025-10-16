package services

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/n0needt0/bytefreezer-proxy/config"
	"github.com/n0needt0/bytefreezer-proxy/domain"
	"github.com/n0needt0/go-goodies/log"
)

// UploadError represents an upload failure with retry information
type UploadError struct {
	StatusCode    int
	Message       string
	IsPermanent   bool // true if this error should not be retried
	FailureReason string
}

func (e *UploadError) Error() string {
	return e.Message
}

// isPermanentHTTPError determines if an HTTP status code represents a permanent failure
func isPermanentHTTPError(statusCode int) bool {
	switch statusCode {
	case http.StatusBadRequest,          // 400 - Bad Request
		http.StatusUnauthorized,         // 401 - Unauthorized (inactive tenant)
		http.StatusForbidden,            // 403 - Forbidden
		http.StatusNotFound,             // 404 - Not Found
		http.StatusGone,                 // 410 - Gone (explicitly deactivated)
		http.StatusRequestEntityTooLarge, // 413 - Payload Too Large
		http.StatusUnprocessableEntity:  // 422 - Unprocessable Entity
		return true
	case http.StatusTooManyRequests: // 429 - Rate limited (transient, should retry)
		return false
	default:
		// 5xx errors are transient (server errors, should retry)
		if statusCode >= 500 && statusCode < 600 {
			return false
		}
		// Other 4xx errors are permanent
		if statusCode >= 400 && statusCode < 500 {
			return true
		}
		return false
	}
}

// HTTPForwarder handles HTTP forwarding to bytefreezer-receiver
type HTTPForwarder struct {
	config         *config.Config
	httpClient     *http.Client
	metricsService *MetricsService
}

// NewHTTPForwarder creates a new HTTP forwarder with connection pooling
func NewHTTPForwarder(cfg *config.Config) *HTTPForwarder {
	// Create custom transport with connection pooling
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:        cfg.GetMaxIdleConns(),
		MaxIdleConnsPerHost: cfg.GetMaxConnsPerHost(),
		MaxConnsPerHost:     cfg.GetMaxConnsPerHost(),
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
		DisableCompression:  false, // Enable gzip compression
	}

	return &HTTPForwarder{
		config: cfg,
		httpClient: &http.Client{
			Timeout:   cfg.GetReceiverTimeout(),
			Transport: transport,
		},
		metricsService: nil, // Will be set if needed
	}
}

// NewHTTPForwarderWithMetrics creates a new HTTP forwarder with metrics service and connection pooling
func NewHTTPForwarderWithMetrics(cfg *config.Config, metricsService *MetricsService) *HTTPForwarder {
	// Create custom transport with connection pooling
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:        cfg.GetMaxIdleConns(),
		MaxIdleConnsPerHost: cfg.GetMaxConnsPerHost(),
		MaxConnsPerHost:     cfg.GetMaxConnsPerHost(),
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
		DisableCompression:  false, // Enable gzip compression
	}

	return &HTTPForwarder{
		config: cfg,
		httpClient: &http.Client{
			Timeout:   cfg.GetReceiverTimeout(),
			Transport: transport,
		},
		metricsService: metricsService,
	}
}

// NewRetryHTTPForwarder creates a new HTTP forwarder specifically for retry processing with dedicated connection pool
func NewRetryHTTPForwarder(cfg *config.Config) *HTTPForwarder {
	// Create custom transport with dedicated retry connection pooling
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:        cfg.GetRetryMaxIdleConns(),    // Dedicated retry pool size
		MaxIdleConnsPerHost: cfg.GetRetryMaxConnsPerHost(), // Dedicated retry connections per host
		MaxConnsPerHost:     cfg.GetRetryMaxConnsPerHost(), // Dedicated retry connections per host
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
		DisableCompression:  false, // Enable gzip compression
	}

	return &HTTPForwarder{
		config: cfg,
		httpClient: &http.Client{
			Timeout:   cfg.GetReceiverTimeout(),
			Transport: transport,
		},
		metricsService: nil, // Retry forwarder doesn't need metrics
	}
}

// ForwardBatch forwards a data batch to bytefreezer-receiver
func (f *HTTPForwarder) ForwardBatch(batch *domain.DataBatch) error {
	// Replace placeholders in base URL with actual tenant and dataset IDs, and add file extension
	url := f.config.Receiver.BaseURL
	url = strings.ReplaceAll(url, "{tenantid}", batch.TenantID)
	url = strings.ReplaceAll(url, "{datasetid}", batch.DatasetID)

	// File extension is communicated via headers, not URL path

	// Create request
	req, err := http.NewRequest("POST", url, bytes.NewReader(batch.Data))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("User-Agent", fmt.Sprintf("%s/%s", f.config.App.Name, f.config.App.Version))

	// Add Bearer authentication header if token is configured
	bearerToken := batch.BearerToken
	if bearerToken == "" {
		bearerToken = f.config.BearerToken // Fallback to global token
	}
	if bearerToken != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", bearerToken))
	}

	// Always send compressed raw data
	req.Header.Set("Content-Encoding", "gzip")
	req.Header.Set("Content-Type", "application/octet-stream")

	// Add custom headers for metadata
	req.Header.Set("X-Proxy-Batch-ID", batch.ID)
	req.Header.Set("X-Proxy-Line-Count", fmt.Sprintf("%d", batch.LineCount))
	req.Header.Set("X-Proxy-Original-Bytes", fmt.Sprintf("%d", batch.TotalBytes))
	req.Header.Set("X-Proxy-Created-At", batch.CreatedAt.Format(time.RFC3339))
	req.Header.Set("X-Proxy-Data-Hint", batch.DataHint) // Data format hint for downstream processing

	// Use the original filename from spooling to maintain consistency
	filename := batch.Filename
	if filename == "" {
		// Fallback: generate new filename only if not provided
		filename = generateProxyFilename(batch.TenantID, batch.DatasetID, batch.CreatedAt, batch.DataHint)
	}
	req.Header.Set("X-Proxy-Filename", filename)

	log.Infof("📁 Sending to receiver: URL=%s, Filename=%s, DataHint=%s", url, filename, batch.DataHint)

	// Single HTTP attempt only - file-level retry handles failures
	resp, err := f.httpClient.Do(req)
	if err != nil {
		log.Warnf("Batch %s upload failed - network error to %s: %v", batch.ID, url, err)
		return fmt.Errorf("HTTP request failed: %w", err)
	}

	// Read response body for debugging
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	// Check response status
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		log.Debugf("Successfully forwarded batch %s to %s (status: %d)",
			batch.ID, url, resp.StatusCode)

		// Preserve source files if keep_src is enabled
		if f.config.Spooling.KeepSrc {
			if err := f.preserveSource(batch, req.Header); err != nil {
				log.Warnf("Failed to preserve source for batch %s: %v", batch.ID, err)
			}
		}

		return nil
	}

	// Log detailed error from receiver
	bodyStr := string(body)
	if bodyStr == "" {
		bodyStr = "(empty response body)"
	}

	// Determine if this is a permanent failure
	isPermanent := isPermanentHTTPError(resp.StatusCode)
	failureType := "transient"
	if isPermanent {
		failureType = "permanent"
	}

	log.Warnf("Batch %s upload failed (%s) - %s returned HTTP %d: %s",
		batch.ID, failureType, url, resp.StatusCode, bodyStr)

	// Return custom error with retry information
	return &UploadError{
		StatusCode:    resp.StatusCode,
		Message:       fmt.Sprintf("HTTP request failed with status %d: %s", resp.StatusCode, bodyStr),
		IsPermanent:   isPermanent,
		FailureReason: fmt.Sprintf("HTTP %d from receiver: %s", resp.StatusCode, bodyStr),
	}
}

// generateProxyFilename creates a filename in format: tenant--dataset--timestamp--extension.gz
func generateProxyFilename(tenantID, datasetID string, createdAt time.Time, dataHint string) string {
	timestamp := createdAt.UnixNano()
	return fmt.Sprintf("%s--%s--%d--%s.gz", tenantID, datasetID, timestamp, dataHint)
}

// extractDataHint parses data hint from proxy filename format
func extractDataHint(filename string) string {
	// Remove path if present
	basename := filepath.Base(filename)

	// Remove .gz suffix
	basename = strings.TrimSuffix(basename, ".gz")

	// Split by -- separator
	parts := strings.Split(basename, "--")
	if len(parts) >= 4 {
		return parts[3] // data hint is 4th part
	}

	// Fallback: try old format batch_id.datahint.gz
	if strings.Contains(basename, ".") {
		parts := strings.Split(basename, ".")
		if len(parts) >= 2 {
			return parts[len(parts)-1] // last part before .gz (data hint)
		}
	}

	return ""
}

// preserveSource saves the batch file and headers to {dataset}/src directory for debugging/audit
func (f *HTTPForwarder) preserveSource(batch *domain.DataBatch, headers http.Header) error {
	// Create src directory path: {spooldir}/{tenantid}/{dataset}/src/
	srcDir := filepath.Join(f.config.Spooling.Directory, batch.TenantID, batch.DatasetID, "src")
	if err := os.MkdirAll(srcDir, 0750); err != nil {
		return fmt.Errorf("failed to create src directory %s: %w", srcDir, err)
	}

	// Get source file path (current location of the batch file in queue directory)
	srcFile := batch.Filename
	if !filepath.IsAbs(srcFile) {
		// Batch files are located in the queue directory: {spooldir}/{tenantid}/{datasetid}/queue/{filename}
		srcFile = filepath.Join(f.config.Spooling.Directory, batch.TenantID, batch.DatasetID, "queue", srcFile)
	}

	// Generate target filename in src directory
	baseFilename := filepath.Base(batch.Filename)
	targetFile := filepath.Join(srcDir, baseFilename)
	headersFile := targetFile + ".headers"

	// Copy the batch file to src directory
	if err := f.copyFile(srcFile, targetFile); err != nil {
		return fmt.Errorf("failed to copy batch file to src: %w", err)
	}

	// Create headers file with all HTTP headers sent to receiver
	headersContent := f.formatHeaders(headers)
	if err := os.WriteFile(headersFile, []byte(headersContent), 0600); err != nil {
		return fmt.Errorf("failed to write headers file: %w", err)
	}

	log.Debugf("Preserved source files for batch %s: %s and %s", batch.ID, targetFile, headersFile)
	return nil
}

// copyFile copies a file from src to dst with path validation
func (f *HTTPForwarder) copyFile(src, dst string) error {
	// Validate paths to prevent directory traversal
	if !filepath.IsAbs(src) {
		return fmt.Errorf("source path must be absolute: %s", src)
	}
	if !filepath.IsAbs(dst) {
		return fmt.Errorf("destination path must be absolute: %s", dst)
	}

	// Clean paths to remove any .. elements
	src = filepath.Clean(src)
	dst = filepath.Clean(dst)

	srcFile, err := os.Open(filepath.Clean(src))
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(filepath.Clean(dst))
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}

// formatHeaders formats HTTP headers for storage
func (f *HTTPForwarder) formatHeaders(headers http.Header) string {
	var lines []string
	for key, values := range headers {
		for _, value := range values {
			lines = append(lines, fmt.Sprintf("%s: %s", key, value))
		}
	}
	sort.Strings(lines) // Sort for consistent output
	return strings.Join(lines, "\n") + "\n"
}

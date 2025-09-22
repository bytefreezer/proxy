package services

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/n0needt0/bytefreezer-proxy/config"
	"github.com/n0needt0/bytefreezer-proxy/domain"
	"github.com/n0needt0/go-goodies/log"
)

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
	req.Header.Set("X-Proxy-File-Extension", batch.FileExtension) // Plugin-defined file extension

	// Complete filename for receiver queue storage
	filename := fmt.Sprintf("%s.%s.gz", batch.ID, batch.FileExtension)
	req.Header.Set("X-Proxy-Filename", filename)

	log.Infof("📁 Sending to receiver: URL=%s, Filename=%s, Extension=%s", url, filename, batch.FileExtension)

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
		return nil
	}

	// Log detailed error from receiver
	bodyStr := string(body)
	if bodyStr == "" {
		bodyStr = "(empty response body)"
	}
	log.Warnf("Batch %s upload failed - %s returned HTTP %d: %s",
		batch.ID, url, resp.StatusCode, bodyStr)
	return fmt.Errorf("HTTP request failed with status %d: %s", resp.StatusCode, bodyStr)
}

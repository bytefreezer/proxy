package services

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/n0needt0/bytefreezer-proxy/config"
	"github.com/n0needt0/bytefreezer-proxy/domain"
	"github.com/n0needt0/go-goodies/log"
)

// HTTPForwarder handles HTTP forwarding to bytefreezer-receiver
type HTTPForwarder struct {
	config     *config.Config
	httpClient *http.Client
}

// NewHTTPForwarder creates a new HTTP forwarder
func NewHTTPForwarder(cfg *config.Config) *HTTPForwarder {
	return &HTTPForwarder{
		config: cfg,
		httpClient: &http.Client{
			Timeout: cfg.GetReceiverTimeout(),
		},
	}
}

// ForwardBatch forwards a data batch to bytefreezer-receiver
func (f *HTTPForwarder) ForwardBatch(batch *domain.DataBatch) error {
	// Replace placeholders in base URL with actual tenant and dataset IDs
	url := f.config.Receiver.BaseURL
	url = strings.ReplaceAll(url, "{tenantid}", batch.TenantID)
	url = strings.ReplaceAll(url, "{datasetid}", batch.DatasetID)

	// Create request
	req, err := http.NewRequest("POST", url, bytes.NewReader(batch.Data))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("User-Agent", fmt.Sprintf("%s/%s", f.config.App.Name, f.config.App.Version))

	// Add Bearer authentication header if token is configured
	if f.config.BearerToken != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", f.config.BearerToken))
	}

	if f.config.UDP.EnableCompression {
		req.Header.Set("Content-Encoding", "gzip")
		req.Header.Set("Content-Type", "application/x-ndjson")
	} else {
		req.Header.Set("Content-Type", "application/x-ndjson")
	}

	// Add custom headers for metadata
	req.Header.Set("X-Proxy-Batch-ID", batch.ID)
	req.Header.Set("X-Proxy-Line-Count", fmt.Sprintf("%d", batch.LineCount))
	req.Header.Set("X-Proxy-Original-Bytes", fmt.Sprintf("%d", batch.TotalBytes))
	req.Header.Set("X-Proxy-Created-At", batch.CreatedAt.Format(time.RFC3339))

	// Retry logic
	var lastErr error
	for attempt := 0; attempt <= f.config.Receiver.RetryCount; attempt++ {
		if attempt > 0 {
			log.Debugf("Retrying batch %s, attempt %d/%d", batch.ID, attempt, f.config.Receiver.RetryCount)
			time.Sleep(f.config.GetRetryDelay())
		}

		resp, err := f.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("HTTP request failed: %w", err)
			continue
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

		lastErr = fmt.Errorf("HTTP request failed with status %d: %s", resp.StatusCode, string(body))

		// Don't retry on client errors (4xx)
		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			break
		}
	}

	return fmt.Errorf("failed to forward batch after %d attempts: %w", f.config.Receiver.RetryCount+1, lastErr)
}

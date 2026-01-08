// Licensed under Elastic License 2.0
// See LICENSE.txt for details

package services

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/bytefreezer/goodies/log"
	"github.com/bytefreezer/proxy/config"
	"github.com/bytefreezer/proxy/plugins"
)

// ConnectivityTestResult represents the result of a connectivity test
type ConnectivityTestResult struct {
	TenantID     string `json:"tenant_id"`
	DatasetID    string `json:"dataset_id"`
	PluginName   string `json:"plugin_name"`
	PluginType   string `json:"plugin_type"`
	BearerToken  string `json:"bearer_token,omitempty"`
	ReceiverURL  string `json:"receiver_url"`
	Status       string `json:"status"` // "success", "failed", "error"
	StatusCode   int    `json:"status_code,omitempty"`
	ResponseTime string `json:"response_time"`
	ErrorMessage string `json:"error_message,omitempty"`
	ResponseBody string `json:"response_body,omitempty"`
}

// ConnectivityTestService handles connectivity testing to bytefreezer-receiver
type ConnectivityTestService struct {
	config     *config.Config
	httpClient *http.Client
}

// NewConnectivityTestService creates a new connectivity test service
func NewConnectivityTestService(cfg *config.Config) *ConnectivityTestService {
	return &ConnectivityTestService{
		config: cfg,
		httpClient: &http.Client{
			Timeout: 10 * time.Second, // Shorter timeout for connectivity tests
		},
	}
}

// TestAllConnections tests connectivity to receiver for all configured plugins/tenants/datasets
func (c *ConnectivityTestService) TestAllConnections() ([]ConnectivityTestResult, error) {
	var results []ConnectivityTestResult

	// Test each input plugin configuration with its specific tenant/dataset/destination
	for _, input := range c.config.Inputs {
		result := c.testPluginConnectivity(input)
		results = append(results, result)
	}

	// If no plugins configured, test global configuration
	if len(c.config.Inputs) == 0 {
		log.Warn("No input plugins configured, testing global configuration")
		result := c.testConnection(
			c.config.TenantID,
			"unknown",
			"global",
			"unknown",
			c.config.BearerToken,
		)
		results = append(results, result)
	}

	return results, nil
}

// TestSpecificConnection tests connectivity for a specific tenant/dataset
func (c *ConnectivityTestService) TestSpecificConnection(tenantID, datasetID string) (*ConnectivityTestResult, error) {
	// Find the plugin configuration for this tenant/dataset or use global config
	var pluginName, pluginType string
	var bearerToken string

	// Look for plugin that matches this tenant/dataset
	for _, input := range c.config.Inputs {
		if configDatasetID, ok := input.Config["dataset_id"].(string); ok && configDatasetID == datasetID {
			pluginName = input.Name
			pluginType = input.Type
			if pluginBearerToken, ok := input.Config["bearer_token"].(string); ok && pluginBearerToken != "" {
				bearerToken = pluginBearerToken
			}
			break
		}
	}

	// Use global config if no plugin-specific config found
	if pluginName == "" {
		pluginName = "global"
		pluginType = "unknown"
	}
	if bearerToken == "" {
		bearerToken = c.config.BearerToken
	}

	result := c.testConnection(tenantID, datasetID, pluginName, pluginType, bearerToken)
	return &result, nil
}

// testPluginConnectivity tests connectivity for a specific plugin configuration
func (c *ConnectivityTestService) testPluginConnectivity(input plugins.PluginConfig) ConnectivityTestResult {
	// Determine tenant ID and dataset ID for this plugin
	tenantID := c.config.TenantID // Use global tenant ID

	// Extract dataset_id from plugin config
	datasetID, _ := input.Config["dataset_id"].(string)
	if datasetID == "" {
		datasetID = "unknown"
	}

	// Extract bearer token from plugin config
	bearerToken, _ := input.Config["bearer_token"].(string)
	if bearerToken == "" {
		bearerToken = c.config.BearerToken
	}

	return c.testConnection(tenantID, datasetID, input.Name, input.Type, bearerToken)
}

// testConnection performs the actual connectivity test
func (c *ConnectivityTestService) testConnection(tenantID, datasetID, pluginName, pluginType, bearerToken string) ConnectivityTestResult {
	start := time.Now()

	result := ConnectivityTestResult{
		TenantID:    tenantID,
		DatasetID:   datasetID,
		PluginName:  pluginName,
		PluginType:  pluginType,
		BearerToken: maskToken(bearerToken),
	}

	// Build receiver URL from base URL + tenant/dataset path
	url := strings.TrimSuffix(c.config.Receiver.BaseURL, "/") + "/" + tenantID + "/" + datasetID
	result.ReceiverURL = url

	// Create test payload (small NDJSON data)
	testData := `{"test": "connectivity_check", "timestamp": "` + time.Now().Format(time.RFC3339) + `", "plugin": "` + pluginName + `"}` + "\n"

	// Compress the test data
	var compressed bytes.Buffer
	gzipWriter, err := gzip.NewWriterLevel(&compressed, 6)
	if err != nil {
		result.Status = "error"
		result.ErrorMessage = fmt.Sprintf("Failed to create gzip writer: %v", err)
		result.ResponseTime = time.Since(start).String()
		return result
	}

	if _, err := gzipWriter.Write([]byte(testData)); err != nil {
		result.Status = "error"
		result.ErrorMessage = fmt.Sprintf("Failed to compress test data: %v", err)
		result.ResponseTime = time.Since(start).String()
		return result
	}

	if err := gzipWriter.Close(); err != nil {
		result.Status = "error"
		result.ErrorMessage = fmt.Sprintf("Failed to close gzip writer: %v", err)
		result.ResponseTime = time.Since(start).String()
		return result
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(context.Background(), "POST", url, bytes.NewReader(compressed.Bytes()))
	if err != nil {
		result.Status = "error"
		result.ErrorMessage = fmt.Sprintf("Failed to create request: %v", err)
		result.ResponseTime = time.Since(start).String()
		return result
	}

	// Set headers
	req.Header.Set("User-Agent", fmt.Sprintf("%s/%s-connectivity-test", c.config.App.Name, c.config.App.Version))
	req.Header.Set("Content-Encoding", "gzip")
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("X-Proxy-Test", "connectivity-check")
	req.Header.Set("X-Proxy-Plugin", pluginName)

	// Add Bearer authentication header
	if bearerToken != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", bearerToken))
	}

	// Make the request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		result.Status = "failed"
		result.ErrorMessage = fmt.Sprintf("HTTP request failed: %v", err)
		result.ResponseTime = time.Since(start).String()
		return result
	}
	defer resp.Body.Close()

	// Read response
	body := make([]byte, 1024) // Limit response body size
	n, _ := resp.Body.Read(body)
	responseBody := string(body[:n])

	result.StatusCode = resp.StatusCode
	result.ResponseTime = time.Since(start).String()
	result.ResponseBody = responseBody

	// Determine success/failure
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		result.Status = "success"
		log.Debugf("Connectivity test successful for %s/%s via %s (status: %d, time: %s)",
			tenantID, datasetID, pluginName, resp.StatusCode, result.ResponseTime)
	} else {
		result.Status = "failed"
		result.ErrorMessage = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, responseBody)
		log.Warnf("Connectivity test failed for %s/%s via %s (status: %d, time: %s): %s",
			tenantID, datasetID, pluginName, resp.StatusCode, result.ResponseTime, responseBody)
	}

	return result
}

// maskToken masks a bearer token for display purposes
func maskToken(token string) string {
	if token == "" {
		return ""
	}
	if len(token) <= 8 {
		return "***"
	}
	return token[:4] + "***" + token[len(token)-4:]
}

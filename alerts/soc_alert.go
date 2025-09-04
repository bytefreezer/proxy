package alerts

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/n0needt0/go-goodies/log"
)

type SOCAlertClient struct {
	config AlertClientConfig
}

type AlertClientConfig struct {
	SOC SOCConfig
	App AppConfig
	Dev bool
}

type SOCConfig struct {
	Enabled  bool
	Endpoint string
	Timeout  int
}

type AppConfig struct {
	Name    string
	Version string
}

type AlertPayload struct {
	Service   string                 `json:"service"`
	Version   string                 `json:"version"`
	Severity  string                 `json:"severity"`
	Title     string                 `json:"title"`
	Message   string                 `json:"message"`
	Details   map[string]interface{} `json:"details"`
	Timestamp string                 `json:"timestamp"`
}

func NewSOCAlertClient(config AlertClientConfig) *SOCAlertClient {
	return &SOCAlertClient{
		config: config,
	}
}

func (client *SOCAlertClient) SendCriticalAlert(title, message, details string) error {
	return client.sendAlert("critical", title, message, details)
}

func (client *SOCAlertClient) SendWarningAlert(title, message, details string) error {
	return client.sendAlert("warning", title, message, details)
}

func (client *SOCAlertClient) SendInfoAlert(title, message, details string) error {
	return client.sendAlert("info", title, message, details)
}

func (client *SOCAlertClient) SendAlert(severity, title, message, details string) error {
	return client.sendAlert(severity, title, message, details)
}

func (client *SOCAlertClient) SendUDPListenerFailureAlert(err error) error {
	return client.SendCriticalAlert(
		"UDP Listener Failure",
		"ByteFreezer Proxy UDP listener has failed",
		fmt.Sprintf("Error: %v", err),
	)
}

func (client *SOCAlertClient) SendReceiverForwardingFailureAlert(url string, err error) error {
	return client.SendWarningAlert(
		"Receiver Forwarding Failure",
		"Failed to forward data to ByteFreezer Receiver",
		fmt.Sprintf("URL: %s, Error: %v", url, err),
	)
}

func (client *SOCAlertClient) SendBatchProcessingFailureAlert(batchID string, err error) error {
	return client.SendWarningAlert(
		"Batch Processing Failure",
		"Failed to process UDP data batch",
		fmt.Sprintf("Batch ID: %s, Error: %v", batchID, err),
	)
}

func (client *SOCAlertClient) sendAlert(severity, title, message, details string) error {
	if !client.config.SOC.Enabled {
		if client.config.Dev {
			log.Infof("SOC Alert [%s]: %s - %s (%s)", severity, title, message, details)
		}
		return nil
	}

	if client.config.SOC.Endpoint == "" {
		return fmt.Errorf("SOC endpoint not configured")
	}

	payload := AlertPayload{
		Service:  client.config.App.Name,
		Version:  client.config.App.Version,
		Severity: severity,
		Title:    title,
		Message:  message,
		Details: map[string]interface{}{
			"details": details,
		},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal alert payload: %w", err)
	}

	timeout := time.Duration(client.config.SOC.Timeout) * time.Second
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	httpClient := &http.Client{
		Timeout: timeout,
	}

	req, err := http.NewRequest("POST", client.config.SOC.Endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create alert request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", fmt.Sprintf("%s/%s", client.config.App.Name, client.config.App.Version))

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send alert: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("SOC alert request failed with status %d", resp.StatusCode)
	}

	log.Debugf("SOC alert sent successfully: %s", title)
	return nil
}

package errors

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/n0needt0/go-goodies/log"
)

// ErrorReporter handles reporting errors to the control service (account-scoped for proxy)
type ErrorReporter struct {
	controlURL string
	apiKey     string // This is the account JWT token for proxy
	httpClient *http.Client
	component  string
	accountID  string
	enabled    bool
}

// ErrorReport represents an error to be reported
type ErrorReport struct {
	ErrorType    string                 `json:"error_type"`
	Component    string                 `json:"component"`
	TenantID     string                 `json:"tenant_id,omitempty"`
	DatasetID    string                 `json:"dataset_id,omitempty"`
	ErrorMessage string                 `json:"error_message"`
	ErrorSample  map[string]interface{} `json:"error_sample,omitempty"`
	Severity     string                 `json:"severity,omitempty"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
}

// NewErrorReporter creates a new error reporter for proxy (account-scoped)
func NewErrorReporter(controlURL, accountID, jwtToken, component string, enabled bool) *ErrorReporter {
	return &ErrorReporter{
		controlURL: controlURL,
		apiKey:     jwtToken,
		component:  component,
		accountID:  accountID,
		enabled:    enabled,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// ReportError reports an error to the control service (account-scoped endpoint)
func (er *ErrorReporter) ReportError(ctx context.Context, report ErrorReport) error {
	if !er.enabled {
		log.Debug("Error reporting is disabled, skipping report")
		return nil
	}

	// Set component from reporter if not specified
	if report.Component == "" {
		report.Component = er.component
	}

	// Default severity to "error" if not specified
	if report.Severity == "" {
		report.Severity = "error"
	}

	// Marshal the report
	body, err := json.Marshal(report)
	if err != nil {
		return fmt.Errorf("failed to marshal error report: %w", err)
	}

	// Create the request (account-scoped endpoint for proxy)
	url := fmt.Sprintf("%s/api/v1/accounts/%s/errors", er.controlURL, er.accountID)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("failed to create error report request: %w", err)
	}

	// Add headers (use JWT token for account-scoped auth)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", er.apiKey))

	// Send the request
	resp, err := er.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send error report: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("error report failed with status %d", resp.StatusCode)
	}

	log.Debugf("Error reported successfully (account: %s): type=%s, component=%s", er.accountID, report.ErrorType, report.Component)
	return nil
}

// ReportErrorSimple is a convenience method for reporting simple errors
func (er *ErrorReporter) ReportErrorSimple(ctx context.Context, errorType, errorMessage, severity string, tenantID, datasetID string) error {
	report := ErrorReport{
		ErrorType:    errorType,
		ErrorMessage: errorMessage,
		Severity:     severity,
		TenantID:     tenantID,
		DatasetID:    datasetID,
	}
	return er.ReportError(ctx, report)
}

// ReportErrorWithSample reports an error with a sample
func (er *ErrorReporter) ReportErrorWithSample(ctx context.Context, errorType, errorMessage, severity string, tenantID, datasetID string, sample map[string]interface{}) error {
	report := ErrorReport{
		ErrorType:    errorType,
		ErrorMessage: errorMessage,
		Severity:     severity,
		TenantID:     tenantID,
		DatasetID:    datasetID,
		ErrorSample:  sample,
	}
	return er.ReportError(ctx, report)
}

// ReportCritical reports a critical error
func (er *ErrorReporter) ReportCritical(ctx context.Context, errorType, errorMessage string, tenantID, datasetID string) error {
	return er.ReportErrorSimple(ctx, errorType, errorMessage, "critical", tenantID, datasetID)
}

// ReportWarning reports a warning
func (er *ErrorReporter) ReportWarning(ctx context.Context, errorType, errorMessage string, tenantID, datasetID string) error {
	return er.ReportErrorSimple(ctx, errorType, errorMessage, "warning", tenantID, datasetID)
}

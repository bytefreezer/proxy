package services

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// MetricsService handles OpenTelemetry metrics collection
type MetricsService struct {
	meter metric.Meter

	// Counters
	udpBytesReceived   metric.Int64Counter
	udpPacketsReceived metric.Int64Counter
	udpLinesReceived   metric.Int64Counter
	httpBytesForwarded metric.Int64Counter
	httpLinesForwarded metric.Int64Counter
	httpRequestsTotal  metric.Int64Counter

	// Gauges
	spoolQueueSize  metric.Int64UpDownCounter
	spoolQueueBytes metric.Int64UpDownCounter

	// Histograms
	batchSizeBytes  metric.Int64Histogram
	batchSizeLines  metric.Int64Histogram
	forwardDuration metric.Float64Histogram
}

// NewMetricsService creates a new metrics service
func NewMetricsService() (*MetricsService, error) {
	meter := otel.Meter("bytefreezer-proxy")

	// Create counters
	udpBytesReceived, err := meter.Int64Counter(
		"bytefreezer_proxy_udp_bytes_received_total",
		metric.WithDescription("Total bytes received via UDP by tenant and dataset"),
		metric.WithUnit("bytes"),
	)
	if err != nil {
		return nil, err
	}

	udpPacketsReceived, err := meter.Int64Counter(
		"bytefreezer_proxy_udp_packets_received_total",
		metric.WithDescription("Total UDP packets received by tenant and dataset"),
	)
	if err != nil {
		return nil, err
	}

	udpLinesReceived, err := meter.Int64Counter(
		"bytefreezer_proxy_udp_lines_received_total",
		metric.WithDescription("Total lines received via UDP by tenant and dataset"),
	)
	if err != nil {
		return nil, err
	}

	httpBytesForwarded, err := meter.Int64Counter(
		"bytefreezer_proxy_http_bytes_forwarded_total",
		metric.WithDescription("Total bytes forwarded via HTTP by tenant and dataset"),
		metric.WithUnit("bytes"),
	)
	if err != nil {
		return nil, err
	}

	httpLinesForwarded, err := meter.Int64Counter(
		"bytefreezer_proxy_http_lines_forwarded_total",
		metric.WithDescription("Total lines forwarded via HTTP by tenant and dataset"),
	)
	if err != nil {
		return nil, err
	}

	httpRequestsTotal, err := meter.Int64Counter(
		"bytefreezer_proxy_http_requests_total",
		metric.WithDescription("Total HTTP requests made by tenant, dataset, and status"),
	)
	if err != nil {
		return nil, err
	}

	// Create gauges (up/down counters)
	spoolQueueSize, err := meter.Int64UpDownCounter(
		"bytefreezer_proxy_spool_queue_size",
		metric.WithDescription("Current number of files in spool queue by tenant and dataset"),
	)
	if err != nil {
		return nil, err
	}

	spoolQueueBytes, err := meter.Int64UpDownCounter(
		"bytefreezer_proxy_spool_queue_bytes",
		metric.WithDescription("Current bytes in spool queue by tenant and dataset"),
		metric.WithUnit("bytes"),
	)
	if err != nil {
		return nil, err
	}

	// Create histograms
	batchSizeBytes, err := meter.Int64Histogram(
		"bytefreezer_proxy_batch_size_bytes",
		metric.WithDescription("Distribution of batch sizes in bytes"),
		metric.WithUnit("bytes"),
	)
	if err != nil {
		return nil, err
	}

	batchSizeLines, err := meter.Int64Histogram(
		"bytefreezer_proxy_batch_size_lines",
		metric.WithDescription("Distribution of batch sizes in lines"),
	)
	if err != nil {
		return nil, err
	}

	forwardDuration, err := meter.Float64Histogram(
		"bytefreezer_proxy_forward_duration_seconds",
		metric.WithDescription("Time taken to forward batches to receiver"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, err
	}

	return &MetricsService{
		meter:              meter,
		udpBytesReceived:   udpBytesReceived,
		udpPacketsReceived: udpPacketsReceived,
		udpLinesReceived:   udpLinesReceived,
		httpBytesForwarded: httpBytesForwarded,
		httpLinesForwarded: httpLinesForwarded,
		httpRequestsTotal:  httpRequestsTotal,
		spoolQueueSize:     spoolQueueSize,
		spoolQueueBytes:    spoolQueueBytes,
		batchSizeBytes:     batchSizeBytes,
		batchSizeLines:     batchSizeLines,
		forwardDuration:    forwardDuration,
	}, nil
}

// RecordUDPBytesReceived records bytes received via UDP
func (m *MetricsService) RecordUDPBytesReceived(ctx context.Context, tenantID, datasetID string, bytes int64) {
	m.udpBytesReceived.Add(ctx, bytes, metric.WithAttributes(
		attribute.String("tenant_id", tenantID),
		attribute.String("dataset_id", datasetID),
	))
}

// RecordUDPPacketsReceived records packets received via UDP
func (m *MetricsService) RecordUDPPacketsReceived(ctx context.Context, tenantID, datasetID string, count int64) {
	m.udpPacketsReceived.Add(ctx, count, metric.WithAttributes(
		attribute.String("tenant_id", tenantID),
		attribute.String("dataset_id", datasetID),
	))
}

// RecordUDPLinesReceived records lines received via UDP
func (m *MetricsService) RecordUDPLinesReceived(ctx context.Context, tenantID, datasetID string, lines int64) {
	m.udpLinesReceived.Add(ctx, lines, metric.WithAttributes(
		attribute.String("tenant_id", tenantID),
		attribute.String("dataset_id", datasetID),
	))
}

// RecordHTTPBytesForwarded records bytes forwarded via HTTP
func (m *MetricsService) RecordHTTPBytesForwarded(ctx context.Context, tenantID, datasetID string, bytes int64) {
	m.httpBytesForwarded.Add(ctx, bytes, metric.WithAttributes(
		attribute.String("tenant_id", tenantID),
		attribute.String("dataset_id", datasetID),
	))
}

// RecordHTTPLinesForwarded records lines forwarded via HTTP
func (m *MetricsService) RecordHTTPLinesForwarded(ctx context.Context, tenantID, datasetID string, lines int64) {
	m.httpLinesForwarded.Add(ctx, lines, metric.WithAttributes(
		attribute.String("tenant_id", tenantID),
		attribute.String("dataset_id", datasetID),
	))
}

// RecordHTTPRequest records HTTP request attempts
func (m *MetricsService) RecordHTTPRequest(ctx context.Context, tenantID, datasetID, status string) {
	m.httpRequestsTotal.Add(ctx, 1, metric.WithAttributes(
		attribute.String("tenant_id", tenantID),
		attribute.String("dataset_id", datasetID),
		attribute.String("status", status), // success, error, retry
	))
}

// RecordSpoolQueueSize records current spool queue size
func (m *MetricsService) RecordSpoolQueueSize(ctx context.Context, tenantID, datasetID string, size int64) {
	m.spoolQueueSize.Add(ctx, size, metric.WithAttributes(
		attribute.String("tenant_id", tenantID),
		attribute.String("dataset_id", datasetID),
	))
}

// RecordSpoolQueueBytes records current spool queue bytes
func (m *MetricsService) RecordSpoolQueueBytes(ctx context.Context, tenantID, datasetID string, bytes int64) {
	m.spoolQueueBytes.Add(ctx, bytes, metric.WithAttributes(
		attribute.String("tenant_id", tenantID),
		attribute.String("dataset_id", datasetID),
	))
}

// RecordBatchSize records batch size metrics
func (m *MetricsService) RecordBatchSize(ctx context.Context, tenantID, datasetID string, bytes, lines int64) {
	attrs := metric.WithAttributes(
		attribute.String("tenant_id", tenantID),
		attribute.String("dataset_id", datasetID),
	)

	m.batchSizeBytes.Record(ctx, bytes, attrs)
	m.batchSizeLines.Record(ctx, lines, attrs)
}

// RecordForwardDuration records time taken to forward a batch
func (m *MetricsService) RecordForwardDuration(ctx context.Context, tenantID, datasetID string, durationSeconds float64) {
	m.forwardDuration.Record(ctx, durationSeconds, metric.WithAttributes(
		attribute.String("tenant_id", tenantID),
		attribute.String("dataset_id", datasetID),
	))
}

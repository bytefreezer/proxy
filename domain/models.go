package domain

import (
	"time"
)

// UDPMessage represents a single UDP message received
type UDPMessage struct {
	Data      []byte
	From      string
	Timestamp time.Time
	TenantID  string
	DatasetID string
}

// DataBatch represents a batch of UDP messages ready for forwarding
type DataBatch struct {
	ID           string
	TenantID     string
	DatasetID    string
	Messages     []UDPMessage
	LineCount    int
	TotalBytes   int64
	CreatedAt    time.Time
	CompressedAt time.Time
	Data         []byte // Compressed NDJSON data
}

// ProxyStats represents proxy processing statistics
type ProxyStats struct {
	UDPMessagesReceived int64
	UDPMessageErrors    int64
	BatchesCreated      int64
	BatchesForwarded    int64
	ForwardingErrors    int64
	BytesReceived       int64
	BytesForwarded      int64
	LastActivity        time.Time
	UptimeSeconds       int64
}

// ReceiverConfig represents configuration for forwarding to bytefreezer-receiver
type ReceiverConfig struct {
	BaseURL   string
	TenantID  string
	DatasetID string
	Timeout   time.Duration
	RetryCount int
}

// UDPConfig represents UDP listener configuration
type UDPConfig struct {
	Host              string
	Port              int
	ReadBufferSize    int
	MaxBatchLines     int
	MaxBatchBytes     int64
	BatchTimeoutSec   int
	CompressionLevel  int
	EnableCompression bool
}
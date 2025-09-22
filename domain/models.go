package domain

import (
	"time"
)

// UDPMessage represents a single UDP message received
type UDPMessage struct {
	Data        []byte
	From        string
	Timestamp   time.Time
	TenantID    string
	DatasetID   string
	BearerToken string // Authentication token for this message's tenant
}

// DataBatch represents a batch of UDP messages ready for forwarding
type DataBatch struct {
	ID            string
	TenantID      string
	DatasetID     string
	BearerToken   string // Authentication token for this tenant
	FileExtension string // Plugin-defined file extension
	Messages      []UDPMessage
	LineCount     int
	TotalBytes    int64
	CreatedAt     time.Time
	CompressedAt  time.Time
	Data          []byte // Compressed NDJSON data
	TriggerReason string // Reason batch was finalized: "timeout", "size_limit_reached", "service_shutdown"
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

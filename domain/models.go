// Licensed under Elastic License 2.0
// See LICENSE.txt for details

package domain

import (
	"sync"
	"sync/atomic"
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
	DataHint      string // Data format hint for downstream processing
	Filename      string // Original filename for proxy consistency
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
	ForwardingErrors    int64 // Total cumulative errors (deprecated, use ForwardingErrors24h)
	BytesReceived       int64
	BytesForwarded      int64
	LastActivity        time.Time
	UptimeSeconds       int64

	// Rolling window error tracking
	errorTimestamps   []time.Time
	errorTimestampsMu sync.Mutex
}

// AddForwardingError records a forwarding error with timestamp
func (s *ProxyStats) AddForwardingError() {
	s.errorTimestampsMu.Lock()
	defer s.errorTimestampsMu.Unlock()

	now := time.Now()
	s.errorTimestamps = append(s.errorTimestamps, now)

	// Also increment legacy counter for backwards compatibility
	atomic.AddInt64(&s.ForwardingErrors, 1)

	// Prune old entries (older than 24h) to prevent memory growth
	cutoff := now.Add(-24 * time.Hour)
	pruneIdx := 0
	for i, t := range s.errorTimestamps {
		if t.After(cutoff) {
			pruneIdx = i
			break
		}
		pruneIdx = i + 1
	}
	if pruneIdx > 0 {
		s.errorTimestamps = s.errorTimestamps[pruneIdx:]
	}
}

// ForwardingErrors24h returns the count of forwarding errors in the last 24 hours
func (s *ProxyStats) ForwardingErrors24h() int64 {
	s.errorTimestampsMu.Lock()
	defer s.errorTimestampsMu.Unlock()

	cutoff := time.Now().Add(-24 * time.Hour)
	var count int64
	for _, t := range s.errorTimestamps {
		if t.After(cutoff) {
			count++
		}
	}
	return count
}

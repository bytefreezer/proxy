package udp

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/n0needt0/bytefreezer-proxy/config"
	"github.com/n0needt0/bytefreezer-proxy/domain"
	"github.com/n0needt0/bytefreezer-proxy/services"
	"github.com/n0needt0/go-goodies/log"
)

// Listener represents a UDP listener that collects data and forwards to bytefreezer-receiver
type Listener struct {
	services     *services.Services
	config       *config.Config
	listeners    []*UDPPortListener
	quit         chan struct{}
	batchChannel chan *domain.UDPMessage
	bufferPool   sync.Pool
	stopOnce     sync.Once
	wg           sync.WaitGroup
	forwarder    *Forwarder
}

// UDPPortListener represents a single UDP port listener
type UDPPortListener struct {
	port      int
	tenantID  string
	datasetID string
	addr      *net.UDPAddr
	conn      *net.UDPConn
}

// NewListener creates a new UDP listener
func NewListener(services *services.Services, cfg *config.Config) *Listener {
	var portListeners []*UDPPortListener

	// Create listeners for each configured port
	for _, udpListener := range cfg.UDP.Listeners {
		tenantID := udpListener.TenantID
		if tenantID == "" {
			tenantID = cfg.TenantID // Use global tenant if not specified
		}

		portListener := &UDPPortListener{
			port:      udpListener.Port,
			tenantID:  tenantID,
			datasetID: udpListener.DatasetID,
			addr: &net.UDPAddr{
				IP:   net.ParseIP(cfg.UDP.Host),
				Port: udpListener.Port,
			},
		}

		// Debug log to verify values are set
		log.Debugf("Created port listener - Port: %d, TenantID: '%s', DatasetID: '%s'",
			portListener.port, portListener.tenantID, portListener.datasetID)
		portListeners = append(portListeners, portListener)
	}

	return &Listener{
		services:     services,
		config:       cfg,
		listeners:    portListeners,
		quit:         make(chan struct{}),
		batchChannel: make(chan *domain.UDPMessage, 1000), // Buffer for incoming messages
		bufferPool: sync.Pool{
			New: func() interface{} {
				return make([]byte, cfg.UDP.ReadBufferSizeBytes)
			},
		},
		forwarder: NewForwarder(services, cfg),
	}
}

// Start starts the UDP listener
func (l *Listener) Start() error {
	if !l.config.UDP.Enabled {
		log.Info("UDP listener is disabled")
		return nil
	}

	if len(l.listeners) == 0 {
		log.Info("No UDP listeners configured")
		return nil
	}

	// Start listeners for each port
	for _, portListener := range l.listeners {
		var err error
		portListener.conn, err = net.ListenUDP("udp", portListener.addr)
		if err != nil {
			// Clean up any already started listeners
			l.Stop()
			return fmt.Errorf("failed to listen on UDP %s: %w", portListener.addr.String(), err)
		}

		if err := portListener.conn.SetReadBuffer(l.config.UDP.ReadBufferSizeBytes); err != nil {
			portListener.conn.Close()
			l.Stop()
			return fmt.Errorf("failed to set read buffer for %s: %w", portListener.addr.String(), err)
		}

		log.Info("UDP server listening on " + portListener.addr.IP.String() + ":" +
			fmt.Sprintf("%d", portListener.addr.Port) + " (tenant: " + portListener.tenantID +
			", dataset: " + portListener.datasetID + ")")

		// Start message handler for this port
		l.wg.Add(1)
		go func(pl *UDPPortListener) {
			defer l.wg.Done()
			l.handleMessagesForPort(pl)
		}(portListener)
	}

	// Start the forwarder
	l.wg.Add(1)
	go func() {
		defer l.wg.Done()
		l.forwarder.Start(l.batchChannel)
	}()

	return nil
}

// Stop stops the UDP listener
func (l *Listener) Stop() error {
	log.Info("UDP listener shutting down")

	l.stopOnce.Do(func() {
		close(l.quit)

		// Close all port listeners
		for _, portListener := range l.listeners {
			if portListener.conn != nil {
				portListener.conn.Close()
			}
		}

		// Stop the forwarder
		if l.forwarder != nil {
			l.forwarder.Stop()
		}
	})

	l.wg.Wait()
	log.Info("UDP listener shut down gracefully")
	return nil
}

// handleMessagesForPort handles incoming UDP messages for a specific port
func (l *Listener) handleMessagesForPort(portListener *UDPPortListener) {
	for {
		select {
		case <-l.quit:
			return
		default:
		}

		// Set read timeout
		portListener.conn.SetReadDeadline(time.Now().Add(1 * time.Second))

		buf := l.allocateBuffer()
		readLen, remoteAddr, err := portListener.conn.ReadFromUDP(buf)

		if err != nil {
			l.deallocateBuffer(buf)

			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				// Timeout is expected, continue
				continue
			}

			if l.isClosedConnError(err) {
				// Normal shutdown
				return
			}

			log.Errorf("UDP read error on port %d: %v", portListener.port, err)
			l.services.ProxyStats.UDPMessageErrors++

			// Send SOC alert for persistent errors
			if l.config.SOCAlertClient != nil {
				l.config.SOCAlertClient.SendUDPListenerFailureAlert(err)
			}
			continue
		}

		// Process the message with port-specific tenant/dataset info
		l.processMessageWithContext(buf[:readLen], remoteAddr, portListener.tenantID, portListener.datasetID)
		l.deallocateBuffer(buf)
	}
}

// processMessageWithContext processes a single UDP message with tenant/dataset context
func (l *Listener) processMessageWithContext(data []byte, from *net.UDPAddr, tenantID, datasetID string) {
	// Clean up the payload
	payload := bytes.TrimSpace(data)
	payload = bytes.Trim(payload, "\x08\x00")

	if len(payload) == 0 {
		return
	}

	// Create UDP message with context
	msg := &domain.UDPMessage{
		Data:      make([]byte, len(payload)),
		From:      from.String(),
		Timestamp: time.Now(),
		TenantID:  tenantID,
		DatasetID: datasetID,
	}
	copy(msg.Data, payload)

	// Try to send to batch channel (non-blocking)
	select {
	case l.batchChannel <- msg:
		l.services.ProxyStats.UDPMessagesReceived++
		l.services.ProxyStats.BytesReceived += int64(len(payload))
		l.services.ProxyStats.LastActivity = time.Now()
	default:
		// Channel is full, drop message and log
		log.Warnf("UDP message channel full, dropping message from %s", from)
		l.services.ProxyStats.UDPMessageErrors++
	}
}

// allocateBuffer gets a buffer from the pool
func (l *Listener) allocateBuffer() []byte {
	return l.bufferPool.Get().([]byte)
}

// deallocateBuffer returns a buffer to the pool
func (l *Listener) deallocateBuffer(buf []byte) {
	// Ignore SA6002: sync.Pool.Put expects the same interface type that New() returns
	//lint:ignore SA6002 sync.Pool requires putting back the same type that New() returns
	l.bufferPool.Put(buf)
}

// isClosedConnError checks if the error is due to closed connection
func (l *Listener) isClosedConnError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, net.ErrClosed) {
		return true
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) && opErr.Err != nil {
		return strings.Contains(opErr.Err.Error(), "use of closed network connection")
	}
	return strings.Contains(err.Error(), "use of closed network connection")
}

// Forwarder handles batching and forwarding data to bytefreezer-receiver
type Forwarder struct {
	services *services.Services
	config   *config.Config
	quit     chan struct{}
}

// NewForwarder creates a new forwarder
func NewForwarder(services *services.Services, cfg *config.Config) *Forwarder {
	return &Forwarder{
		services: services,
		config:   cfg,
		quit:     make(chan struct{}),
	}
}

// Start starts the forwarder
func (f *Forwarder) Start(messageChannel <-chan *domain.UDPMessage) {
	// Track batches by tenant+dataset combination
	batches := make(map[string]*domain.DataBatch)

	batchTimer := time.NewTimer(f.config.GetBatchTimeout())
	defer batchTimer.Stop()

	for {
		select {
		case <-f.quit:
			// Send all remaining batches
			for _, batch := range batches {
				if len(batch.Messages) > 0 {
					f.sendBatch(batch)
				}
			}
			return

		case msg, ok := <-messageChannel:
			if !ok {
				// Channel closed, send all remaining batches
				for _, batch := range batches {
					if len(batch.Messages) > 0 {
						f.sendBatch(batch)
					}
				}
				return
			}

			// Create batch key from tenant+dataset
			batchKey := fmt.Sprintf("%s:%s", msg.TenantID, msg.DatasetID)

			// Get or create batch for this tenant+dataset
			batch, exists := batches[batchKey]
			if !exists {
				batch = &domain.DataBatch{
					ID:        fmt.Sprintf("%d_%s", time.Now().UnixNano(), batchKey),
					TenantID:  msg.TenantID,
					DatasetID: msg.DatasetID,
					Messages:  make([]domain.UDPMessage, 0),
					CreatedAt: time.Now(),
				}
				batches[batchKey] = batch
			}

			// Add message to batch
			batch.Messages = append(batch.Messages, *msg)
			batch.LineCount++
			batch.TotalBytes += int64(len(msg.Data))

			// Check if batch is ready to send
			shouldSend := false
			if f.config.UDP.MaxBatchLines > 0 && batch.LineCount >= f.config.UDP.MaxBatchLines {
				shouldSend = true
			}
			if f.config.UDP.MaxBatchBytes > 0 && batch.TotalBytes >= f.config.UDP.MaxBatchBytes {
				shouldSend = true
			}

			if shouldSend {
				f.sendBatch(batch)
				delete(batches, batchKey)

				// Reset timer since we sent a batch
				batchTimer.Stop()
				batchTimer.Reset(f.config.GetBatchTimeout())
			}

		case <-batchTimer.C:
			// Timeout reached, send all non-empty batches
			for batchKey, batch := range batches {
				if len(batch.Messages) > 0 {
					f.sendBatch(batch)
					delete(batches, batchKey)
				}
			}

			// Reset timer
			batchTimer.Reset(f.config.GetBatchTimeout())
		}
	}
}

// Stop stops the forwarder
func (f *Forwarder) Stop() {
	close(f.quit)
}

// sendBatch sends a batch to bytefreezer-receiver
func (f *Forwarder) sendBatch(batch *domain.DataBatch) {
	// Convert messages to NDJSON
	var ndjsonData bytes.Buffer
	for _, msg := range batch.Messages {
		// Try to parse as JSON first
		var jsonObj interface{}
		if err := json.Unmarshal(msg.Data, &jsonObj); err == nil {
			// Valid JSON, marshal it to ensure consistent formatting
			if jsonBytes, err := json.Marshal(jsonObj); err == nil {
				ndjsonData.Write(jsonBytes)
				ndjsonData.WriteByte('\n')
			} else {
				// Fallback to raw data
				ndjsonData.Write(msg.Data)
				ndjsonData.WriteByte('\n')
			}
		} else {
			// Not valid JSON, create a JSON envelope
			envelope := map[string]interface{}{
				"message":   string(msg.Data),
				"source":    msg.From,
				"timestamp": msg.Timestamp.Format(time.RFC3339Nano),
			}
			if jsonBytes, err := json.Marshal(envelope); err == nil {
				ndjsonData.Write(jsonBytes)
				ndjsonData.WriteByte('\n')
			}
		}
	}

	// Compress if enabled
	var finalData []byte
	if f.config.UDP.EnableCompression {
		var compressed bytes.Buffer
		gzipWriter, err := gzip.NewWriterLevel(&compressed, f.config.UDP.CompressionLevel)
		if err != nil {
			log.Errorf("Failed to create gzip writer: %v", err)
			f.services.ProxyStats.ForwardingErrors++
			return
		}

		if _, err := gzipWriter.Write(ndjsonData.Bytes()); err != nil {
			log.Errorf("Failed to compress data: %v", err)
			f.services.ProxyStats.ForwardingErrors++
			return
		}

		if err := gzipWriter.Close(); err != nil {
			log.Errorf("Failed to close gzip writer: %v", err)
			f.services.ProxyStats.ForwardingErrors++
			return
		}

		finalData = compressed.Bytes()
		batch.CompressedAt = time.Now()
	} else {
		finalData = ndjsonData.Bytes()
	}

	batch.Data = finalData

	// Send to bytefreezer-receiver
	err := f.sendToReceiver(batch)
	if err != nil {
		log.Errorf("Failed to send batch %s to receiver: %v", batch.ID, err)
		f.services.ProxyStats.ForwardingErrors++

		// Spool the failed batch using the correct tenant/dataset from the batch
		if f.services.SpoolingService != nil {
			if spoolErr := f.services.SpoolingService.SpoolData(batch.TenantID, batch.DatasetID, finalData, err.Error()); spoolErr != nil {
				log.Errorf("Failed to spool batch %s: %v", batch.ID, spoolErr)
			} else {
				log.Debugf("Spooled failed batch %s for tenant=%s, dataset=%s", batch.ID, batch.TenantID, batch.DatasetID)
			}
		}

		// Send SOC alert
		if f.config.SOCAlertClient != nil {
			f.config.SOCAlertClient.SendReceiverForwardingFailureAlert(f.config.Receiver.BaseURL, err)
		}
	} else {
		f.services.ProxyStats.BatchesForwarded++
		f.services.ProxyStats.BytesForwarded += int64(len(finalData))
		log.Debugf("Successfully sent batch %s (%d messages, %d bytes)", batch.ID, batch.LineCount, len(finalData))
	}

	f.services.ProxyStats.BatchesCreated++
}

// sendToReceiver sends the batch to bytefreezer-receiver
func (f *Forwarder) sendToReceiver(batch *domain.DataBatch) error {
	// Use HTTP forwarder from services
	forwarder := services.NewHTTPForwarder(f.config)
	return forwarder.ForwardBatch(batch)
}

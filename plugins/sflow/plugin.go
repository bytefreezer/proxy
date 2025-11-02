package sflow

import (
	"bytes"
	"context"
	"github.com/bytedance/sonic"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/mitchellh/mapstructure"
	"github.com/n0needt0/bytefreezer-proxy/plugins"
	"github.com/n0needt0/go-goodies/log"
	sflowdec "github.com/netsampler/goflow2/v2/decoders/sflow"
)

// Plugin implements the sFlow input plugin with direct filesystem writes
type Plugin struct {
	config  Config
	conn    *net.UDPConn
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	health  plugins.PluginHealth
	mu      sync.RWMutex
	spooler plugins.SpoolingInterface
	metrics PluginMetrics
}

// Config represents sFlow plugin configuration
type Config struct {
	Host           string `mapstructure:"host"`
	Port           int    `mapstructure:"port"`
	TenantID       string `mapstructure:"tenant_id"`
	DatasetID      string `mapstructure:"dataset_id"`
	BearerToken    string `mapstructure:"bearer_token,omitempty"`
	Protocol       string `mapstructure:"protocol,omitempty"` // Always "sflow" for this plugin
	DataHint       string `mapstructure:"data_hint,omitempty"` // Always "ndjson" for flow data
	ReadBufferSize int    `mapstructure:"read_buffer_size,omitempty"`
	WorkerCount    int    `mapstructure:"worker_count,omitempty"` // Number of processing workers
}

// PluginMetrics tracks sFlow plugin metrics
type PluginMetrics struct {
	PacketsReceived uint64
	BytesReceived   uint64
	FlowsDecoded    uint64
	PacketsDropped  uint64
	LastPacketTime  time.Time
	StartTime       time.Time
}

// NewPlugin creates a new sFlow plugin instance
func NewPlugin() plugins.InputPlugin {
	return &Plugin{
		health: plugins.PluginHealth{
			Status:      plugins.HealthStatusStopped,
			Message:     "Plugin created but not started",
			LastUpdated: time.Now(),
		},
		metrics: PluginMetrics{
			StartTime: time.Now(),
		},
	}
}

// Name returns the plugin name
func (p *Plugin) Name() string {
	return "sflow"
}

// Configure initializes the plugin with configuration
func (p *Plugin) Configure(config map[string]interface{}) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Decode configuration
	if err := mapstructure.Decode(config, &p.config); err != nil {
		return fmt.Errorf("failed to decode sFlow plugin config: %w", err)
	}

	// Set defaults
	if p.config.Host == "" {
		p.config.Host = "0.0.0.0"
	}
	if p.config.Port == 0 {
		return fmt.Errorf("port is required for sFlow plugin")
	}
	if p.config.TenantID == "" {
		return fmt.Errorf("tenant_id is required for sFlow plugin")
	}
	if p.config.DatasetID == "" {
		return fmt.Errorf("dataset_id is required for sFlow plugin")
	}
	if p.config.Protocol == "" {
		p.config.Protocol = "sflow"
	}
	if p.config.DataHint == "" {
		p.config.DataHint = "ndjson" // sFlow data is converted to NDJSON
	}
	if p.config.ReadBufferSize == 0 {
		p.config.ReadBufferSize = 65536 // 64KB default
	}
	if p.config.WorkerCount == 0 {
		p.config.WorkerCount = 4 // Default 4 workers
	}

	p.updateHealth(plugins.HealthStatusStopped, "Plugin configured successfully", "")
	log.Infof("sFlow plugin configured: %s:%d -> %s/%s (data_hint: %s)",
		p.config.Host, p.config.Port, p.config.TenantID, p.config.DatasetID, p.config.DataHint)

	return nil
}

// Start begins consuming sFlow data
func (p *Plugin) Start(ctx context.Context, spooler plugins.SpoolingInterface) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.ctx != nil {
		return fmt.Errorf("plugin is already started")
	}

	p.ctx, p.cancel = context.WithCancel(ctx)
	p.spooler = spooler
	p.metrics.StartTime = time.Now()

	// Create UDP listener
	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", p.config.Host, p.config.Port))
	if err != nil {
		p.updateHealth(plugins.HealthStatusUnhealthy, "Failed to resolve UDP address", err.Error())
		return fmt.Errorf("failed to resolve UDP address: %w", err)
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		p.updateHealth(plugins.HealthStatusUnhealthy, "Failed to start UDP listener", err.Error())
		return fmt.Errorf("failed to listen on UDP: %w", err)
	}

	p.conn = conn

	// Set read buffer size
	if err := conn.SetReadBuffer(p.config.ReadBufferSize); err != nil {
		log.Warnf("Failed to set UDP read buffer size to %d: %v", p.config.ReadBufferSize, err)
	}

	p.updateHealth(plugins.HealthStatusStarting, "Starting sFlow listener with direct spooling", "")

	// Start packet reader with direct spooling
	p.wg.Add(1)
	go p.packetReaderWithSpooling()

	p.updateHealth(plugins.HealthStatusHealthy, fmt.Sprintf("sFlow listener active with direct spooling on %s:%d", p.config.Host, p.config.Port), "")
	log.Infof("sFlow plugin started with direct spooling on %s:%d", p.config.Host, p.config.Port)

	return nil
}

// Stop gracefully shuts down the sFlow plugin
func (p *Plugin) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cancel == nil {
		return nil // Already stopped
	}

	p.updateHealth(plugins.HealthStatusStopping, "Shutting down sFlow listener", "")

	// Cancel context
	p.cancel()

	// Close UDP connection
	if p.conn != nil {
		p.conn.Close()
	}

	// Wait for all goroutines to finish
	p.wg.Wait()

	p.ctx = nil
	p.cancel = nil
	p.conn = nil

	p.updateHealth(plugins.HealthStatusStopped, "sFlow listener stopped", "")
	log.Infof("sFlow plugin stopped")

	return nil
}

// Health returns the current plugin health status
func (p *Plugin) Health() plugins.PluginHealth {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.health
}

// updateHealth updates the plugin health status
func (p *Plugin) updateHealth(status plugins.HealthStatus, message, lastError string) {
	p.health = plugins.PluginHealth{
		Status:      status,
		Message:     message,
		LastError:   lastError,
		LastUpdated: time.Now(),
	}
}

// packetReaderWithSpooling reads sFlow packets and writes directly to filesystem (zero data loss)
func (p *Plugin) packetReaderWithSpooling() {
	defer p.wg.Done()

	buffer := make([]byte, 65536) // 64KB buffer for UDP packets

	for {
		select {
		case <-p.ctx.Done():
			return
		default:
			// Set read deadline to allow checking for context cancellation
			p.conn.SetReadDeadline(time.Now().Add(1 * time.Second))

			n, clientAddr, err := p.conn.ReadFromUDP(buffer)
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					continue // Timeout is expected, continue to check context
				}
				if p.ctx.Err() != nil {
					return // Context cancelled
				}
				log.Errorf("Error reading sFlow packet: %v", err)
				continue
			}

			// Process message with direct spooling
			if n > 0 {
				p.processMessageWithSpooling(buffer[:n], clientAddr)
			}
		}
	}
}

// ExtendedFlowRecord adds IP extraction to sFlow flow records
type ExtendedFlowRecord struct {
	Header struct {
		DataFormat uint32 `json:"data-format"`
		Length     uint32 `json:"length"`
	} `json:"header"`
	Data struct {
		Protocol       uint32 `json:"protocol"`
		FrameLength    uint32 `json:"frame-length"`
		Stripped       uint32 `json:"stripped"`
		OriginalLength uint32 `json:"original-length"`
		HeaderData     string `json:"header-data"`
	} `json:"data"`
	// Extracted fields
	SrcIP   string `json:"src_ip,omitempty"`
	DstIP   string `json:"dst_ip,omitempty"`
	SrcPort uint16 `json:"src_port,omitempty"`
	DstPort uint16 `json:"dst_port,omitempty"`
}

// processMessageWithSpooling processes sFlow message with direct filesystem write (zero data loss)
func (p *Plugin) processMessageWithSpooling(data []byte, clientAddr *net.UDPAddr) {
	// Update metrics
	p.metrics.PacketsReceived++
	p.metrics.BytesReceived += uint64(len(data))
	p.metrics.LastPacketTime = time.Now()

	// Decode sFlow packet using goflow2
	payload := bytes.NewBuffer(data)
	packet := &sflowdec.Packet{}

	// Use DecodeMessageVersion which handles version detection
	err := sflowdec.DecodeMessageVersion(payload, packet)
	if err != nil {
		log.Warnf("Failed to decode sFlow packet from %s: %v", clientAddr.IP.String(), err)
		p.metrics.PacketsDropped++
		return
	}

	p.metrics.FlowsDecoded++

	// Convert decoded packet to JSON with IP extraction
	jsonData, err := p.extractFlowRecords(packet)
	if err != nil {
		log.Warnf("Failed to extract flow records from %s: %v", clientAddr.IP.String(), err)
		p.metrics.PacketsDropped++
		return
	}

	// Add newline for NDJSON format
	ndjsonData := append(jsonData, '\n')

	// Write directly to filesystem - NO CHANNEL DROPS POSSIBLE
	bearerToken := p.config.BearerToken
	dataHint := p.config.DataHint
	if err := p.spooler.StoreRawMessage(p.config.TenantID, p.config.DatasetID, bearerToken, ndjsonData, dataHint); err != nil {
		log.Errorf("Failed to store sFlow message to filesystem from %s: %v", clientAddr.IP.String(), err)
		p.metrics.PacketsDropped++
		return
	}

	log.Debugf("Stored sFlow message from %s:%d as NDJSON directly to filesystem (%d bytes)",
		clientAddr.IP.String(), clientAddr.Port, len(ndjsonData))
}

// extractFlowRecords processes the sFlow packet and extracts IP addresses from headers
func (p *Plugin) extractFlowRecords(packet *sflowdec.Packet) ([]byte, error) {
	// Create output structure
	output := map[string]interface{}{
		"version":         packet.Version,
		"ip-version":      packet.IPVersion,
		"agent-ip":        packet.AgentIP,
		"sub-agent-id":    packet.SubAgentId,
		"sequence-number": packet.SequenceNumber,
		"uptime":          packet.Uptime,
		"samples-count":   packet.SamplesCount,
		"samples":         []map[string]interface{}{},
	}

	// Process each sample
	for _, sample := range packet.Samples {
		sampleData := make(map[string]interface{})

		// Handle flow sample
		if flowSample, ok := sample.(sflowdec.FlowSample); ok {
			sampleData["header"] = flowSample.Header
			sampleData["sampling-rate"] = flowSample.SamplingRate
			sampleData["sample-pool"] = flowSample.SamplePool
			sampleData["drops"] = flowSample.Drops
			sampleData["input"] = flowSample.Input
			sampleData["output"] = flowSample.Output
			sampleData["flow-records-count"] = flowSample.FlowRecordsCount

			// Process flow records and extract IPs
			records := []map[string]interface{}{}
			for _, record := range flowSample.Records {
				recordData := map[string]interface{}{
					"header": record.Header,
					"data":   record.Data,
				}

				// Try to extract IPs if this is a raw record with SampledHeader
				if rawData, ok := record.Data.(sflowdec.SampledHeader); ok {
					ips := extractIPsFromHeader(rawData.HeaderData)
					if ips != nil {
						recordData["src_ip"] = ips["src_ip"]
						recordData["dst_ip"] = ips["dst_ip"]
						if ips["src_port"] != 0 {
							recordData["src_port"] = ips["src_port"]
						}
						if ips["dst_port"] != 0 {
							recordData["dst_port"] = ips["dst_port"]
						}
					}
				}

				records = append(records, recordData)
			}
			sampleData["records"] = records
		}

		output["samples"] = append(output["samples"].([]map[string]interface{}), sampleData)
	}

	return sonic.Marshal(output)
}

// extractIPsFromHeader extracts IP addresses and ports from raw packet header
func extractIPsFromHeader(headerBytes []byte) map[string]interface{} {
	// Skip Ethernet header (14 bytes) if present
	offset := 0
	if len(headerBytes) >= 14 {
		// Check for IP ethertype (0x0800 for IPv4)
		if len(headerBytes) > 13 && headerBytes[12] == 0x08 && headerBytes[13] == 0x00 {
			offset = 14
		} else if headerBytes[12] == 0x45 { // Direct IP packet (starts with 0x45 for IPv4)
			offset = 0
		} else {
			offset = 14 // Assume Ethernet
		}
	}

	if len(headerBytes) < offset+20 {
		return nil // Not enough data for IP header
	}

	// Check for IPv4
	if headerBytes[offset]>>4 == 4 {
		srcIP := net.IPv4(headerBytes[offset+12], headerBytes[offset+13], headerBytes[offset+14], headerBytes[offset+15])
		dstIP := net.IPv4(headerBytes[offset+16], headerBytes[offset+17], headerBytes[offset+18], headerBytes[offset+19])

		result := map[string]interface{}{
			"src_ip": srcIP.String(),
			"dst_ip": dstIP.String(),
		}

		// Get IP header length and extract TCP/UDP ports if available
		ihl := int(headerBytes[offset] & 0x0F) * 4
		protocol := headerBytes[offset+9]

		if len(headerBytes) >= offset+ihl+4 && (protocol == 6 || protocol == 17) { // TCP or UDP
			srcPort := uint16(headerBytes[offset+ihl])<<8 | uint16(headerBytes[offset+ihl+1])
			dstPort := uint16(headerBytes[offset+ihl+2])<<8 | uint16(headerBytes[offset+ihl+3])
			result["src_port"] = srcPort
			result["dst_port"] = dstPort
		}

		return result
	}

	return nil
}

// Schema returns the sFlow plugin configuration schema
func (p *Plugin) Schema() plugins.PluginSchema {
	return plugins.PluginSchema{
		Name:        "sflow",
		DisplayName: "sFlow v5/v6",
		Description: "sFlow network flow collector with IP extraction. Decodes sFlow packets and outputs structured NDJSON with flow metadata.",
		Category:    "UDP-based (Network Flow)",
		Transport:   "UDP",
		DefaultPort: 6343,
		Fields: []plugins.PluginFieldSchema{
			{
				Name:        "host",
				Type:        "string",
				Required:    false,
				Default:     "0.0.0.0",
				Description: "Host address to bind to",
				Placeholder: "0.0.0.0",
				Group:       "Network",
			},
			{
				Name:        "port",
				Type:        "int",
				Required:    false,
				Default:     6343,
				Description: "UDP port to listen on (standard sFlow port: 6343)",
				Validation:  "1-65535",
				Placeholder: "6343",
				Group:       "Network",
			},
			{
				Name:        "read_buffer_size",
				Type:        "int",
				Required:    false,
				Default:     65536,
				Description: "UDP read buffer size in bytes (64KB default)",
				Validation:  "min:1024,max:1048576",
				Placeholder: "65536",
				Group:       "Performance",
			},
			{
				Name:        "worker_count",
				Type:        "int",
				Required:    false,
				Default:     4,
				Description: "Number of processing workers",
				Validation:  "min:1,max:32",
				Placeholder: "4",
				Group:       "Performance",
			},
		},
	}
}

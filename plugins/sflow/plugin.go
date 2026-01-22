// Licensed under Elastic License 2.0
// See LICENSE.txt for details

package sflow

import (
	"bytes"
	"context"
	"fmt"
	"github.com/bytedance/sonic"
	"net"
	"sync"
	"time"

	"github.com/bytefreezer/goodies/log"
	"github.com/bytefreezer/proxy/plugins"
	"github.com/mitchellh/mapstructure"
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
	Protocol       string `mapstructure:"protocol,omitempty"`  // Always "sflow" for this plugin
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
		p.config.ReadBufferSize = 8388608 // 8MB default for burst handling
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

	// Set read buffer size with verification and report if limited
	bufferResult := plugins.SetUDPReadBufferWithCheck(conn, p.config.ReadBufferSize)
	if bufferResult.Limited && bufferResult.Warning != "" {
		metadata := map[string]interface{}{
			"requested_size": bufferResult.RequestedSize,
			"actual_size":    bufferResult.ActualSize,
			"sysctl_cmd":     bufferResult.SysctlCmd,
		}
		spooler.ReportWarningWithMetadata(p.config.TenantID, p.config.DatasetID, "udp_buffer_limited", bufferResult.Warning, metadata)
	} else {
		// Buffer is OK - resolve any previous warnings
		spooler.ResolveWarning(p.config.DatasetID, "udp_buffer_limited")
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

	// Convert decoded packet to flat NDJSON records with IP extraction
	ndjsonData, err := p.extractFlowRecords(packet)
	if err != nil {
		log.Warnf("Failed to extract flow records from %s: %v", clientAddr.IP.String(), err)
		p.metrics.PacketsDropped++
		return
	}

	if len(ndjsonData) == 0 {
		return // No data to store
	}

	// Write directly to filesystem - NO CHANNEL DROPS POSSIBLE
	bearerToken := p.config.BearerToken
	dataHint := p.config.DataHint
	if err := p.spooler.StoreRawMessage(p.config.TenantID, p.config.DatasetID, bearerToken, ndjsonData, dataHint); err != nil {
		log.Errorf("Failed to store sFlow message to filesystem from %s: %v", clientAddr.IP.String(), err)
		p.metrics.PacketsDropped++
		return
	}

	log.Debugf("Stored sFlow flows from %s:%d as NDJSON directly to filesystem (%d bytes)",
		clientAddr.IP.String(), clientAddr.Port, len(ndjsonData))
}

// extractFlowRecords processes the sFlow packet and outputs flat NDJSON records
// Each flow record with extracted IPs becomes its own line for better queryability
func (p *Plugin) extractFlowRecords(packet *sflowdec.Packet) ([]byte, error) {
	var result []byte
	flowCount := 0

	// Process each sample
	for _, sample := range packet.Samples {
		// Handle flow sample
		if flowSample, ok := sample.(sflowdec.FlowSample); ok {
			for _, record := range flowSample.Records {
				// Create flat record for each flow
				flowRecord := map[string]interface{}{
					"version":       packet.Version,
					"agent_ip":      net.IP(packet.AgentIP).String(),
					"sampling_rate": flowSample.SamplingRate,
					"input_if":      flowSample.Input,
					"output_if":     flowSample.Output,
				}

				// Try to extract IPs from raw packet header
				if rawData, ok := record.Data.(sflowdec.SampledHeader); ok {
					flowRecord["protocol"] = rawData.Protocol
					flowRecord["frame_length"] = rawData.FrameLength

					ips := extractIPsFromHeader(rawData.HeaderData)
					if ips != nil {
						flowRecord["src_ip"] = ips["src_ip"]
						flowRecord["dst_ip"] = ips["dst_ip"]
						if port, ok := ips["src_port"].(uint16); ok && port != 0 {
							flowRecord["src_port"] = port
						}
						if port, ok := ips["dst_port"].(uint16); ok && port != 0 {
							flowRecord["dst_port"] = port
						}
					}
				}

				// Try to extract IPs from already-decoded IPv4 data
				if ipv4Data, ok := record.Data.(sflowdec.SampledIPv4); ok {
					flowRecord["src_ip"] = net.IP(ipv4Data.SrcIP).String()
					flowRecord["dst_ip"] = net.IP(ipv4Data.DstIP).String()
					flowRecord["src_port"] = ipv4Data.SrcPort
					flowRecord["dst_port"] = ipv4Data.DstPort
					flowRecord["protocol"] = ipv4Data.Protocol
				}

				// Try to extract IPs from already-decoded IPv6 data
				if ipv6Data, ok := record.Data.(sflowdec.SampledIPv6); ok {
					flowRecord["src_ip"] = net.IP(ipv6Data.SrcIP).String()
					flowRecord["dst_ip"] = net.IP(ipv6Data.DstIP).String()
					flowRecord["src_port"] = ipv6Data.SrcPort
					flowRecord["dst_port"] = ipv6Data.DstPort
					flowRecord["protocol"] = ipv6Data.Protocol
				}

				// Marshal and append
				jsonData, err := sonic.Marshal(flowRecord)
				if err != nil {
					continue
				}
				result = append(result, jsonData...)
				result = append(result, '\n')
				flowCount++
			}
		}

		// Handle expanded flow sample
		if expandedSample, ok := sample.(sflowdec.ExpandedFlowSample); ok {
			for _, record := range expandedSample.Records {
				flowRecord := map[string]interface{}{
					"version":       packet.Version,
					"agent_ip":      net.IP(packet.AgentIP).String(),
					"sampling_rate": expandedSample.SamplingRate,
					"input_if":      expandedSample.InputIfValue,
					"output_if":     expandedSample.OutputIfValue,
				}

				if rawData, ok := record.Data.(sflowdec.SampledHeader); ok {
					flowRecord["protocol"] = rawData.Protocol
					flowRecord["frame_length"] = rawData.FrameLength

					ips := extractIPsFromHeader(rawData.HeaderData)
					if ips != nil {
						flowRecord["src_ip"] = ips["src_ip"]
						flowRecord["dst_ip"] = ips["dst_ip"]
						if port, ok := ips["src_port"].(uint16); ok && port != 0 {
							flowRecord["src_port"] = port
						}
						if port, ok := ips["dst_port"].(uint16); ok && port != 0 {
							flowRecord["dst_port"] = port
						}
					}
				}

				if ipv4Data, ok := record.Data.(sflowdec.SampledIPv4); ok {
					flowRecord["src_ip"] = net.IP(ipv4Data.SrcIP).String()
					flowRecord["dst_ip"] = net.IP(ipv4Data.DstIP).String()
					flowRecord["src_port"] = ipv4Data.SrcPort
					flowRecord["dst_port"] = ipv4Data.DstPort
					flowRecord["protocol"] = ipv4Data.Protocol
				}

				jsonData, err := sonic.Marshal(flowRecord)
				if err != nil {
					continue
				}
				result = append(result, jsonData...)
				result = append(result, '\n')
				flowCount++
			}
		}
	}

	if flowCount == 0 {
		// No flow records extracted, output packet-level info
		packetInfo := map[string]interface{}{
			"version":         packet.Version,
			"agent_ip":        net.IP(packet.AgentIP).String(),
			"sequence_number": packet.SequenceNumber,
			"samples_count":   packet.SamplesCount,
		}
		jsonData, err := sonic.Marshal(packetInfo)
		if err != nil {
			return nil, err
		}
		result = append(result, jsonData...)
		result = append(result, '\n')
	}

	return result, nil
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
		ihl := int(headerBytes[offset]&0x0F) * 4
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
				Default:     8388608,
				Description: "UDP socket read buffer size in bytes (8MB default). Requires kernel sysctl: net.core.rmem_max=16777216",
				Validation:  "min:65536,max:16777216",
				Placeholder: "8388608",
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

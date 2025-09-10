package sflow

import (
	"encoding/binary"
	"testing"
	"time"
)

func TestSFlowParser_Parse(t *testing.T) {
	parser := NewSFlowParser()

	// Create a simple sFlow v5 packet with one flow sample
	packet := createSFlowV5TestPacket()

	records, err := parser.Parse(packet)
	if err != nil {
		t.Fatalf("Failed to parse sFlow packet: %v", err)
	}

	if len(records) != 1 {
		t.Fatalf("Expected 1 record, got %d", len(records))
	}

	record := records[0]
	if record.Version != 5 {
		t.Errorf("Expected version 5, got %d", record.Version)
	}

	if record.Type != "flow" {
		t.Errorf("Expected type 'flow', got %s", record.Type)
	}

	if record.AgentAddr != "192.168.1.1" {
		t.Errorf("Expected agent addr 192.168.1.1, got %s", record.AgentAddr)
	}

	if record.SamplingRate != 1000 {
		t.Errorf("Expected sampling rate 1000, got %d", record.SamplingRate)
	}
}

func TestSFlowParser_ParseToJSON(t *testing.T) {
	parser := NewSFlowParser()

	packet := createSFlowV5TestPacket()
	jsonData, err := parser.ParseToJSON(packet)
	if err != nil {
		t.Fatalf("Failed to parse to JSON: %v", err)
	}

	if len(jsonData) == 0 {
		t.Error("Expected non-empty JSON data")
	}

	// Should end with newline for NDJSON format
	if jsonData[len(jsonData)-1] != '\n' {
		t.Error("JSON data should end with newline")
	}
}

func TestSFlowParser_UnsupportedVersion(t *testing.T) {
	parser := NewSFlowParser()

	// Create packet with unsupported version
	packet := make([]byte, 28)
	binary.BigEndian.PutUint32(packet[0:4], 4) // Version 4 (unsupported)

	_, err := parser.Parse(packet)
	if err == nil {
		t.Error("Expected error for unsupported version, got nil")
	}
}

func TestSFlowParser_EmptyPacket(t *testing.T) {
	parser := NewSFlowParser()

	_, err := parser.Parse([]byte{})
	if err == nil {
		t.Error("Expected error for empty packet")
	}
}

func TestSFlowParser_TooSmallPacket(t *testing.T) {
	parser := NewSFlowParser()

	_, err := parser.Parse(make([]byte, 10)) // Too small for header
	if err == nil {
		t.Error("Expected error for packet too small")
	}
}

func TestSFlowRecord_ToJSON(t *testing.T) {
	record := &SFlowRecord{
		Type:         "flow",
		Version:      5,
		AgentAddr:    "192.168.1.1",
		SampleType:   "flow_sample",
		SamplingRate: 1000,
		SrcIP:        "192.168.1.10",
		DstIP:        "10.0.0.1",
		SrcPort:      80,
		DstPort:      12345,
		Protocol:     6,
		ReceivedAt:   time.Now(),
	}

	jsonData, err := record.ToJSON()
	if err != nil {
		t.Fatalf("Failed to convert to JSON: %v", err)
	}

	if len(jsonData) == 0 {
		t.Error("Expected non-empty JSON data")
	}
}

func TestSFlowParser_CounterSample(t *testing.T) {
	parser := NewSFlowParser()

	packet := createSFlowV5CounterPacket()
	records, err := parser.Parse(packet)
	if err != nil {
		t.Fatalf("Failed to parse counter sample: %v", err)
	}

	if len(records) != 1 {
		t.Fatalf("Expected 1 record, got %d", len(records))
	}

	record := records[0]
	if record.Type != "counter" {
		t.Errorf("Expected type 'counter', got %s", record.Type)
	}

	if record.CounterRecords == nil {
		t.Error("Expected counter records to be populated")
	}
}

func TestIntToIP_sFlow(t *testing.T) {
	// Test IP conversion (same function as NetFlow)
	ip := intToIP(0xC0A80101) // 192.168.1.1
	expected := "192.168.1.1"
	if ip.String() != expected {
		t.Errorf("Expected %s, got %s", expected, ip.String())
	}
}

func TestSFlowParser_ParseEthernetFromBytes(t *testing.T) {
	parser := NewSFlowParser()
	record := &SFlowRecord{}

	// Create a simple Ethernet frame with IPv4
	ethFrame := make([]byte, 34) // Ethernet + IP header

	// Destination MAC
	copy(ethFrame[0:6], []byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66})
	// Source MAC
	copy(ethFrame[6:12], []byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF})
	// EtherType (IPv4 = 0x0800)
	binary.BigEndian.PutUint16(ethFrame[12:14], 0x0800)

	// Simple IPv4 header
	ethFrame[14] = 0x45 // Version + IHL
	ethFrame[15] = 0x00 // TOS
	ethFrame[23] = 6    // Protocol (TCP)
	// Source IP (192.168.1.10)
	ethFrame[26] = 192
	ethFrame[27] = 168
	ethFrame[28] = 1
	ethFrame[29] = 10
	// Dest IP (10.0.0.1)
	ethFrame[30] = 10
	ethFrame[31] = 0
	ethFrame[32] = 0
	ethFrame[33] = 1

	parser.parseEthernetFromBytes(ethFrame, record)

	if record.SrcMAC != "aa:bb:cc:dd:ee:ff" {
		t.Errorf("Expected source MAC aa:bb:cc:dd:ee:ff, got %s", record.SrcMAC)
	}

	if record.DstMAC != "11:22:33:44:55:66" {
		t.Errorf("Expected dest MAC 11:22:33:44:55:66, got %s", record.DstMAC)
	}

	if record.SrcIP != "192.168.1.10" {
		t.Errorf("Expected source IP 192.168.1.10, got %s", record.SrcIP)
	}

	if record.DstIP != "10.0.0.1" {
		t.Errorf("Expected dest IP 10.0.0.1, got %s", record.DstIP)
	}

	if record.Protocol != 6 {
		t.Errorf("Expected protocol 6, got %d", record.Protocol)
	}
}

func TestSFlowParser_ParseVLANTag(t *testing.T) {
	parser := NewSFlowParser()
	record := &SFlowRecord{}

	// Create Ethernet frame with VLAN tag
	ethFrame := make([]byte, 38) // Ethernet + VLAN + IP header

	// Destination and Source MAC
	copy(ethFrame[0:6], []byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66})
	copy(ethFrame[6:12], []byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF})

	// VLAN tag (0x8100)
	binary.BigEndian.PutUint16(ethFrame[12:14], 0x8100)
	// VLAN info: priority=3, VLAN ID=100
	binary.BigEndian.PutUint16(ethFrame[14:16], (3<<13)|100)
	// EtherType (IPv4 = 0x0800)
	binary.BigEndian.PutUint16(ethFrame[16:18], 0x0800)

	// Simple IPv4 header starts at offset 18
	ethFrame[18] = 0x45 // Version + IHL
	ethFrame[27] = 6    // Protocol (TCP)

	parser.parseEthernetFromBytes(ethFrame, record)

	if record.VLAN != 100 {
		t.Errorf("Expected VLAN 100, got %d", record.VLAN)
	}

	if record.Priority != 3 {
		t.Errorf("Expected priority 3, got %d", record.Priority)
	}
}

// createSFlowV5TestPacket creates a sample sFlow v5 packet for testing
func createSFlowV5TestPacket() []byte {
	// sFlow header (28 bytes) + sample header (8 bytes) + flow sample data (32 bytes)
	headerSize := 28
	sampleHeaderSize := 8
	flowSampleSize := 32                                 // Basic flow sample data
	totalSampleSize := sampleHeaderSize + flowSampleSize // 40 bytes total for sample
	totalSize := headerSize + totalSampleSize

	packet := make([]byte, totalSize)

	// sFlow header
	binary.BigEndian.PutUint32(packet[0:4], 5)           // Version
	binary.BigEndian.PutUint32(packet[4:8], 1)           // Address type (IPv4)
	binary.BigEndian.PutUint32(packet[8:12], 0xC0A80101) // Agent addr (192.168.1.1)
	binary.BigEndian.PutUint32(packet[12:16], 1)         // Sub-agent ID
	binary.BigEndian.PutUint32(packet[16:20], 1000)      // Sequence number
	binary.BigEndian.PutUint32(packet[20:24], 123456)    // Sys uptime
	binary.BigEndian.PutUint32(packet[24:28], 1)         // Number of samples

	// Sample header (enterprise + format combined, then length)
	offset := 28
	binary.BigEndian.PutUint32(packet[offset:offset+4], (0<<12)|FlowSample)        // Enterprise = 0, Format = FlowSample (1)
	binary.BigEndian.PutUint32(packet[offset+4:offset+8], uint32(totalSampleSize)) // Sample length includes header

	// Flow sample data
	offset += 8
	binary.BigEndian.PutUint32(packet[offset:offset+4], 500)      // Sequence number
	binary.BigEndian.PutUint32(packet[offset+4:offset+8], 0x01)   // Source ID
	binary.BigEndian.PutUint32(packet[offset+8:offset+12], 1000)  // Sampling rate
	binary.BigEndian.PutUint32(packet[offset+12:offset+16], 5000) // Sample pool
	binary.BigEndian.PutUint32(packet[offset+16:offset+20], 0)    // Drops
	binary.BigEndian.PutUint32(packet[offset+20:offset+24], 1)    // Input interface
	binary.BigEndian.PutUint32(packet[offset+24:offset+28], 2)    // Output interface
	binary.BigEndian.PutUint32(packet[offset+28:offset+32], 0)    // Flow record count

	return packet
}

// createSFlowV5CounterPacket creates a sample sFlow v5 counter packet for testing
func createSFlowV5CounterPacket() []byte {
	// sFlow header (28 bytes) + counter sample
	headerSize := 28
	sampleHeaderSize := 8
	counterSampleSize := 12 // Basic counter sample data
	totalSampleSize := sampleHeaderSize + counterSampleSize
	totalSize := headerSize + totalSampleSize

	packet := make([]byte, totalSize)

	// sFlow header
	binary.BigEndian.PutUint32(packet[0:4], 5)           // Version
	binary.BigEndian.PutUint32(packet[4:8], 1)           // Address type (IPv4)
	binary.BigEndian.PutUint32(packet[8:12], 0xC0A80101) // Agent addr (192.168.1.1)
	binary.BigEndian.PutUint32(packet[12:16], 1)         // Sub-agent ID
	binary.BigEndian.PutUint32(packet[16:20], 1000)      // Sequence number
	binary.BigEndian.PutUint32(packet[20:24], 123456)    // Sys uptime
	binary.BigEndian.PutUint32(packet[24:28], 1)         // Number of samples

	// Sample header
	offset := 28
	binary.BigEndian.PutUint32(packet[offset:offset+4], (0<<12)|CounterSample)     // Enterprise = 0, Format = CounterSample (2)
	binary.BigEndian.PutUint32(packet[offset+4:offset+8], uint32(totalSampleSize)) // Sample length includes header

	// Counter sample data
	offset += 8
	binary.BigEndian.PutUint32(packet[offset:offset+4], 600)    // Sequence number
	binary.BigEndian.PutUint32(packet[offset+4:offset+8], 0x01) // Source ID
	binary.BigEndian.PutUint32(packet[offset+8:offset+12], 0)   // Counter record count

	return packet
}

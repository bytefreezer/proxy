package netflow

import (
	"encoding/binary"
	"testing"
	"time"
)

func TestNetFlowParser_ParseV5(t *testing.T) {
	parser := NewNetFlowParser()

	// Create a simple NetFlow v5 packet with one flow record
	packet := createNetFlowV5TestPacket()

	records, err := parser.Parse(packet)
	if err != nil {
		t.Fatalf("Failed to parse NetFlow v5 packet: %v", err)
	}

	if len(records) != 1 {
		t.Fatalf("Expected 1 record, got %d", len(records))
	}

	record := records[0]
	if record.Version != 5 {
		t.Errorf("Expected version 5, got %d", record.Version)
	}

	if record.SrcAddr != "192.168.1.1" {
		t.Errorf("Expected src addr 192.168.1.1, got %s", record.SrcAddr)
	}

	if record.DstAddr != "10.0.0.1" {
		t.Errorf("Expected dst addr 10.0.0.1, got %s", record.DstAddr)
	}

	if record.SrcPort != 80 {
		t.Errorf("Expected src port 80, got %d", record.SrcPort)
	}

	if record.DstPort != 12345 {
		t.Errorf("Expected dst port 12345, got %d", record.DstPort)
	}

	if record.Protocol != 6 {
		t.Errorf("Expected protocol 6 (TCP), got %d", record.Protocol)
	}

	if record.Packets != 10 {
		t.Errorf("Expected 10 packets, got %d", record.Packets)
	}

	if record.Bytes != 1500 {
		t.Errorf("Expected 1500 bytes, got %d", record.Bytes)
	}
}

func TestNetFlowParser_ParseV9_UnsupportedVersion(t *testing.T) {
	parser := NewNetFlowParser()

	// Create packet with unsupported version
	packet := make([]byte, 4)
	binary.BigEndian.PutUint16(packet[0:2], 99) // Unsupported version

	_, err := parser.Parse(packet)
	if err == nil {
		t.Error("Expected error for unsupported version, got nil")
	}
}

func TestNetFlowParser_ParseToJSON(t *testing.T) {
	parser := NewNetFlowParser()

	packet := createNetFlowV5TestPacket()
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

func TestNetFlowParser_EmptyPacket(t *testing.T) {
	parser := NewNetFlowParser()

	_, err := parser.Parse([]byte{})
	if err == nil {
		t.Error("Expected error for empty packet")
	}
}

func TestNetFlowParser_TooSmallPacket(t *testing.T) {
	parser := NewNetFlowParser()

	_, err := parser.Parse([]byte{1, 2, 3}) // Too small for header
	if err == nil {
		t.Error("Expected error for packet too small")
	}
}

func TestFlowRecord_ToJSON(t *testing.T) {
	record := &FlowRecord{
		Version:    5,
		SrcAddr:    "192.168.1.1",
		DstAddr:    "10.0.0.1",
		SrcPort:    80,
		DstPort:    12345,
		Protocol:   6,
		Packets:    10,
		Bytes:      1500,
		FlowStart:  time.Now(),
		FlowEnd:    time.Now(),
		ReceivedAt: time.Now(),
	}

	jsonData, err := record.ToJSON()
	if err != nil {
		t.Fatalf("Failed to convert to JSON: %v", err)
	}

	if len(jsonData) == 0 {
		t.Error("Expected non-empty JSON data")
	}
}

func TestIntToIP(t *testing.T) {
	// Test IP conversion
	ip := intToIP(0xC0A80101) // 192.168.1.1
	expected := "192.168.1.1"
	if ip.String() != expected {
		t.Errorf("Expected %s, got %s", expected, ip.String())
	}

	ip = intToIP(0x0A000001) // 10.0.0.1
	expected = "10.0.0.1"
	if ip.String() != expected {
		t.Errorf("Expected %s, got %s", expected, ip.String())
	}
}

// createNetFlowV5TestPacket creates a sample NetFlow v5 packet for testing
func createNetFlowV5TestPacket() []byte {
	packet := make([]byte, 24+48) // Header + 1 flow record

	// NetFlow v5 header
	binary.BigEndian.PutUint16(packet[0:2], 5)                          // Version
	binary.BigEndian.PutUint16(packet[2:4], 1)                          // Count
	binary.BigEndian.PutUint32(packet[4:8], 123456)                     // SysUptime
	binary.BigEndian.PutUint32(packet[8:12], uint32(time.Now().Unix())) // Unix seconds
	binary.BigEndian.PutUint32(packet[12:16], 0)                        // Unix nanoseconds
	binary.BigEndian.PutUint32(packet[16:20], 1000)                     // Flow sequence
	packet[20] = 1                                                      // Engine type
	packet[21] = 2                                                      // Engine ID
	binary.BigEndian.PutUint16(packet[22:24], 100)                      // Sampling rate

	// NetFlow v5 record (starts at byte 24)
	offset := 24
	binary.BigEndian.PutUint32(packet[offset:offset+4], 0xC0A80101)    // Src addr (192.168.1.1)
	binary.BigEndian.PutUint32(packet[offset+4:offset+8], 0x0A000001)  // Dst addr (10.0.0.1)
	binary.BigEndian.PutUint32(packet[offset+8:offset+12], 0xC0A80101) // Next hop
	binary.BigEndian.PutUint16(packet[offset+12:offset+14], 1)         // Input interface
	binary.BigEndian.PutUint16(packet[offset+14:offset+16], 2)         // Output interface
	binary.BigEndian.PutUint32(packet[offset+16:offset+20], 10)        // Packets
	binary.BigEndian.PutUint32(packet[offset+20:offset+24], 1500)      // Bytes
	binary.BigEndian.PutUint32(packet[offset+24:offset+28], 1000)      // First (flow start)
	binary.BigEndian.PutUint32(packet[offset+28:offset+32], 2000)      // Last (flow end)
	binary.BigEndian.PutUint16(packet[offset+32:offset+34], 80)        // Src port
	binary.BigEndian.PutUint16(packet[offset+34:offset+36], 12345)     // Dst port
	// Skip padding byte at offset+36
	packet[offset+37] = 24                                         // TCP flags
	packet[offset+38] = 6                                          // Protocol (TCP)
	packet[offset+39] = 0                                          // TOS
	binary.BigEndian.PutUint16(packet[offset+40:offset+42], 65001) // Src AS
	binary.BigEndian.PutUint16(packet[offset+42:offset+44], 65002) // Dst AS
	packet[offset+44] = 24                                         // Src mask
	packet[offset+45] = 16                                         // Dst mask
	// Skip 2 padding bytes at offset+46:offset+48

	return packet
}

func TestNetFlowParser_ParseV9_TemplateHandling(t *testing.T) {
	parser := NewNetFlowParser()

	// Create a NetFlow v9 packet with template flowset
	packet := createNetFlowV9TemplatePacket()

	records, err := parser.Parse(packet)
	if err != nil {
		t.Fatalf("Failed to parse NetFlow v9 packet: %v", err)
	}

	// Template packets don't contain data records, so we should get empty result
	if len(records) != 0 {
		t.Errorf("Expected 0 records for template packet, got %d", len(records))
	}

	// Check if template was cached
	if len(parser.templateCache) == 0 {
		t.Error("Expected template to be cached")
	}
}

// createNetFlowV9TemplatePacket creates a NetFlow v9 template packet for testing
func createNetFlowV9TemplatePacket() []byte {
	// NetFlow v9 header (20 bytes) + Template flowset
	packet := make([]byte, 20+4+4+4+4+4) // Header + flowset header + template header + 2 fields

	// NetFlow v9 header
	binary.BigEndian.PutUint16(packet[0:2], 9)                          // Version
	binary.BigEndian.PutUint16(packet[2:4], 1)                          // Count
	binary.BigEndian.PutUint32(packet[4:8], 123456)                     // SysUptime
	binary.BigEndian.PutUint32(packet[8:12], uint32(time.Now().Unix())) // Unix seconds
	binary.BigEndian.PutUint32(packet[12:16], 1000)                     // Sequence number
	binary.BigEndian.PutUint32(packet[16:20], 12345)                    // Source ID

	// Template flowset header
	offset := 20
	binary.BigEndian.PutUint16(packet[offset:offset+2], 0)    // Flowset ID (0 = template)
	binary.BigEndian.PutUint16(packet[offset+2:offset+4], 20) // Length

	// Template header
	offset += 4
	binary.BigEndian.PutUint16(packet[offset:offset+2], 256) // Template ID
	binary.BigEndian.PutUint16(packet[offset+2:offset+4], 2) // Field count

	// Template fields
	offset += 4
	binary.BigEndian.PutUint16(packet[offset:offset+2], 8)   // Field type (src addr)
	binary.BigEndian.PutUint16(packet[offset+2:offset+4], 4) // Field length

	offset += 4
	binary.BigEndian.PutUint16(packet[offset:offset+2], 12)  // Field type (dst addr)
	binary.BigEndian.PutUint16(packet[offset+2:offset+4], 4) // Field length

	return packet
}

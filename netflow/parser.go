package netflow

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"time"
)

// NetFlowVersion represents the NetFlow protocol version
type NetFlowVersion uint16

const (
	Version5     NetFlowVersion = 5
	Version9     NetFlowVersion = 9
	VersionIPFIX NetFlowVersion = 10 // IPFIX is version 10
)

// NetFlowParser handles parsing of NetFlow packets
type NetFlowParser struct {
	templateCache map[uint32]*Template // Cache for NetFlow v9/IPFIX templates
}

// NewNetFlowParser creates a new NetFlow parser
func NewNetFlowParser() *NetFlowParser {
	return &NetFlowParser{
		templateCache: make(map[uint32]*Template),
	}
}

// Parse parses a NetFlow packet and returns flow records as JSON
func (p *NetFlowParser) Parse(data []byte) ([]*FlowRecord, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("packet too small for NetFlow header")
	}

	// Read version from the first 2 bytes
	version := NetFlowVersion(binary.BigEndian.Uint16(data[:2]))

	switch version {
	case Version5:
		return p.parseV5(data)
	case Version9:
		return p.parseV9(data)
	case VersionIPFIX:
		return p.parseIPFIX(data)
	default:
		return nil, fmt.Errorf("unsupported NetFlow version: %d", version)
	}
}

// FlowRecord represents a single flow record in JSON format
type FlowRecord struct {
	Version      uint16    `json:"version"`
	SrcAddr      string    `json:"src_ip"`
	DstAddr      string    `json:"dst_ip"`
	NextHop      string    `json:"next_hop,omitempty"`
	Input        uint16    `json:"input_interface,omitempty"`
	Output       uint16    `json:"output_interface,omitempty"`
	Packets      uint32    `json:"packets"`
	Bytes        uint32    `json:"bytes"`
	FlowStart    time.Time `json:"flow_start"`
	FlowEnd      time.Time `json:"flow_end"`
	SrcPort      uint16    `json:"src_port"`
	DstPort      uint16    `json:"dst_port"`
	Protocol     uint8     `json:"protocol"`
	TOS          uint8     `json:"tos,omitempty"`
	TCPFlags     uint8     `json:"tcp_flags,omitempty"`
	SrcAS        uint16    `json:"src_as,omitempty"`
	DstAS        uint16    `json:"dst_as,omitempty"`
	SrcMask      uint8     `json:"src_mask,omitempty"`
	DstMask      uint8     `json:"dst_mask,omitempty"`
	EngineType   uint8     `json:"engine_type,omitempty"`
	EngineID     uint8     `json:"engine_id,omitempty"`
	SamplingRate uint16    `json:"sampling_rate,omitempty"`
	ReceivedAt   time.Time `json:"received_at"`
	ExporterAddr string    `json:"exporter_addr,omitempty"`
}

// ToJSON converts the flow record to JSON bytes
func (fr *FlowRecord) ToJSON() ([]byte, error) {
	return json.Marshal(fr)
}

// NetFlow v5 structures
type NetFlowV5Header struct {
	Version      uint16
	Count        uint16
	SysUptime    uint32
	UnixSecs     uint32
	UnixNSecs    uint32
	FlowSequence uint32
	EngineType   uint8
	EngineID     uint8
	SamplingRate uint16
}

type NetFlowV5Record struct {
	SrcAddr  uint32
	DstAddr  uint32
	NextHop  uint32
	Input    uint16
	Output   uint16
	Packets  uint32
	Bytes    uint32
	First    uint32
	Last     uint32
	SrcPort  uint16
	DstPort  uint16
	_        uint8 // padding
	TCPFlags uint8
	Protocol uint8
	TOS      uint8
	SrcAS    uint16
	DstAS    uint16
	SrcMask  uint8
	DstMask  uint8
	_        uint16 // padding
}

// parseV5 parses NetFlow v5 packets
func (p *NetFlowParser) parseV5(data []byte) ([]*FlowRecord, error) {
	if len(data) < 24 {
		return nil, fmt.Errorf("packet too small for NetFlow v5 header")
	}

	reader := bytes.NewReader(data)
	var header NetFlowV5Header

	if err := binary.Read(reader, binary.BigEndian, &header); err != nil {
		return nil, fmt.Errorf("failed to parse NetFlow v5 header: %w", err)
	}

	if header.Version != 5 {
		return nil, fmt.Errorf("expected version 5, got %d", header.Version)
	}

	expectedSize := 24 + (int(header.Count) * 48) // header + records
	if len(data) < expectedSize {
		return nil, fmt.Errorf("packet size mismatch: expected %d bytes, got %d", expectedSize, len(data))
	}

	records := make([]*FlowRecord, 0, header.Count)
	baseTime := time.Unix(int64(header.UnixSecs), int64(header.UnixNSecs))

	for i := 0; i < int(header.Count); i++ {
		var record NetFlowV5Record
		if err := binary.Read(reader, binary.BigEndian, &record); err != nil {
			return nil, fmt.Errorf("failed to parse flow record %d: %w", i, err)
		}

		// Convert to FlowRecord
		flowRecord := &FlowRecord{
			Version:      5,
			SrcAddr:      intToIP(record.SrcAddr).String(),
			DstAddr:      intToIP(record.DstAddr).String(),
			NextHop:      intToIP(record.NextHop).String(),
			Input:        record.Input,
			Output:       record.Output,
			Packets:      record.Packets,
			Bytes:        record.Bytes,
			FlowStart:    baseTime.Add(time.Duration(record.First) * time.Millisecond),
			FlowEnd:      baseTime.Add(time.Duration(record.Last) * time.Millisecond),
			SrcPort:      record.SrcPort,
			DstPort:      record.DstPort,
			Protocol:     record.Protocol,
			TOS:          record.TOS,
			TCPFlags:     record.TCPFlags,
			SrcAS:        record.SrcAS,
			DstAS:        record.DstAS,
			SrcMask:      record.SrcMask,
			DstMask:      record.DstMask,
			EngineType:   header.EngineType,
			EngineID:     header.EngineID,
			SamplingRate: header.SamplingRate,
			ReceivedAt:   time.Now(),
		}

		records = append(records, flowRecord)
	}

	return records, nil
}

// NetFlow v9/IPFIX structures
type NetFlowV9Header struct {
	Version     uint16
	Count       uint16
	SysUptime   uint32
	UnixSecs    uint32
	SequenceNum uint32
	SourceID    uint32
}

type IPFIXHeader struct {
	Version       uint16
	Length        uint16
	ExportTime    uint32
	SequenceNum   uint32
	ObservationID uint32
}

type FlowSet struct {
	ID     uint16
	Length uint16
	Data   []byte
}

type Template struct {
	ID         uint16
	FieldCount uint16
	Fields     []TemplateField
}

type TemplateField struct {
	Type   uint16
	Length uint16
}

// parseV9 parses NetFlow v9 packets
func (p *NetFlowParser) parseV9(data []byte) ([]*FlowRecord, error) {
	if len(data) < 20 {
		return nil, fmt.Errorf("packet too small for NetFlow v9 header")
	}

	reader := bytes.NewReader(data)
	var header NetFlowV9Header

	if err := binary.Read(reader, binary.BigEndian, &header); err != nil {
		return nil, fmt.Errorf("failed to parse NetFlow v9 header: %w", err)
	}

	baseTime := time.Unix(int64(header.UnixSecs), 0)
	records := make([]*FlowRecord, 0)

	// Parse flowsets
	for reader.Len() >= 4 {
		var flowSet FlowSet
		if err := binary.Read(reader, binary.BigEndian, &flowSet.ID); err != nil {
			break
		}
		if err := binary.Read(reader, binary.BigEndian, &flowSet.Length); err != nil {
			break
		}

		readerLen := reader.Len()
		if flowSet.Length < 4 || readerLen < 0 || int64(flowSet.Length) > int64(readerLen)+4 {
			break
		}

		dataLen := int(flowSet.Length) - 4
		flowSet.Data = make([]byte, dataLen)
		if _, err := reader.Read(flowSet.Data); err != nil {
			break
		}

		if flowSet.ID == 0 {
			// Template flowset
			p.parseTemplate(flowSet.Data, false)
		} else if flowSet.ID == 1 {
			// Options template flowset
			p.parseTemplate(flowSet.Data, true)
		} else if flowSet.ID >= 256 {
			// Data flowset
			flowRecords := p.parseDataFlowSet(flowSet, baseTime, header.SourceID, 9)
			records = append(records, flowRecords...)
		}
	}

	return records, nil
}

// parseIPFIX parses IPFIX packets
func (p *NetFlowParser) parseIPFIX(data []byte) ([]*FlowRecord, error) {
	if len(data) < 16 {
		return nil, fmt.Errorf("packet too small for IPFIX header")
	}

	reader := bytes.NewReader(data)
	var header IPFIXHeader

	if err := binary.Read(reader, binary.BigEndian, &header); err != nil {
		return nil, fmt.Errorf("failed to parse IPFIX header: %w", err)
	}

	baseTime := time.Unix(int64(header.ExportTime), 0)
	records := make([]*FlowRecord, 0)

	// Parse sets (similar to NetFlow v9 flowsets)
	for reader.Len() >= 4 {
		var flowSet FlowSet
		if err := binary.Read(reader, binary.BigEndian, &flowSet.ID); err != nil {
			break
		}
		if err := binary.Read(reader, binary.BigEndian, &flowSet.Length); err != nil {
			break
		}

		readerLen := reader.Len()
		if flowSet.Length < 4 || readerLen < 0 || int64(flowSet.Length) > int64(readerLen)+4 {
			break
		}

		dataLen := int(flowSet.Length) - 4
		flowSet.Data = make([]byte, dataLen)
		if _, err := reader.Read(flowSet.Data); err != nil {
			break
		}

		if flowSet.ID == 2 {
			// Template set
			p.parseTemplate(flowSet.Data, false)
		} else if flowSet.ID == 3 {
			// Options template set
			p.parseTemplate(flowSet.Data, true)
		} else if flowSet.ID >= 256 {
			// Data set
			flowRecords := p.parseDataFlowSet(flowSet, baseTime, header.ObservationID, 10)
			records = append(records, flowRecords...)
		}
	}

	return records, nil
}

// parseTemplate parses template definitions
func (p *NetFlowParser) parseTemplate(data []byte, isOptions bool) {
	reader := bytes.NewReader(data)

	for reader.Len() >= 4 {
		var template Template
		if err := binary.Read(reader, binary.BigEndian, &template.ID); err != nil {
			break
		}
		if err := binary.Read(reader, binary.BigEndian, &template.FieldCount); err != nil {
			break
		}

		// Skip options template scope field count for now
		if isOptions && reader.Len() >= 2 {
			var scopeFieldCount uint16
			binary.Read(reader, binary.BigEndian, &scopeFieldCount)
		}

		// Read template fields
		template.Fields = make([]TemplateField, template.FieldCount)
		for i := 0; i < int(template.FieldCount); i++ {
			if reader.Len() < 4 {
				break
			}
			if err := binary.Read(reader, binary.BigEndian, &template.Fields[i].Type); err != nil {
				break
			}
			if err := binary.Read(reader, binary.BigEndian, &template.Fields[i].Length); err != nil {
				break
			}
		}

		// Cache the template
		p.templateCache[uint32(template.ID)] = &template
	}
}

// parseDataFlowSet parses data flowsets using cached templates
func (p *NetFlowParser) parseDataFlowSet(flowSet FlowSet, baseTime time.Time, sourceID uint32, version uint16) []*FlowRecord {
	template, exists := p.templateCache[uint32(flowSet.ID)]
	if !exists {
		return nil
	}

	records := make([]*FlowRecord, 0)
	reader := bytes.NewReader(flowSet.Data)

	// Calculate record size
	recordSize := 0
	for _, field := range template.Fields {
		recordSize += int(field.Length)
	}

	if recordSize == 0 {
		return nil
	}

	// Parse records
	for reader.Len() >= recordSize {
		record := &FlowRecord{
			Version:    version,
			ReceivedAt: time.Now(),
		}

		// Parse fields based on template
		for _, field := range template.Fields {
			fieldData := make([]byte, field.Length)
			if _, err := reader.Read(fieldData); err != nil {
				break
			}

			p.parseField(record, field.Type, fieldData, baseTime)
		}

		records = append(records, record)
	}

	return records
}

// parseField parses individual fields based on IPFIX field types
func (p *NetFlowParser) parseField(record *FlowRecord, fieldType uint16, data []byte, baseTime time.Time) {
	switch fieldType {
	case 8: // IPv4 source address
		if len(data) >= 4 {
			record.SrcAddr = intToIP(binary.BigEndian.Uint32(data)).String()
		}
	case 12: // IPv4 destination address
		if len(data) >= 4 {
			record.DstAddr = intToIP(binary.BigEndian.Uint32(data)).String()
		}
	case 15: // IPv4 next hop address
		if len(data) >= 4 {
			record.NextHop = intToIP(binary.BigEndian.Uint32(data)).String()
		}
	case 7: // Source port
		if len(data) >= 2 {
			record.SrcPort = binary.BigEndian.Uint16(data)
		}
	case 11: // Destination port
		if len(data) >= 2 {
			record.DstPort = binary.BigEndian.Uint16(data)
		}
	case 4: // Protocol
		if len(data) >= 1 {
			record.Protocol = data[0]
		}
	case 2: // Packet count
		if len(data) >= 4 {
			record.Packets = binary.BigEndian.Uint32(data)
		}
	case 1: // Byte count
		if len(data) >= 4 {
			record.Bytes = binary.BigEndian.Uint32(data)
		}
	case 22: // Flow start time (milliseconds)
		if len(data) >= 4 {
			ms := binary.BigEndian.Uint32(data)
			record.FlowStart = baseTime.Add(time.Duration(ms) * time.Millisecond)
		}
	case 21: // Flow end time (milliseconds)
		if len(data) >= 4 {
			ms := binary.BigEndian.Uint32(data)
			record.FlowEnd = baseTime.Add(time.Duration(ms) * time.Millisecond)
		}
	case 6: // TCP flags
		if len(data) >= 1 {
			record.TCPFlags = data[0]
		}
	case 5: // Type of service
		if len(data) >= 1 {
			record.TOS = data[0]
		}
	case 10: // Input interface
		if len(data) >= 2 {
			record.Input = binary.BigEndian.Uint16(data)
		}
	case 14: // Output interface
		if len(data) >= 2 {
			record.Output = binary.BigEndian.Uint16(data)
		}
	case 16: // Source AS
		if len(data) >= 2 {
			record.SrcAS = binary.BigEndian.Uint16(data)
		}
	case 17: // Destination AS
		if len(data) >= 2 {
			record.DstAS = binary.BigEndian.Uint16(data)
		}
	case 9: // Source mask
		if len(data) >= 1 {
			record.SrcMask = data[0]
		}
	case 13: // Destination mask
		if len(data) >= 1 {
			record.DstMask = data[0]
		}
	}
}

// intToIP converts a uint32 to net.IP
func intToIP(i uint32) net.IP {
	ip := make(net.IP, 4)
	binary.BigEndian.PutUint32(ip, i)
	return ip
}

// ParseToJSON parses NetFlow data and returns JSON bytes
func (p *NetFlowParser) ParseToJSON(data []byte) ([]byte, error) {
	records, err := p.Parse(data)
	if err != nil {
		return nil, err
	}

	if len(records) == 0 {
		return nil, fmt.Errorf("no flow records found")
	}

	// Convert to NDJSON format
	var result bytes.Buffer
	for _, record := range records {
		jsonData, err := record.ToJSON()
		if err != nil {
			continue // Skip invalid records
		}
		result.Write(jsonData)
		result.WriteByte('\n')
	}

	return result.Bytes(), nil
}

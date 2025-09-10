package sflow

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"time"
)

// SFlowVersion represents the sFlow protocol version
type SFlowVersion uint32

const (
	Version5 SFlowVersion = 5
)

// Sample types
const (
	FlowSample            uint32 = 1
	CounterSample         uint32 = 2
	ExpandedFlowSample    uint32 = 3
	ExpandedCounterSample uint32 = 4
)

// Flow record types
const (
	RawPacketHeader uint32 = 1
	EthernetFrame   uint32 = 2
	IPv4Header      uint32 = 3
	IPv6Header      uint32 = 4
	ExtendedSwitch  uint32 = 1001
	ExtendedRouter  uint32 = 1002
	ExtendedGateway uint32 = 1003
)

// SFlowParser handles parsing of sFlow packets
type SFlowParser struct{}

// NewSFlowParser creates a new sFlow parser
func NewSFlowParser() *SFlowParser {
	return &SFlowParser{}
}

// Parse parses an sFlow packet and returns flow/counter records as JSON
func (p *SFlowParser) Parse(data []byte) ([]*SFlowRecord, error) {
	if len(data) < 28 {
		return nil, fmt.Errorf("packet too small for sFlow header")
	}

	reader := bytes.NewReader(data)
	var header SFlowHeader

	if err := binary.Read(reader, binary.BigEndian, &header); err != nil {
		return nil, fmt.Errorf("failed to parse sFlow header: %w", err)
	}

	if header.Version != uint32(Version5) {
		return nil, fmt.Errorf("unsupported sFlow version: %d", header.Version)
	}

	records := make([]*SFlowRecord, 0, header.NumSamples)

	// Parse samples
	for i := 0; i < int(header.NumSamples) && reader.Len() >= 8; i++ {
		sample, err := p.parseSample(reader, &header)
		if err != nil {
			continue // Skip invalid samples
		}
		if sample != nil {
			records = append(records, sample)
		}
	}

	return records, nil
}

// SFlowRecord represents a single sFlow record (flow or counter) in JSON format
type SFlowRecord struct {
	Type            string `json:"type"` // "flow" or "counter"
	Version         uint32 `json:"version"`
	AgentAddr       string `json:"agent_addr"`
	SubAgentID      uint32 `json:"sub_agent_id"`
	SequenceNum     uint32 `json:"sequence_num"`
	SysUpTime       uint32 `json:"sys_uptime"`
	SampleType      string `json:"sample_type"`
	SamplingRate    uint32 `json:"sampling_rate,omitempty"`
	SamplePool      uint32 `json:"sample_pool,omitempty"`
	Drops           uint32 `json:"drops,omitempty"`
	InputInterface  uint32 `json:"input_interface,omitempty"`
	OutputInterface uint32 `json:"output_interface,omitempty"`

	// Flow sample specific fields
	SrcIP      string `json:"src_ip,omitempty"`
	DstIP      string `json:"dst_ip,omitempty"`
	SrcPort    uint16 `json:"src_port,omitempty"`
	DstPort    uint16 `json:"dst_port,omitempty"`
	Protocol   uint8  `json:"protocol,omitempty"`
	TOS        uint8  `json:"tos,omitempty"`
	TCPFlags   uint8  `json:"tcp_flags,omitempty"`
	PacketSize uint32 `json:"packet_size,omitempty"`
	HeaderSize uint32 `json:"header_size,omitempty"`
	SrcMAC     string `json:"src_mac,omitempty"`
	DstMAC     string `json:"dst_mac,omitempty"`
	VLAN       uint16 `json:"vlan,omitempty"`
	Priority   uint8  `json:"priority,omitempty"`

	// Counter sample specific fields
	CounterRecords map[string]interface{} `json:"counter_records,omitempty"`

	// Metadata
	ReceivedAt   time.Time `json:"received_at"`
	ExporterAddr string    `json:"exporter_addr,omitempty"`
}

// ToJSON converts the sFlow record to JSON bytes
func (sr *SFlowRecord) ToJSON() ([]byte, error) {
	return json.Marshal(sr)
}

// SFlowHeader represents the sFlow datagram header
type SFlowHeader struct {
	Version     uint32
	AddressType uint32
	AgentAddr   uint32 // IPv4 address
	SubAgentID  uint32
	SequenceNum uint32
	SysUpTime   uint32
	NumSamples  uint32
}

// SampleHeader represents a sample header
type SampleHeader struct {
	Format       uint32 // Contains both enterprise (upper 20 bits) and format (lower 12 bits)
	SampleLength uint32
}

// FlowSampleData represents flow sample data
type FlowSampleData struct {
	SequenceNum     uint32
	SourceID        uint32
	SamplingRate    uint32
	SamplePool      uint32
	Drops           uint32
	InputInterface  uint32
	OutputInterface uint32
	FlowRecordCount uint32
}

// CounterSampleData represents counter sample data
type CounterSampleData struct {
	SequenceNum        uint32
	SourceID           uint32
	CounterRecordCount uint32
}

// parseSample parses a single sFlow sample
func (p *SFlowParser) parseSample(reader *bytes.Reader, header *SFlowHeader) (*SFlowRecord, error) {
	if reader.Len() < 8 {
		return nil, fmt.Errorf("not enough data for sample header")
	}

	var sampleHeader SampleHeader
	if err := binary.Read(reader, binary.BigEndian, &sampleHeader); err != nil {
		return nil, fmt.Errorf("failed to parse sample header: %w", err)
	}

	// Extract format from the lower 12 bits, enterprise from upper 20 bits
	format := sampleHeader.Format & 0xFFF
	enterprise := (sampleHeader.Format >> 12) & 0xFFFFF

	readerLen := reader.Len()
	if sampleHeader.SampleLength < 8 || readerLen < 0 || int64(sampleHeader.SampleLength) > int64(readerLen)+8 {
		return nil, fmt.Errorf("invalid sample length: %d", sampleHeader.SampleLength)
	}

	// Read sample data
	sampleDataLen := int(sampleHeader.SampleLength) - 8
	sampleData := make([]byte, sampleDataLen)
	if _, err := reader.Read(sampleData); err != nil {
		return nil, fmt.Errorf("failed to read sample data: %w", err)
	}

	sampleReader := bytes.NewReader(sampleData)

	record := &SFlowRecord{
		Version:     header.Version,
		AgentAddr:   intToIP(header.AgentAddr).String(),
		SubAgentID:  header.SubAgentID,
		SequenceNum: header.SequenceNum,
		SysUpTime:   header.SysUpTime,
		ReceivedAt:  time.Now(),
	}

	// Parse based on sample type
	switch format {
	case FlowSample:
		return p.parseFlowSample(sampleReader, record, enterprise, false)
	case CounterSample:
		return p.parseCounterSample(sampleReader, record, enterprise, false)
	case ExpandedFlowSample:
		return p.parseFlowSample(sampleReader, record, enterprise, true)
	case ExpandedCounterSample:
		return p.parseCounterSample(sampleReader, record, enterprise, true)
	default:
		return nil, fmt.Errorf("unsupported sample format: %d", format)
	}
}

// parseFlowSample parses a flow sample
func (p *SFlowParser) parseFlowSample(reader *bytes.Reader, record *SFlowRecord, enterprise uint32, expanded bool) (*SFlowRecord, error) {
	record.Type = "flow"
	record.SampleType = "flow_sample"
	if expanded {
		record.SampleType = "expanded_flow_sample"
	}

	var flowData FlowSampleData
	if err := binary.Read(reader, binary.BigEndian, &flowData); err != nil {
		return nil, fmt.Errorf("failed to parse flow sample data: %w", err)
	}

	record.SamplingRate = flowData.SamplingRate
	record.SamplePool = flowData.SamplePool
	record.Drops = flowData.Drops
	record.InputInterface = flowData.InputInterface
	record.OutputInterface = flowData.OutputInterface

	// Parse flow records
	for i := 0; i < int(flowData.FlowRecordCount) && reader.Len() >= 8; i++ {
		if err := p.parseFlowRecord(reader, record); err != nil {
			continue // Skip invalid flow records
		}
	}

	return record, nil
}

// parseCounterSample parses a counter sample
func (p *SFlowParser) parseCounterSample(reader *bytes.Reader, record *SFlowRecord, enterprise uint32, expanded bool) (*SFlowRecord, error) {
	record.Type = "counter"
	record.SampleType = "counter_sample"
	if expanded {
		record.SampleType = "expanded_counter_sample"
	}

	var counterData CounterSampleData
	if err := binary.Read(reader, binary.BigEndian, &counterData); err != nil {
		return nil, fmt.Errorf("failed to parse counter sample data: %w", err)
	}

	record.CounterRecords = make(map[string]interface{})

	// Parse counter records
	for i := 0; i < int(counterData.CounterRecordCount) && reader.Len() >= 8; i++ {
		if err := p.parseCounterRecord(reader, record); err != nil {
			continue // Skip invalid counter records
		}
	}

	return record, nil
}

// parseFlowRecord parses a flow record
func (p *SFlowParser) parseFlowRecord(reader *bytes.Reader, record *SFlowRecord) error {
	if reader.Len() < 8 {
		return fmt.Errorf("not enough data for flow record header")
	}

	var recordFormat, recordLength uint32
	if err := binary.Read(reader, binary.BigEndian, &recordFormat); err != nil {
		return err
	}
	if err := binary.Read(reader, binary.BigEndian, &recordLength); err != nil {
		return err
	}

	readerLen := reader.Len()
	if recordLength < 8 || readerLen < 0 || int64(recordLength) > int64(readerLen)+8 {
		return fmt.Errorf("invalid record length: %d", recordLength)
	}

	recordDataLen := int(recordLength) - 8
	recordData := make([]byte, recordDataLen)
	if _, err := reader.Read(recordData); err != nil {
		return err
	}

	recordReader := bytes.NewReader(recordData)

	// Parse based on record format
	format := recordFormat & 0xFFF
	switch format {
	case RawPacketHeader:
		return p.parseRawPacketHeader(recordReader, record)
	case EthernetFrame:
		return p.parseEthernetFrame(recordReader, record)
	case IPv4Header:
		return p.parseIPv4Header(recordReader, record)
	case IPv6Header:
		return p.parseIPv6Header(recordReader, record)
	}

	return nil
}

// parseRawPacketHeader parses a raw packet header
func (p *SFlowParser) parseRawPacketHeader(reader *bytes.Reader, record *SFlowRecord) error {
	if reader.Len() < 16 {
		return fmt.Errorf("not enough data for raw packet header")
	}

	var headerProtocol, frameLength, stripped, headerLength uint32
	binary.Read(reader, binary.BigEndian, &headerProtocol)
	binary.Read(reader, binary.BigEndian, &frameLength)
	binary.Read(reader, binary.BigEndian, &stripped)
	binary.Read(reader, binary.BigEndian, &headerLength)

	record.PacketSize = frameLength
	record.HeaderSize = headerLength

	if headerLength > 0 {
		readerLen := reader.Len()
		if readerLen >= 0 && int64(headerLength) <= int64(readerLen) {
			headerData := make([]byte, headerLength)
			reader.Read(headerData)

			// Parse Ethernet header if present
			if len(headerData) >= 14 {
				p.parseEthernetFromBytes(headerData, record)
			}
		}
	}

	return nil
}

// parseEthernetFrame parses an Ethernet frame record
func (p *SFlowParser) parseEthernetFrame(reader *bytes.Reader, record *SFlowRecord) error {
	if reader.Len() < 4 {
		return fmt.Errorf("not enough data for ethernet frame")
	}

	var frameLength uint32
	binary.Read(reader, binary.BigEndian, &frameLength)

	if frameLength > 0 {
		readerLen := reader.Len()
		if readerLen >= 0 && int64(frameLength) <= int64(readerLen) {
			frameData := make([]byte, frameLength)
			reader.Read(frameData)

			if len(frameData) >= 14 {
				p.parseEthernetFromBytes(frameData, record)
			}
		}
	}

	return nil
}

// parseEthernetFromBytes parses Ethernet header from bytes
func (p *SFlowParser) parseEthernetFromBytes(data []byte, record *SFlowRecord) {
	if len(data) < 14 {
		return
	}

	// Parse MAC addresses
	record.DstMAC = fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x",
		data[0], data[1], data[2], data[3], data[4], data[5])
	record.SrcMAC = fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x",
		data[6], data[7], data[8], data[9], data[10], data[11])

	// Parse EtherType
	etherType := binary.BigEndian.Uint16(data[12:14])

	// Check for VLAN tag (0x8100)
	offset := 14
	if etherType == 0x8100 && len(data) >= 18 {
		// VLAN tagged frame
		vlanInfo := binary.BigEndian.Uint16(data[14:16])
		record.VLAN = vlanInfo & 0x0FFF
		// Safe conversion: (vlanInfo >> 13) & 0x07 is always <= 7, fits in uint8
		priority := (vlanInfo >> 13) & 0x07
		if priority <= 7 {
			record.Priority = uint8(priority)
		}
		etherType = binary.BigEndian.Uint16(data[16:18])
		offset = 18
	}

	// Parse IP header if present
	if etherType == 0x0800 && len(data) >= offset+20 { // IPv4
		p.parseIPv4FromBytes(data[offset:], record)
	} else if etherType == 0x86DD && len(data) >= offset+40 { // IPv6
		p.parseIPv6FromBytes(data[offset:], record)
	}
}

// parseIPv4Header parses IPv4 header record
func (p *SFlowParser) parseIPv4Header(reader *bytes.Reader, record *SFlowRecord) error {
	if reader.Len() < 4 {
		return fmt.Errorf("not enough data for IPv4 header")
	}

	var headerLength uint32
	binary.Read(reader, binary.BigEndian, &headerLength)

	if headerLength > 0 {
		readerLen := reader.Len()
		if readerLen >= 0 && int64(headerLength) <= int64(readerLen) {
			headerData := make([]byte, headerLength)
			reader.Read(headerData)
			p.parseIPv4FromBytes(headerData, record)
		}
	}

	return nil
}

// parseIPv6Header parses IPv6 header record
func (p *SFlowParser) parseIPv6Header(reader *bytes.Reader, record *SFlowRecord) error {
	if reader.Len() < 4 {
		return fmt.Errorf("not enough data for IPv6 header")
	}

	var headerLength uint32
	binary.Read(reader, binary.BigEndian, &headerLength)

	if headerLength > 0 {
		readerLen := reader.Len()
		if readerLen >= 0 && int64(headerLength) <= int64(readerLen) {
			headerData := make([]byte, headerLength)
			reader.Read(headerData)
			p.parseIPv6FromBytes(headerData, record)
		}
	}

	return nil
}

// parseIPv4FromBytes parses IPv4 header from bytes
func (p *SFlowParser) parseIPv4FromBytes(data []byte, record *SFlowRecord) {
	if len(data) < 20 {
		return
	}

	record.TOS = data[1]
	record.Protocol = data[9]
	record.SrcIP = fmt.Sprintf("%d.%d.%d.%d", data[12], data[13], data[14], data[15])
	record.DstIP = fmt.Sprintf("%d.%d.%d.%d", data[16], data[17], data[18], data[19])

	// Parse TCP/UDP ports if present
	headerLen := int(data[0]&0x0F) * 4
	if record.Protocol == 6 || record.Protocol == 17 { // TCP or UDP
		if len(data) >= headerLen+4 {
			record.SrcPort = binary.BigEndian.Uint16(data[headerLen : headerLen+2])
			record.DstPort = binary.BigEndian.Uint16(data[headerLen+2 : headerLen+4])

			if record.Protocol == 6 && len(data) >= headerLen+14 { // TCP
				record.TCPFlags = data[headerLen+13]
			}
		}
	}
}

// parseIPv6FromBytes parses IPv6 header from bytes
func (p *SFlowParser) parseIPv6FromBytes(data []byte, record *SFlowRecord) {
	if len(data) < 40 {
		return
	}

	record.Protocol = data[6] // Next header field

	// Parse IPv6 addresses
	srcIP := make(net.IP, 16)
	copy(srcIP, data[8:24])
	record.SrcIP = srcIP.String()

	dstIP := make(net.IP, 16)
	copy(dstIP, data[24:40])
	record.DstIP = dstIP.String()

	// Parse TCP/UDP ports if present
	if record.Protocol == 6 || record.Protocol == 17 { // TCP or UDP
		if len(data) >= 44 {
			record.SrcPort = binary.BigEndian.Uint16(data[40:42])
			record.DstPort = binary.BigEndian.Uint16(data[42:44])

			if record.Protocol == 6 && len(data) >= 54 { // TCP
				record.TCPFlags = data[53]
			}
		}
	}
}

// parseCounterRecord parses a counter record
func (p *SFlowParser) parseCounterRecord(reader *bytes.Reader, record *SFlowRecord) error {
	if reader.Len() < 8 {
		return fmt.Errorf("not enough data for counter record header")
	}

	var recordFormat, recordLength uint32
	if err := binary.Read(reader, binary.BigEndian, &recordFormat); err != nil {
		return err
	}
	if err := binary.Read(reader, binary.BigEndian, &recordLength); err != nil {
		return err
	}

	readerLen := reader.Len()
	if recordLength < 8 || readerLen < 0 || int64(recordLength) > int64(readerLen)+8 {
		return fmt.Errorf("invalid counter record length: %d", recordLength)
	}

	recordDataLen := int(recordLength) - 8
	recordData := make([]byte, recordDataLen)
	if _, err := reader.Read(recordData); err != nil {
		return err
	}

	// Parse counter data based on format
	format := recordFormat & 0xFFF
	counterName := fmt.Sprintf("counter_%d", format)

	// Store raw counter data - in a real implementation you'd parse specific counter types
	record.CounterRecords[counterName] = map[string]interface{}{
		"format":   format,
		"length":   recordLength,
		"data_hex": fmt.Sprintf("%x", recordData),
	}

	return nil
}

// intToIP converts a uint32 to net.IP
func intToIP(i uint32) net.IP {
	ip := make(net.IP, 4)
	binary.BigEndian.PutUint32(ip, i)
	return ip
}

// ParseToJSON parses sFlow data and returns JSON bytes
func (p *SFlowParser) ParseToJSON(data []byte) ([]byte, error) {
	records, err := p.Parse(data)
	if err != nil {
		return nil, err
	}

	if len(records) == 0 {
		return nil, fmt.Errorf("no sFlow records found")
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

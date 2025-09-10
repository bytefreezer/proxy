#!/usr/bin/env python3
"""
NetFlow v9/IPFIX Test Script for ByteFreezer Proxy

This script generates NetFlow v9 and IPFIX packets with templates and data records
to test template-based NetFlow parsing capabilities.

Usage:
    python3 test-netflow-v9.py [options]

Requirements:
    pip3 install scapy
"""

import socket
import struct
import time
import random
import argparse
import sys
from typing import List, Tuple, Dict

# IPFIX/NetFlow v9 field types (subset)
FIELD_TYPES = {
    'IPV4_SRC_ADDR': 8,
    'IPV4_DST_ADDR': 12,
    'IPV4_NEXT_HOP': 15,
    'L4_SRC_PORT': 7,
    'L4_DST_PORT': 11,
    'PROTOCOL': 4,
    'IN_PKTS': 2,
    'IN_BYTES': 1,
    'FIRST_SWITCHED': 22,
    'LAST_SWITCHED': 21,
    'TCP_FLAGS': 6,
    'IP_TOS': 5,
    'INPUT_SNMP': 10,
    'OUTPUT_SNMP': 14,
    'SRC_AS': 16,
    'DST_AS': 17,
    'SRC_MASK': 9,
    'DST_MASK': 13
}

def create_netflow_v9_header(count: int, uptime: int, unix_secs: int, 
                           sequence: int, source_id: int = 12345) -> bytes:
    """Create NetFlow v9 header (20 bytes)"""
    return struct.pack('!HHIIIII', 
                       9,          # Version
                       count,      # Count of flowsets
                       uptime,     # System uptime (ms)
                       unix_secs,  # Unix timestamp
                       sequence,   # Sequence number
                       source_id)  # Source ID

def create_ipfix_header(length: int, export_time: int, sequence: int,
                       observation_id: int = 12345) -> bytes:
    """Create IPFIX header (16 bytes)"""
    return struct.pack('!HHIII',
                       10,             # Version (IPFIX)
                       length,         # Message length
                       export_time,    # Export time
                       sequence,       # Sequence number
                       observation_id) # Observation domain ID

def create_template_flowset(template_id: int, fields: List[Tuple[int, int]], 
                          is_ipfix: bool = False) -> bytes:
    """Create template flowset for NetFlow v9 or IPFIX"""
    
    # Template header
    template_header = struct.pack('!HH', template_id, len(fields))
    
    # Template fields
    field_data = b''
    for field_type, field_length in fields:
        field_data += struct.pack('!HH', field_type, field_length)
    
    template_body = template_header + field_data
    
    # Flowset header
    flowset_id = 2 if is_ipfix else 0  # Template set ID
    flowset_length = len(template_body) + 4
    flowset_header = struct.pack('!HH', flowset_id, flowset_length)
    
    return flowset_header + template_body

def create_data_flowset(template_id: int, records: List[bytes]) -> bytes:
    """Create data flowset with flow records"""
    
    # Calculate total length
    records_data = b''.join(records)
    flowset_length = len(records_data) + 4
    
    # Flowset header  
    flowset_header = struct.pack('!HH', template_id, flowset_length)
    
    return flowset_header + records_data

def create_flow_record(src_ip: str, dst_ip: str, src_port: int, dst_port: int,
                      protocol: int, packets: int, bytes_count: int,
                      first: int, last: int, tcp_flags: int = 0, tos: int = 0,
                      input_if: int = 1, output_if: int = 2, src_as: int = 65001,
                      dst_as: int = 65002, src_mask: int = 24, dst_mask: int = 16) -> bytes:
    """Create flow record matching template field order"""
    
    def ip_to_int(ip: str) -> int:
        return struct.unpack("!I", socket.inet_aton(ip))[0]
    
    return struct.pack('!IIHHHIIBBHHHHBB',
                       ip_to_int(src_ip),    # IPV4_SRC_ADDR (4 bytes)
                       ip_to_int(dst_ip),    # IPV4_DST_ADDR (4 bytes)
                       src_port,             # L4_SRC_PORT (2 bytes)
                       dst_port,             # L4_DST_PORT (2 bytes)
                       protocol,             # PROTOCOL (1 byte)
                       packets,              # IN_PKTS (2 bytes)
                       bytes_count,          # IN_BYTES (4 bytes)
                       first,                # FIRST_SWITCHED (4 bytes)
                       last,                 # LAST_SWITCHED (4 bytes)
                       tcp_flags,            # TCP_FLAGS (1 byte)
                       tos,                  # IP_TOS (1 byte)
                       input_if,             # INPUT_SNMP (2 bytes)
                       output_if,            # OUTPUT_SNMP (2 bytes)
                       src_as,               # SRC_AS (2 bytes)
                       dst_as,               # DST_AS (2 bytes)
                       src_mask,             # SRC_MASK (1 byte)
                       dst_mask)             # DST_MASK (1 byte)

def generate_template() -> List[Tuple[int, int]]:
    """Generate template field definitions"""
    return [
        (FIELD_TYPES['IPV4_SRC_ADDR'], 4),
        (FIELD_TYPES['IPV4_DST_ADDR'], 4),
        (FIELD_TYPES['L4_SRC_PORT'], 2),
        (FIELD_TYPES['L4_DST_PORT'], 2),
        (FIELD_TYPES['PROTOCOL'], 1),
        (FIELD_TYPES['IN_PKTS'], 2),
        (FIELD_TYPES['IN_BYTES'], 4),
        (FIELD_TYPES['FIRST_SWITCHED'], 4),
        (FIELD_TYPES['LAST_SWITCHED'], 4),
        (FIELD_TYPES['TCP_FLAGS'], 1),
        (FIELD_TYPES['IP_TOS'], 1),
        (FIELD_TYPES['INPUT_SNMP'], 2),
        (FIELD_TYPES['OUTPUT_SNMP'], 2),
        (FIELD_TYPES['SRC_AS'], 2),
        (FIELD_TYPES['DST_AS'], 2),
        (FIELD_TYPES['SRC_MASK'], 1),
        (FIELD_TYPES['DST_MASK'], 1)
    ]

def generate_flow_data() -> List[Dict]:
    """Generate realistic flow data for testing"""
    flows = [
        {"src_ip": "192.168.1.10", "dst_ip": "10.0.0.1", "src_port": 80, "dst_port": 52341, 
         "protocol": 6, "packets": 15, "bytes": 1500, "duration": 5000},
        {"src_ip": "192.168.1.20", "dst_ip": "8.8.8.8", "src_port": 443, "dst_port": 52342,
         "protocol": 6, "packets": 25, "bytes": 3200, "duration": 8000},
        {"src_ip": "192.168.1.30", "dst_ip": "10.0.0.2", "src_port": 22, "dst_port": 52343,
         "protocol": 6, "packets": 8, "bytes": 640, "duration": 2000},
        {"src_ip": "192.168.1.40", "dst_ip": "10.0.0.3", "src_port": 53, "dst_port": 52344,
         "protocol": 17, "packets": 2, "bytes": 128, "duration": 100},
        {"src_ip": "192.168.1.50", "dst_ip": "224.0.0.1", "src_port": 0, "dst_port": 0,
         "protocol": 1, "packets": 1, "bytes": 64, "duration": 0}
    ]
    return flows

def create_netflow_v9_packet(template_id: int = 256, send_template: bool = True,
                            sequence: int = 1000) -> bytes:
    """Create complete NetFlow v9 packet with template and data"""
    current_time = int(time.time())
    uptime = random.randint(100000, 500000)
    
    flowsets = []
    flowset_count = 0
    
    # Add template flowset if requested
    if send_template:
        template_fields = generate_template()
        template_flowset = create_template_flowset(template_id, template_fields, is_ipfix=False)
        flowsets.append(template_flowset)
        flowset_count += 1
    
    # Create data flowset
    flows = generate_flow_data()
    flow_records = []
    
    for flow in flows:
        first = uptime - flow["duration"] - random.randint(0, 1000)
        last = uptime - random.randint(0, 100)
        
        tcp_flags = 0
        if flow["protocol"] == 6:  # TCP
            tcp_flags = random.choice([0x18, 0x10, 0x11, 0x19])
        
        record = create_flow_record(
            flow["src_ip"], flow["dst_ip"], flow["src_port"], flow["dst_port"],
            flow["protocol"], flow["packets"], flow["bytes"], first, last, tcp_flags
        )
        flow_records.append(record)
    
    data_flowset = create_data_flowset(template_id, flow_records)
    flowsets.append(data_flowset)
    flowset_count += 1
    
    # Create header
    header = create_netflow_v9_header(
        count=flowset_count,
        uptime=uptime,
        unix_secs=current_time,
        sequence=sequence
    )
    
    return header + b''.join(flowsets)

def create_ipfix_packet(template_id: int = 256, send_template: bool = True,
                       sequence: int = 1000) -> bytes:
    """Create complete IPFIX packet with template and data"""
    current_time = int(time.time())
    
    sets = []
    
    # Add template set if requested
    if send_template:
        template_fields = generate_template()
        template_set = create_template_flowset(template_id, template_fields, is_ipfix=True)
        sets.append(template_set)
    
    # Create data set
    flows = generate_flow_data()
    flow_records = []
    
    for flow in flows:
        # IPFIX uses absolute timestamps (milliseconds since epoch)
        first = (current_time - random.randint(1, 60)) * 1000
        last = current_time * 1000
        
        tcp_flags = 0
        if flow["protocol"] == 6:  # TCP
            tcp_flags = random.choice([0x18, 0x10, 0x11, 0x19])
        
        record = create_flow_record(
            flow["src_ip"], flow["dst_ip"], flow["src_port"], flow["dst_port"],
            flow["protocol"], flow["packets"], flow["bytes"], first, last, tcp_flags
        )
        flow_records.append(record)
    
    data_set = create_data_flowset(template_id, flow_records)
    sets.append(data_set)
    
    # Calculate total message length
    sets_data = b''.join(sets)
    total_length = 16 + len(sets_data)  # Header + sets
    
    # Create header
    header = create_ipfix_header(
        length=total_length,
        export_time=current_time,
        sequence=sequence
    )
    
    return header + sets_data

def send_packets(host: str, port: int, count: int, interval: float,
                protocol: str = "v9", template_frequency: int = 10,
                verbose: bool = False) -> None:
    """Send NetFlow v9 or IPFIX packets to the specified host and port"""
    
    sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    template_id = 256
    
    try:
        print(f"Sending {count} {protocol.upper()} packets to {host}:{port}")
        print(f"Template frequency: every {template_frequency} packets")
        
        for i in range(count):
            # Send template every N packets or on first packet
            send_template = (i % template_frequency == 0)
            
            if protocol.lower() == "v9":
                packet = create_netflow_v9_packet(template_id, send_template, i + 1)
            else:  # IPFIX
                packet = create_ipfix_packet(template_id, send_template, i + 1)
            
            sock.sendto(packet, (host, port))
            
            if verbose:
                template_info = " (with template)" if send_template else ""
                print(f"Sent {protocol.upper()} packet {i + 1}: {len(packet)} bytes{template_info}")
            
            if i < count - 1:  # Don't sleep after last packet
                time.sleep(interval)
                
        print(f"Successfully sent {count} {protocol.upper()} packets")
        
    except Exception as e:
        print(f"Error sending packets: {e}")
        sys.exit(1)
    finally:
        sock.close()

def main():
    parser = argparse.ArgumentParser(description='NetFlow v9/IPFIX Test Script for ByteFreezer Proxy')
    parser.add_argument('--host', default='localhost', help='Proxy host (default: localhost)')
    parser.add_argument('--port', type=int, default=2055, help='Proxy port (default: 2055)')
    parser.add_argument('--count', type=int, default=10, help='Number of packets to send (default: 10)')
    parser.add_argument('--interval', type=float, default=1.0, help='Interval between packets in seconds (default: 1.0)')
    parser.add_argument('--protocol', choices=['v9', 'ipfix'], default='v9', help='Protocol to test (default: v9)')
    parser.add_argument('--template-freq', type=int, default=10, help='Template frequency (default: every 10 packets)')
    parser.add_argument('--verbose', '-v', action='store_true', help='Verbose output')
    
    args = parser.parse_args()
    
    print(f"NetFlow {args.protocol.upper()} Test Script")
    print("=" * 50)
    
    send_packets(
        host=args.host,
        port=args.port,
        count=args.count,
        interval=args.interval,
        protocol=args.protocol,
        template_frequency=args.template_freq,
        verbose=args.verbose
    )

if __name__ == "__main__":
    main()
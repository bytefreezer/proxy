#!/usr/bin/env python3
"""
NetFlow v5 Test Script for ByteFreezer Proxy

This script generates realistic NetFlow v5 packets and sends them to the proxy
to test NetFlow parsing and processing capabilities.

Usage:
    python3 test-netflow-v5.py [options]

Requirements:
    pip3 install scapy
"""

import socket
import struct
import time
import random
import argparse
import sys
from typing import List, Tuple

def create_netflow_v5_header(count: int, uptime: int, unix_secs: int, 
                           sequence: int, engine_type: int = 1, engine_id: int = 2,
                           sampling_rate: int = 100) -> bytes:
    """Create NetFlow v5 header (24 bytes)"""
    return struct.pack('!HHIIIIBBH', 
                       5,          # Version
                       count,      # Count of flow records
                       uptime,     # System uptime (ms)
                       unix_secs,  # Unix timestamp
                       0,          # Unix nanoseconds
                       sequence,   # Sequence number
                       engine_type, # Engine type
                       engine_id,  # Engine ID
                       sampling_rate) # Sampling rate

def create_netflow_v5_record(src_ip: str, dst_ip: str, src_port: int, dst_port: int,
                           protocol: int, packets: int, bytes_count: int,
                           first: int, last: int, tcp_flags: int = 0,
                           tos: int = 0, src_as: int = 65001, dst_as: int = 65002,
                           src_mask: int = 24, dst_mask: int = 16,
                           next_hop: str = "0.0.0.0", input_if: int = 1, 
                           output_if: int = 2) -> bytes:
    """Create NetFlow v5 flow record (48 bytes)"""
    
    def ip_to_int(ip: str) -> int:
        return struct.unpack("!I", socket.inet_aton(ip))[0]
    
    return struct.pack('!IIIHHIIIHHxBBBHHBBxx',
                       ip_to_int(src_ip),    # Source IP
                       ip_to_int(dst_ip),    # Destination IP
                       ip_to_int(next_hop),  # Next hop IP
                       input_if,             # Input interface
                       output_if,            # Output interface
                       packets,              # Packet count
                       bytes_count,          # Byte count
                       first,                # Flow start time
                       last,                 # Flow end time
                       src_port,             # Source port
                       dst_port,             # Destination port
                       # x = padding byte
                       tcp_flags,            # TCP flags
                       protocol,             # IP protocol
                       tos,                  # Type of service
                       src_as,               # Source AS
                       dst_as,               # Destination AS
                       src_mask,             # Source mask
                       dst_mask)             # Destination mask
                       # xx = 2 padding bytes

def generate_flow_data() -> List[Tuple]:
    """Generate realistic flow data for testing"""
    flows = [
        # (src_ip, dst_ip, src_port, dst_port, protocol, packets, bytes, duration)
        ("192.168.1.10", "10.0.0.1", 80, 52341, 6, 15, 1500, 5000),     # HTTP
        ("192.168.1.20", "8.8.8.8", 443, 52342, 6, 25, 3200, 8000),     # HTTPS
        ("192.168.1.30", "10.0.0.2", 22, 52343, 6, 8, 640, 2000),       # SSH
        ("192.168.1.40", "10.0.0.3", 53, 52344, 17, 2, 128, 100),       # DNS
        ("192.168.1.50", "224.0.0.1", 0, 0, 1, 1, 64, 0),               # ICMP
        ("192.168.1.60", "10.0.0.4", 993, 52345, 6, 12, 2048, 3000),    # IMAPS
        ("192.168.1.70", "172.16.1.1", 3389, 52346, 6, 45, 8192, 15000), # RDP
        ("192.168.1.80", "10.0.0.5", 25, 52347, 6, 6, 512, 1000),       # SMTP
    ]
    return flows

def create_netflow_v5_packet(flows: List[Tuple], sequence: int = 1000) -> bytes:
    """Create complete NetFlow v5 packet with multiple flow records"""
    current_time = int(time.time())
    uptime = random.randint(100000, 500000)
    
    # Create header
    header = create_netflow_v5_header(
        count=len(flows),
        uptime=uptime,
        unix_secs=current_time,
        sequence=sequence
    )
    
    # Create flow records
    records = b''
    for src_ip, dst_ip, src_port, dst_port, protocol, packets, byte_count, duration in flows:
        first = uptime - duration - random.randint(0, 1000)
        last = uptime - random.randint(0, 100)
        
        # Set TCP flags for TCP flows
        tcp_flags = 0
        if protocol == 6:  # TCP
            tcp_flags = random.choice([0x18, 0x10, 0x11, 0x19])  # PSH+ACK, ACK, FIN+ACK, PSH+FIN+ACK
        
        record = create_netflow_v5_record(
            src_ip, dst_ip, src_port, dst_port, protocol,
            packets, byte_count, first, last, tcp_flags
        )
        records += record
    
    return header + records

def send_netflow_packets(host: str, port: int, count: int, interval: float,
                        burst_size: int = 1, verbose: bool = False) -> None:
    """Send NetFlow v5 packets to the specified host and port"""
    
    sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    
    try:
        print(f"Sending {count} NetFlow v5 packets to {host}:{port}")
        print(f"Burst size: {burst_size}, Interval: {interval}s")
        
        for i in range(count):
            # Create flows for this packet
            flows = generate_flow_data()
            
            # Send burst of packets
            for j in range(burst_size):
                packet = create_netflow_v5_packet(flows, sequence=i*burst_size + j + 1)
                sock.sendto(packet, (host, port))
                
                if verbose:
                    print(f"Sent packet {i*burst_size + j + 1}: {len(packet)} bytes, {len(flows)} flows")
            
            if i < count - 1:  # Don't sleep after last packet
                time.sleep(interval)
                
        print(f"Successfully sent {count * burst_size} NetFlow v5 packets")
        
    except Exception as e:
        print(f"Error sending packets: {e}")
        sys.exit(1)
    finally:
        sock.close()

def validate_packet(packet: bytes) -> bool:
    """Validate NetFlow v5 packet structure"""
    if len(packet) < 24:
        return False
    
    # Check header
    version = struct.unpack('!H', packet[0:2])[0]
    count = struct.unpack('!H', packet[2:4])[0]
    
    if version != 5:
        return False
    
    expected_size = 24 + (count * 48)
    return len(packet) == expected_size

def main():
    parser = argparse.ArgumentParser(description='NetFlow v5 Test Script for ByteFreezer Proxy')
    parser.add_argument('--host', default='localhost', help='Proxy host (default: localhost)')
    parser.add_argument('--port', type=int, default=2055, help='Proxy port (default: 2055)')
    parser.add_argument('--count', type=int, default=10, help='Number of packets to send (default: 10)')
    parser.add_argument('--interval', type=float, default=1.0, help='Interval between packets in seconds (default: 1.0)')
    parser.add_argument('--burst', type=int, default=1, help='Number of packets per burst (default: 1)')
    parser.add_argument('--verbose', '-v', action='store_true', help='Verbose output')
    parser.add_argument('--validate', action='store_true', help='Validate packet structure before sending')
    
    args = parser.parse_args()
    
    print("NetFlow v5 Test Script")
    print("=" * 50)
    
    if args.validate:
        print("Validating packet structure...")
        test_packet = create_netflow_v5_packet(generate_flow_data())
        if validate_packet(test_packet):
            print(f"✓ Packet structure valid ({len(test_packet)} bytes)")
        else:
            print("✗ Packet structure invalid")
            sys.exit(1)
    
    send_netflow_packets(
        host=args.host,
        port=args.port,
        count=args.count,
        interval=args.interval,
        burst_size=args.burst,
        verbose=args.verbose
    )

if __name__ == "__main__":
    main()
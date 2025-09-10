#!/usr/bin/env python3
"""
sFlow v5 Test Script for ByteFreezer Proxy

This script generates realistic sFlow v5 packets with flow and counter samples
to test sFlow parsing and processing capabilities.

Usage:
    python3 test-sflow.py [options]

Requirements:
    pip3 install scapy
"""

import socket
import struct
import time
import random
import argparse
import sys
from typing import List, Dict, Tuple

# sFlow sample types
FLOW_SAMPLE = 1
COUNTER_SAMPLE = 2
EXPANDED_FLOW_SAMPLE = 3
EXPANDED_COUNTER_SAMPLE = 4

# sFlow flow record types
RAW_PACKET_HEADER = 1
ETHERNET_FRAME = 2
IPV4_HEADER = 3
IPV6_HEADER = 4
EXTENDED_SWITCH = 1001
EXTENDED_ROUTER = 1002

def create_sflow_header(agent_ip: str, sub_agent_id: int = 1, sequence: int = 1000,
                       uptime: int = 123456, num_samples: int = 1) -> bytes:
    """Create sFlow v5 header (28 bytes)"""
    
    def ip_to_int(ip: str) -> int:
        return struct.unpack("!I", socket.inet_aton(ip))[0]
    
    return struct.pack('!IIIIIII',
                       5,                    # Version
                       1,                    # Address type (IPv4)
                       ip_to_int(agent_ip),  # Agent address
                       sub_agent_id,         # Sub-agent ID
                       sequence,             # Sequence number
                       uptime,               # System uptime
                       num_samples)          # Number of samples

def create_sample_header(sample_format: int, sample_length: int, enterprise: int = 0) -> bytes:
    """Create sample header (8 bytes)"""
    format_enterprise = (enterprise << 12) | sample_format
    return struct.pack('!II', format_enterprise, sample_length)

def create_flow_sample(sampling_rate: int = 1000, sample_pool: int = 5000,
                      drops: int = 0, input_if: int = 1, output_if: int = 2,
                      sequence: int = 500, source_id: int = 0x01,
                      include_records: bool = True) -> bytes:
    """Create flow sample data"""
    
    # Flow sample header
    sample_data = struct.pack('!IIIIIII',
                             sequence,       # Sequence number
                             source_id,      # Source ID  
                             sampling_rate,  # Sampling rate
                             sample_pool,    # Sample pool
                             drops,          # Drops
                             input_if,       # Input interface
                             output_if)      # Output interface
    
    if include_records:
        # Add raw packet header record
        packet_header = create_raw_packet_header()
        sample_data += packet_header
        
        # Update flow record count
        flow_record_count = 1
    else:
        flow_record_count = 0
    
    # Add flow record count at the end
    sample_data += struct.pack('!I', flow_record_count)
    
    return sample_data

def create_raw_packet_header() -> bytes:
    """Create raw packet header flow record"""
    
    # Create a simple Ethernet + IPv4 + TCP packet header
    eth_header = create_ethernet_header()
    ip_header = create_ipv4_header()
    tcp_header = create_tcp_header()
    
    packet_header = eth_header + ip_header + tcp_header
    header_length = len(packet_header)
    
    # Raw packet header record format
    record_header = struct.pack('!II', RAW_PACKET_HEADER, 16 + header_length)  # Format + length
    record_data = struct.pack('!IIII',
                             1,              # Header protocol (Ethernet)
                             1500,           # Frame length
                             0,              # Stripped bytes
                             header_length)  # Header length
    
    return record_header + record_data + packet_header

def create_ethernet_header(src_mac: str = "aa:bb:cc:dd:ee:ff",
                          dst_mac: str = "11:22:33:44:55:66",
                          vlan: int = 0, ether_type: int = 0x0800) -> bytes:
    """Create Ethernet header"""
    
    def mac_to_bytes(mac: str) -> bytes:
        return bytes.fromhex(mac.replace(':', ''))
    
    header = mac_to_bytes(dst_mac) + mac_to_bytes(src_mac)
    
    # Add VLAN tag if specified
    if vlan > 0:
        vlan_tag = struct.pack('!HH', 0x8100, vlan & 0x0FFF)
        header += vlan_tag
    
    header += struct.pack('!H', ether_type)
    return header

def create_ipv4_header(src_ip: str = "192.168.1.10", dst_ip: str = "10.0.0.1",
                      protocol: int = 6, tos: int = 0) -> bytes:
    """Create IPv4 header (20 bytes)"""
    
    def ip_to_int(ip: str) -> int:
        return struct.unpack("!I", socket.inet_aton(ip))[0]
    
    return struct.pack('!BBHHHBBHII',
                       0x45,              # Version + IHL
                       tos,               # TOS
                       40,                # Total length (IP + TCP)
                       random.randint(1, 65535),  # ID
                       0,                 # Flags + Fragment offset
                       64,                # TTL
                       protocol,          # Protocol
                       0,                 # Checksum (0 for simplicity)
                       ip_to_int(src_ip), # Source IP
                       ip_to_int(dst_ip)) # Destination IP

def create_tcp_header(src_port: int = 80, dst_port: int = 52341,
                     flags: int = 0x18) -> bytes:
    """Create TCP header (20 bytes)"""
    
    return struct.pack('!HHIIBBHHH',
                       src_port,           # Source port
                       dst_port,           # Destination port
                       random.randint(1000, 1000000),  # Sequence number
                       0,                  # Acknowledgment number
                       0x50,               # Data offset + reserved
                       flags,              # Flags
                       8192,               # Window size
                       0,                  # Checksum
                       0)                  # Urgent pointer

def create_counter_sample(sequence: int = 600, source_id: int = 0x01) -> bytes:
    """Create counter sample data"""
    
    # Counter sample header
    sample_data = struct.pack('!III',
                             sequence,       # Sequence number
                             source_id,      # Source ID
                             1)              # Counter record count
    
    # Add a generic counter record
    counter_record = create_generic_counter_record()
    sample_data += counter_record
    
    return sample_data

def create_generic_counter_record() -> bytes:
    """Create generic interface counter record"""
    
    # Counter record format (simplified)
    record_data = struct.pack('!IIIIIIIIII',
                             random.randint(1000, 10000),    # ifInOctets
                             random.randint(100, 1000),      # ifInUcastPkts
                             random.randint(0, 10),          # ifInMulticastPkts
                             random.randint(0, 5),           # ifInBroadcastPkts
                             random.randint(0, 2),           # ifInDiscards
                             random.randint(5000, 50000),    # ifOutOctets
                             random.randint(500, 5000),      # ifOutUcastPkts
                             random.randint(0, 20),          # ifOutMulticastPkts
                             random.randint(0, 10),          # ifOutBroadcastPkts
                             random.randint(0, 3))           # ifOutDiscards
    
    # Counter record header
    record_header = struct.pack('!II', 1, len(record_data))  # Format + length
    
    return record_header + record_data

def create_sflow_packet(agent_ip: str = "192.168.1.254", sample_type: str = "flow",
                       sequence: int = 1000) -> bytes:
    """Create complete sFlow v5 packet"""
    
    samples = []
    
    if sample_type == "flow":
        # Create flow sample
        flow_sample_data = create_flow_sample(sequence=sequence)
        sample_header = create_sample_header(FLOW_SAMPLE, len(flow_sample_data) + 8)
        samples.append(sample_header + flow_sample_data)
        
    elif sample_type == "counter":
        # Create counter sample
        counter_sample_data = create_counter_sample(sequence=sequence)
        sample_header = create_sample_header(COUNTER_SAMPLE, len(counter_sample_data) + 8)
        samples.append(sample_header + counter_sample_data)
        
    elif sample_type == "mixed":
        # Create both flow and counter samples
        flow_sample_data = create_flow_sample(sequence=sequence)
        flow_header = create_sample_header(FLOW_SAMPLE, len(flow_sample_data) + 8)
        samples.append(flow_header + flow_sample_data)
        
        counter_sample_data = create_counter_sample(sequence=sequence + 1)
        counter_header = create_sample_header(COUNTER_SAMPLE, len(counter_sample_data) + 8)
        samples.append(counter_header + counter_sample_data)
    
    # Create sFlow header
    header = create_sflow_header(
        agent_ip=agent_ip,
        sequence=sequence,
        num_samples=len(samples)
    )
    
    return header + b''.join(samples)

def generate_test_scenarios() -> List[Dict]:
    """Generate different test scenarios"""
    return [
        {"type": "flow", "description": "Flow sample with packet header"},
        {"type": "counter", "description": "Interface counter sample"},
        {"type": "mixed", "description": "Mixed flow and counter samples"}
    ]

def send_sflow_packets(host: str, port: int, count: int, interval: float,
                      sample_type: str = "flow", agent_ip: str = "192.168.1.254",
                      verbose: bool = False) -> None:
    """Send sFlow v5 packets to the specified host and port"""
    
    sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    
    try:
        print(f"Sending {count} sFlow v5 packets to {host}:{port}")
        print(f"Agent IP: {agent_ip}, Sample type: {sample_type}")
        
        for i in range(count):
            packet = create_sflow_packet(
                agent_ip=agent_ip,
                sample_type=sample_type,
                sequence=i + 1000
            )
            
            sock.sendto(packet, (host, port))
            
            if verbose:
                print(f"Sent sFlow packet {i + 1}: {len(packet)} bytes, type: {sample_type}")
            
            if i < count - 1:  # Don't sleep after last packet
                time.sleep(interval)
                
        print(f"Successfully sent {count} sFlow v5 packets")
        
    except Exception as e:
        print(f"Error sending packets: {e}")
        sys.exit(1)
    finally:
        sock.close()

def run_test_scenarios(host: str, port: int, verbose: bool = False) -> None:
    """Run all test scenarios"""
    scenarios = generate_test_scenarios()
    
    print("Running sFlow Test Scenarios")
    print("=" * 50)
    
    for i, scenario in enumerate(scenarios, 1):
        print(f"\nScenario {i}: {scenario['description']}")
        print("-" * 30)
        
        send_sflow_packets(
            host=host,
            port=port,
            count=3,
            interval=0.5,
            sample_type=scenario["type"],
            verbose=verbose
        )
        
        time.sleep(1)  # Pause between scenarios

def validate_packet(packet: bytes) -> bool:
    """Validate sFlow v5 packet structure"""
    if len(packet) < 28:
        return False
    
    # Check header
    version = struct.unpack('!I', packet[0:4])[0]
    if version != 5:
        return False
    
    num_samples = struct.unpack('!I', packet[24:28])[0]
    return num_samples > 0

def main():
    parser = argparse.ArgumentParser(description='sFlow v5 Test Script for ByteFreezer Proxy')
    parser.add_argument('--host', default='localhost', help='Proxy host (default: localhost)')
    parser.add_argument('--port', type=int, default=6343, help='Proxy port (default: 6343)')
    parser.add_argument('--count', type=int, default=10, help='Number of packets to send (default: 10)')
    parser.add_argument('--interval', type=float, default=1.0, help='Interval between packets in seconds (default: 1.0)')
    parser.add_argument('--type', choices=['flow', 'counter', 'mixed'], default='flow', 
                       help='Sample type to send (default: flow)')
    parser.add_argument('--agent-ip', default='192.168.1.254', help='sFlow agent IP (default: 192.168.1.254)')
    parser.add_argument('--scenarios', action='store_true', help='Run all test scenarios')
    parser.add_argument('--verbose', '-v', action='store_true', help='Verbose output')
    parser.add_argument('--validate', action='store_true', help='Validate packet structure before sending')
    
    args = parser.parse_args()
    
    print("sFlow v5 Test Script")
    print("=" * 50)
    
    if args.validate:
        print("Validating packet structure...")
        test_packet = create_sflow_packet()
        if validate_packet(test_packet):
            print(f"✓ Packet structure valid ({len(test_packet)} bytes)")
        else:
            print("✗ Packet structure invalid")
            sys.exit(1)
    
    if args.scenarios:
        run_test_scenarios(args.host, args.port, args.verbose)
    else:
        send_sflow_packets(
            host=args.host,
            port=args.port,
            count=args.count,
            interval=args.interval,
            sample_type=args.type,
            agent_ip=args.agent_ip,
            verbose=args.verbose
        )

if __name__ == "__main__":
    main()
#!/usr/bin/env python3
"""
sFlow Test Data Generator with Realistic IPs for GeoIP Testing
Generates sFlow v5 packets with IP addresses from different countries
"""

import socket
import struct
import random
import time
import ipaddress

class SFlowGenerator:
    def __init__(self, collector_host, collector_port=6343):
        self.collector_host = collector_host
        self.collector_port = collector_port
        self.sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        self.sequence_number = 0
        self.sample_sequence = 0
        
        # Realistic public IP ranges from different regions
        # These are real public IP ranges for GeoIP testing
        self.ip_ranges = {
            'US_East': ['54.80.0.0/13', '52.20.0.0/14', '34.192.0.0/12'],  # AWS US-East
            'US_West': ['52.8.0.0/16', '54.176.0.0/15', '13.56.0.0/16'],   # AWS US-West
            'Europe_London': ['35.176.0.0/15', '18.132.0.0/16'],           # AWS London
            'Europe_Frankfurt': ['3.120.0.0/14', '18.184.0.0/15'],         # AWS Frankfurt
            'Asia_Tokyo': ['54.248.0.0/15', '52.192.0.0/15'],              # AWS Tokyo
            'Asia_Singapore': ['54.251.0.0/16', '52.76.0.0/14'],           # AWS Singapore
            'Australia': ['54.252.0.0/16', '13.210.0.0/15'],               # AWS Sydney
            'Brazil': ['54.232.0.0/16', '177.71.0.0/16'],                  # AWS Sao Paulo
            'Canada': ['35.182.0.0/15', '52.60.0.0/16'],                   # AWS Canada
            'India': ['13.232.0.0/14', '65.0.0.0/16'],                     # AWS Mumbai
            'China_Adjacent': ['54.222.0.0/15', '52.80.0.0/16'],           # AWS HK/China border
            'Russia_Adjacent': ['13.124.0.0/16', '3.34.0.0/15'],           # AWS Seoul (near Russia)
            'CDN_Cloudflare': ['104.16.0.0/13', '104.24.0.0/14'],          # Cloudflare
            'CDN_Fastly': ['151.101.0.0/16', '146.75.0.0/16'],             # Fastly
            'Google': ['8.8.8.0/24', '8.8.4.0/24', '172.217.0.0/16'],      # Google DNS & services
        }
        
        # Flatten IP ranges for easier random selection
        self.all_ips = []
        for region, ranges in self.ip_ranges.items():
            for range_str in ranges:
                network = ipaddress.ip_network(range_str)
                # Sample some IPs from each range (not all, for performance)
                for _ in range(20):
                    random_ip = str(ipaddress.ip_address(
                        network.network_address + 
                        random.randint(1, network.num_addresses - 2)
                    ))
                    self.all_ips.append((random_ip, region))
    
    def get_random_ip(self):
        """Get a random IP and its region"""
        return random.choice(self.all_ips)
    
    def create_flow_sample(self, src_ip, dst_ip):
        """Create an sFlow flow sample with given IPs"""
        # Enterprise format for flow sample
        sample_type_enterprise = (0 << 31) | (0 << 12) | 1  # Format: 0:1 (single flow sample)
        
        # Basic flow sample header
        seq_number = self.sample_sequence
        self.sample_sequence += 1
        
        # Source ID (interface index)
        source_id_type = 0  # Single interface
        source_id_index = random.randint(1, 100)
        source_id = (source_id_type << 24) | source_id_index
        
        sampling_rate = 1024
        sample_pool = random.randint(10000, 100000)
        drops = 0
        
        # Interface information
        input_if = random.randint(1, 48)
        output_if = random.randint(1, 48)
        
        # Flow record - Raw packet header
        flow_format = (0 << 31) | (0 << 12) | 1  # Format 0:1 - Raw packet header
        
        # Ethernet header (simplified)
        ethernet_header = b'\x00' * 14  # Simplified ethernet
        
        # IP header (20 bytes)
        ip_header = struct.pack('!BBHHHBBH4s4s',
            0x45,  # Version (4) and header length (5 words)
            0,     # Type of service
            random.randint(40, 1500),  # Total length
            random.randint(1, 65535),   # ID
            0x4000,  # Flags and fragment offset
            64,      # TTL
            6,       # Protocol (TCP)
            0,       # Checksum (simplified)
            socket.inet_aton(src_ip),
            socket.inet_aton(dst_ip)
        )
        
        # TCP header (simplified, 20 bytes)
        tcp_header = struct.pack('!HHLLBBHHH',
            random.choice([80, 443, 22, 3306, 5432, 8080, 3000]),  # Source port
            random.choice([80, 443, 22, 3306, 5432, 8080, 3000]),  # Dest port
            random.randint(1000, 100000),  # Sequence
            random.randint(1000, 100000),  # Acknowledgment
            0x50,  # Data offset
            0x18,  # Flags (PSH, ACK)
            8192,  # Window
            0,     # Checksum
            0      # Urgent pointer
        )
        
        raw_packet = ethernet_header + ip_header + tcp_header
        
        # Flow record structure
        protocol = 1  # Ethernet
        frame_length = len(raw_packet)
        stripped = 0
        header_size = len(raw_packet)
        
        flow_data = struct.pack('!IIII',
            protocol,
            frame_length,
            stripped,
            header_size
        ) + raw_packet
        
        # Pad to 4-byte boundary
        while len(flow_data) % 4 != 0:
            flow_data += b'\x00'
        
        flow_record = struct.pack('!II', flow_format, len(flow_data)) + flow_data
        
        # Complete flow sample
        num_records = 1
        sample_data = struct.pack('!IIIIII',
            seq_number,
            source_id,
            sampling_rate,
            sample_pool,
            drops,
            input_if
        ) + struct.pack('!II', output_if, num_records) + flow_record
        
        # Sample header
        sample_header = struct.pack('!II', sample_type_enterprise, len(sample_data))
        
        return sample_header + sample_data
    
    def create_sflow_datagram(self):
        """Create a complete sFlow v5 datagram"""
        # sFlow version 5
        version = 5
        
        # Agent information
        agent_address_type = 1  # IPv4
        agent_ip = random.choice([ip for ip, _ in self.all_ips])[0]
        agent_address = socket.inet_aton(agent_ip)
        sub_agent_id = 0
        
        # Sequence number
        self.sequence_number += 1
        
        # Uptime (milliseconds)
        uptime = int(time.time() * 1000) & 0xFFFFFFFF
        
        # Create 1-3 flow samples
        samples = []
        num_samples = random.randint(1, 3)
        
        for _ in range(num_samples):
            src_ip, src_region = self.get_random_ip()
            dst_ip, dst_region = self.get_random_ip()
            
            # Sometimes use internal IPs for more realistic traffic
            if random.random() < 0.3:
                src_ip = f"10.{random.randint(0,255)}.{random.randint(0,255)}.{random.randint(1,254)}"
                src_region = "Internal"
            
            samples.append(self.create_flow_sample(src_ip, dst_ip))
        
        # Build complete datagram
        header = struct.pack('!II',
            version,
            agent_address_type
        ) + agent_address + struct.pack('!IIII',
            sub_agent_id,
            self.sequence_number,
            uptime,
            num_samples
        )
        
        datagram = header + b''.join(samples)
        
        return datagram
    
    def run(self, packets_per_second=10, duration=None):
        """Generate and send sFlow packets"""
        print(f"Starting sFlow generator...")
        print(f"Sending to {self.collector_host}:{self.collector_port}")
        print(f"Rate: {packets_per_second} packets/second")
        print(f"Duration: {'Unlimited' if duration is None else f'{duration} seconds'}")
        print("\nSample IP regions being used:")
        for region in self.ip_ranges.keys():
            print(f"  - {region}")
        print("\nPress Ctrl+C to stop...")
        
        start_time = time.time()
        packet_count = 0
        
        try:
            while True:
                if duration and (time.time() - start_time) > duration:
                    break
                
                packet = self.create_sflow_datagram()
                self.sock.sendto(packet, (self.collector_host, self.collector_port))
                packet_count += 1
                
                if packet_count % 100 == 0:
                    print(f"Sent {packet_count} packets...")
                
                time.sleep(1.0 / packets_per_second)
                
        except KeyboardInterrupt:
            print("\n\nStopping...")
        finally:
            elapsed = time.time() - start_time
            print(f"\nSent {packet_count} packets in {elapsed:.1f} seconds")
            print(f"Average rate: {packet_count/elapsed:.1f} packets/second")

def main():
    import argparse
    
    parser = argparse.ArgumentParser(description='sFlow v5 Generator with GeoIP test data')
    parser.add_argument('--host', '-H', default='tp1',
                        help='Collector host (default: localhost)')
    parser.add_argument('--port', '-p', type=int, default=2060,
                        help='Collector port (default: 6343)')
    parser.add_argument('--rate', '-r', type=int, default=10,
                        help='Packets per second (default: 10)')
    parser.add_argument('--duration', '-d', type=int, default=None,
                        help='Duration in seconds (default: unlimited)')
    
    args = parser.parse_args()
    
    generator = SFlowGenerator(args.host, args.port)
    generator.run(args.rate, args.duration)

if __name__ == '__main__':
    main()
#!/usr/bin/env python3
"""
Syslog Test Script for ByteFreezer Proxy

This script generates RFC3164 and RFC5424 syslog messages to test
syslog parsing and processing capabilities.

Usage:
    python3 test-syslog.py [options]

Requirements:
    Standard Python library only
"""

import socket
import time
import random
import argparse
import sys
from datetime import datetime
from typing import List, Dict

# Syslog facilities
FACILITIES = {
    'kernel': 0, 'user': 1, 'mail': 2, 'daemon': 3, 'auth': 4, 'syslog': 5,
    'lpr': 6, 'news': 7, 'uucp': 8, 'cron': 9, 'authpriv': 10, 'ftp': 11,
    'local0': 16, 'local1': 17, 'local2': 18, 'local3': 19, 'local4': 20,
    'local5': 21, 'local6': 22, 'local7': 23
}

# Syslog severities
SEVERITIES = {
    'emerg': 0, 'alert': 1, 'crit': 2, 'err': 3,
    'warning': 4, 'notice': 5, 'info': 6, 'debug': 7
}

def calculate_priority(facility: str, severity: str) -> int:
    """Calculate syslog priority value"""
    return FACILITIES[facility] * 8 + SEVERITIES[severity]

def create_rfc3164_message(facility: str = 'local0', severity: str = 'info',
                          hostname: str = 'web01', tag: str = 'nginx',
                          process_id: int = None, message: str = 'Test message') -> str:
    """Create RFC3164 syslog message"""
    
    priority = calculate_priority(facility, severity)
    timestamp = datetime.now().strftime('%b %d %H:%M:%S')
    
    # Handle process ID
    if process_id:
        tag_with_pid = f"{tag}[{process_id}]"
    else:
        tag_with_pid = tag
    
    return f"<{priority}>{timestamp} {hostname} {tag_with_pid}: {message}"

def create_rfc5424_message(facility: str = 'local0', severity: str = 'info',
                          hostname: str = 'web01', app_name: str = 'nginx',
                          proc_id: str = '1234', msg_id: str = 'ID47',
                          message: str = 'Test message') -> str:
    """Create RFC5424 syslog message"""
    
    priority = calculate_priority(facility, severity)
    timestamp = datetime.now().strftime('%Y-%m-%dT%H:%M:%S.%fZ')
    
    # Structured data (simplified - just empty)
    structured_data = '-'
    
    return f"<{priority}>1 {timestamp} {hostname} {app_name} {proc_id} {msg_id} {structured_data} {message}"

def generate_realistic_messages() -> List[Dict]:
    """Generate realistic syslog messages for testing"""
    
    messages = [
        # Web server logs
        {
            'type': 'rfc3164',
            'facility': 'local0',
            'severity': 'info',
            'hostname': 'web01',
            'tag': 'nginx',
            'process_id': 1234,
            'message': '192.168.1.100 - - [10/Oct/2000:13:55:36 -0700] "GET /apache.gif HTTP/1.1" 200 2326'
        },
        {
            'type': 'rfc5424',
            'facility': 'local0',
            'severity': 'info',
            'hostname': 'web01',
            'app_name': 'apache',
            'proc_id': '5678',
            'msg_id': 'ACCESS',
            'message': '10.0.0.1 - frank [10/Oct/2000:13:55:36 -0700] "GET /secure/ HTTP/1.1" 401 2326'
        },
        
        # System logs
        {
            'type': 'rfc3164',
            'facility': 'daemon',
            'severity': 'notice',
            'hostname': 'server01',
            'tag': 'systemd',
            'process_id': 1,
            'message': 'Started User Manager for UID 1000.'
        },
        {
            'type': 'rfc5424',
            'facility': 'daemon',
            'severity': 'info',
            'hostname': 'server01',
            'app_name': 'systemd',
            'proc_id': '1',
            'msg_id': 'UNIT',
            'message': 'Starting Network Manager...'
        },
        
        # Security logs
        {
            'type': 'rfc3164',
            'facility': 'authpriv',
            'severity': 'info',
            'hostname': 'server01',
            'tag': 'sshd',
            'process_id': 12345,
            'message': 'Accepted publickey for user from 192.168.1.10 port 52341 ssh2: RSA SHA256:abc123'
        },
        {
            'type': 'rfc5424',
            'facility': 'authpriv',
            'severity': 'warning',
            'hostname': 'server01',
            'app_name': 'sshd',
            'proc_id': '12346',
            'msg_id': 'AUTH',
            'message': 'Failed password for invalid user admin from 192.168.1.50 port 52342 ssh2'
        },
        
        # Application logs
        {
            'type': 'rfc3164',
            'facility': 'local1',
            'severity': 'err',
            'hostname': 'app01',
            'tag': 'myapp',
            'process_id': 9999,
            'message': 'Database connection failed: timeout after 30 seconds'
        },
        {
            'type': 'rfc5424',
            'facility': 'local1',
            'severity': 'debug',
            'hostname': 'app01',
            'app_name': 'myapp',
            'proc_id': '9999',
            'msg_id': 'DB',
            'message': 'Query executed successfully: SELECT * FROM users WHERE id = 123'
        },
        
        # Network equipment logs
        {
            'type': 'rfc3164',
            'facility': 'local7',
            'severity': 'warning',
            'hostname': 'switch01',
            'tag': 'kernel',
            'message': '%LINK-3-UPDOWN: Interface GigabitEthernet0/1, changed state to down'
        },
        {
            'type': 'rfc5424',
            'facility': 'local7',
            'severity': 'notice',
            'hostname': 'router01',
            'app_name': 'bgp',
            'proc_id': '-',
            'msg_id': 'NEIGHBOR',
            'message': 'BGP neighbor 192.168.1.1 Up'
        }
    ]
    
    return messages

def create_message(msg_config: Dict) -> str:
    """Create syslog message based on configuration"""
    
    if msg_config['type'] == 'rfc3164':
        return create_rfc3164_message(
            facility=msg_config['facility'],
            severity=msg_config['severity'],
            hostname=msg_config['hostname'],
            tag=msg_config['tag'],
            process_id=msg_config.get('process_id'),
            message=msg_config['message']
        )
    else:  # rfc5424
        return create_rfc5424_message(
            facility=msg_config['facility'],
            severity=msg_config['severity'],
            hostname=msg_config['hostname'],
            app_name=msg_config['app_name'],
            proc_id=msg_config['proc_id'],
            msg_id=msg_config['msg_id'],
            message=msg_config['message']
        )

def send_syslog_messages(host: str, port: int, count: int, interval: float,
                        message_type: str = 'mixed', verbose: bool = False) -> None:
    """Send syslog messages to the specified host and port"""
    
    sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    messages = generate_realistic_messages()
    
    # Filter messages by type
    if message_type == 'rfc3164':
        messages = [m for m in messages if m['type'] == 'rfc3164']
    elif message_type == 'rfc5424':
        messages = [m for m in messages if m['type'] == 'rfc5424']
    # 'mixed' uses all messages
    
    try:
        print(f"Sending {count} syslog messages to {host}:{port}")
        print(f"Message type: {message_type}")
        
        for i in range(count):
            # Select message (cycle through available messages)
            msg_config = messages[i % len(messages)]
            
            # Add some randomization to timestamps and IDs
            if 'process_id' in msg_config and msg_config['process_id']:
                msg_config['process_id'] += random.randint(0, 100)
            
            message = create_message(msg_config)
            
            # Send as UDP packet
            sock.sendto(message.encode('utf-8'), (host, port))
            
            if verbose:
                print(f"Sent message {i + 1} ({msg_config['type']}): {message[:100]}...")
            
            if i < count - 1:  # Don't sleep after last message
                time.sleep(interval)
                
        print(f"Successfully sent {count} syslog messages")
        
    except Exception as e:
        print(f"Error sending messages: {e}")
        sys.exit(1)
    finally:
        sock.close()

def send_stress_test(host: str, port: int, duration: int = 60, 
                    rate: int = 100, verbose: bool = False) -> None:
    """Send high-rate syslog messages for stress testing"""
    
    sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    messages = generate_realistic_messages()
    
    print(f"Starting stress test: {rate} messages/second for {duration} seconds")
    print(f"Target: {host}:{port}")
    
    start_time = time.time()
    message_count = 0
    
    try:
        while time.time() - start_time < duration:
            batch_start = time.time()
            
            # Send batch of messages
            for _ in range(rate):
                msg_config = random.choice(messages)
                message = create_message(msg_config)
                sock.sendto(message.encode('utf-8'), (host, port))
                message_count += 1
            
            # Calculate sleep time to maintain rate
            batch_duration = time.time() - batch_start
            sleep_time = max(0, 1.0 - batch_duration)
            
            if verbose and message_count % (rate * 10) == 0:  # Log every 10 seconds
                elapsed = time.time() - start_time
                actual_rate = message_count / elapsed
                print(f"Sent {message_count} messages in {elapsed:.1f}s (rate: {actual_rate:.1f}/s)")
            
            time.sleep(sleep_time)
            
        elapsed = time.time() - start_time
        actual_rate = message_count / elapsed
        print(f"Stress test completed: {message_count} messages in {elapsed:.1f}s")
        print(f"Actual rate: {actual_rate:.1f} messages/second")
        
    except Exception as e:
        print(f"Error during stress test: {e}")
        sys.exit(1)
    finally:
        sock.close()

def validate_message(message: str) -> bool:
    """Validate syslog message format"""
    if not message.startswith('<'):
        return False
    
    # Find priority end
    end_priority = message.find('>')
    if end_priority == -1:
        return False
    
    try:
        priority = int(message[1:end_priority])
        return 0 <= priority <= 191  # Valid priority range
    except ValueError:
        return False

def main():
    parser = argparse.ArgumentParser(description='Syslog Test Script for ByteFreezer Proxy')
    parser.add_argument('--host', default='localhost', help='Proxy host (default: localhost)')
    parser.add_argument('--port', type=int, default=514, help='Proxy port (default: 514)')
    parser.add_argument('--count', type=int, default=10, help='Number of messages to send (default: 10)')
    parser.add_argument('--interval', type=float, default=1.0, help='Interval between messages in seconds (default: 1.0)')
    parser.add_argument('--type', choices=['rfc3164', 'rfc5424', 'mixed'], default='mixed',
                       help='Message format to send (default: mixed)')
    parser.add_argument('--stress', action='store_true', help='Run stress test')
    parser.add_argument('--stress-duration', type=int, default=60, help='Stress test duration in seconds (default: 60)')
    parser.add_argument('--stress-rate', type=int, default=100, help='Stress test rate (messages/second, default: 100)')
    parser.add_argument('--verbose', '-v', action='store_true', help='Verbose output')
    parser.add_argument('--validate', action='store_true', help='Validate message format before sending')
    
    args = parser.parse_args()
    
    print("Syslog Test Script")
    print("=" * 50)
    
    if args.validate:
        print("Validating message formats...")
        test_messages = generate_realistic_messages()
        for msg_config in test_messages[:2]:  # Test first two messages
            message = create_message(msg_config)
            if validate_message(message):
                print(f"✓ {msg_config['type']} message valid: {message[:50]}...")
            else:
                print(f"✗ {msg_config['type']} message invalid: {message[:50]}...")
    
    if args.stress:
        send_stress_test(
            host=args.host,
            port=args.port,
            duration=args.stress_duration,
            rate=args.stress_rate,
            verbose=args.verbose
        )
    else:
        send_syslog_messages(
            host=args.host,
            port=args.port,
            count=args.count,
            interval=args.interval,
            message_type=args.type,
            verbose=args.verbose
        )

if __name__ == "__main__":
    main()
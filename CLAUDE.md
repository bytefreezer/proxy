# ByteFreezer Proxy Service

This service runs a UDP listener to receive data line by line from external sources (syslog, eBPF clients, etc.).

## Core Functionality

1. **UDP Listener**: Receives data line by line from external sources
2. **Data Batching**: Packs data into N lines or N bytes per configuration  
3. **HTTP Forwarding**: Posts batched data to bytefreezer-receiver as compressed NDJSON
4. **URI Format**: Constructs proper URIs according to bytefreezer-receiver format
5. **Configuration**: Tenant token and dataset ID mapping for receiver compatibility

## Architecture

This service is designed to be installed on-premises for heavy UDP users, acting as a data collection and forwarding proxy to the main bytefreezer-receiver service.

## Configuration Requirements

- UDP listener host/port configuration
- Tenant token and dataset ID for bytefreezer-receiver URI format
- Batching parameters (lines/bytes)
- Bytefreezer-receiver endpoint URL
- Compression settings

## API Endpoints

- Health endpoint
- Configuration display endpoint (similar to bytefreezer-receiver)
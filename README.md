# ByteFreezer Proxy

Multi-protocol data collection proxy for the ByteFreezer platform. Proxy accepts data from various sources (UDP, Syslog, Netflow, IPFIX, HTTP, Kafka, etc.) and forwards it to the receiver service.

## Overview

ByteFreezer Proxy is the **data collection layer** in the ByteFreezer architecture:

1. **bytefreezer-proxy** (this service): Protocol data collection → Receiver
2. **bytefreezer-receiver**: HTTP webhook ingestion → S3 raw/
3. **bytefreezer-piper**: Data processing pipeline → S3 processed/
4. **bytefreezer-packer**: Parquet optimization → S3 parquet/

### Key Features

- **Multi-Protocol Support** - UDP, Syslog, Netflow, IPFIX, HTTP, Kafka, SQS, NATS, Kinesis, sFlow
- **Plugin Architecture** - Extensible plugin system for new data sources
- **Batch Processing** - Efficient batching and compression before forwarding
- **Spooling** - Crash-resilient spooling with retry logic
- **Health Reporting** - Reports status to ByteFreezer Control
- **OpenTelemetry Integration** - Comprehensive metrics and tracing

## Installation

### Docker (Recommended)

```bash
docker pull ghcr.io/bytefreezer/proxy:latest
docker run -p 8088:8088 -v $(pwd)/config.yaml:/config.yaml ghcr.io/bytefreezer/proxy:latest
```

### Build from Source

```bash
go build -o bytefreezer-proxy .
./bytefreezer-proxy --config config.yaml
```

## Configuration

```yaml
app:
  name: "bytefreezer-proxy"
  version: "1.0.0"

logging:
  level: "info"
  encoding: "json"

server:
  api_port: 8088

receiver:
  url: "http://receiver:8080"
  timeout_seconds: 30

plugins:
  udp:
    enabled: true
    port: 514
  syslog:
    enabled: true
    port: 1514
```

## API Endpoints

- `GET /api/v1/health` - Service health check
- `GET /api/v1/config` - Current configuration
- `GET /api/v1/stats` - Processing statistics

## License

ByteFreezer is licensed under the [Elastic License 2.0](LICENSE.txt).

You're free to use, modify, and self-host. You cannot offer it as a managed service.

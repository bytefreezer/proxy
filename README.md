# ByteFreezer Proxy

A UDP data collection proxy that batches and forwards data to bytefreezer-receiver.

## Overview

ByteFreezer Proxy is designed to be installed on-premises for heavy UDP users. It:
- Listens for UDP data from external sources (syslog, eBPF, etc.)
- Batches data based on configurable line count or byte size limits
- Compresses and forwards batches to bytefreezer-receiver via HTTP
- Provides health and configuration APIs

## Architecture

```
Syslog Sources---udp:2056--\
                            \
eBPF Data-------udp:2057-----> bytefreezer-proxy --HTTP--> bytefreezer-receiver
                            /
App Logs--------udp:2058---/
```

The proxy follows the same architectural patterns as bytefreezer-receiver:
- `api/` - HTTP API handlers and routing
- `config/` - Configuration management 
- `domain/` - Data models and types
- `services/` - Business logic and HTTP forwarding
- `udp/` - UDP listener and data batching
- `alerts/` - SOC alerting integration

## Configuration

The service is configured via `config.yaml` file. Key configuration sections:

### UDP Listeners
```yaml
udp:
  enabled: true
  host: "0.0.0.0"
  read_buffer_size_bytes: 134217728  # 128MB
  max_batch_lines: 100000
  max_batch_bytes: 268435456  # 256MB
  batch_timeout_seconds: 30
  enable_compression: true
  compression_level: 6
  listeners:
    - port: 2056
      dataset_id: "syslog-data"
    - port: 2057  
      dataset_id: "ebpf-data"
    - port: 2058
      dataset_id: "application-logs"
```

### Receiver Configuration  
```yaml
receiver:
  base_url: "http://localhost:8080"
  timeout_seconds: 30
  retry_count: 3
  retry_delay_seconds: 1

# Global tenant configuration
tenant_id: "customer-1"
```

### API Server
```yaml
server:
  api_port: 8088
```

### OpenTelemetry (Optional)
```yaml
otel:
  enabled: false
  endpoint: "localhost:4317"
  service_name: "bytefreezer-proxy"
  scrapeIntervalseconds: 100
```

## API Endpoints

- `GET /health` - Health check endpoint with service status
- `GET /config` - View current configuration (sensitive values masked)
- `GET /docs` - API documentation

## Building and Running

### Build from Source
```bash
# Build
go build .

# Run with default config
./bytefreezer-proxy

# The service expects config.yaml in the current directory
```

### System Requirements

Configure UDP buffer limits on the host machine to match configuration:
```bash
# For 128MB read buffer (default)
sudo sysctl -w net.core.rmem_max=134217728
sudo sysctl -w net.core.rmem_default=134217728
sudo sysctl -w net.core.wmem_max=134217728  
sudo sysctl -w net.core.wmem_default=134217728
```

## Data Format

The proxy accepts UDP data and converts it to NDJSON format before forwarding:

- Valid JSON messages are passed through as-is
- Non-JSON messages are wrapped in JSON envelopes with metadata:
  ```json
  {
    "message": "original udp data", 
    "source": "sender_ip:port",
    "timestamp": "2025-09-03T23:30:00.123Z"
  }
  ```

## URI Format

Data is forwarded to bytefreezer-receiver using the URI format:
```
POST {base_url}/data/{tenant_id}/{dataset_id}
```

Examples:
- Syslog data: `POST http://localhost:8080/data/customer-1/syslog-data`
- eBPF data: `POST http://localhost:8080/data/customer-1/ebpf-data`
- App logs: `POST http://localhost:8080/data/customer-1/application-logs`

## Monitoring

The service provides metrics and health information:

- Health endpoint shows service status, configuration, and statistics
- OpenTelemetry integration for metrics and tracing
- SOC alerting for operational issues
- Structured logging with configurable levels

## Error Handling

- Automatic retry with exponential backoff for failed forwards
- SOC alerting for persistent failures
- Graceful handling of oversized payloads
- Connection pooling and timeout management
# ByteFreezer Proxy

High-performance multi-protocol data streaming proxy for the ByteFreezer platform. Designed for enterprise environments that need to collect, batch, and forward data from diverse sources including UDP, HTTP webhooks, Kafka, and NATS to the ByteFreezer platform for storage and downstream processing.

## Overview

ByteFreezer Proxy is a **universal data streaming gateway** designed for enterprise environments with diverse data sources. The proxy implements a **plugin-based architecture** with a **stream-first, process-later** design that maximizes throughput while enabling sophisticated downstream processing.

### **Core Capabilities**
- **🔌 Plugin Architecture**: Extensible input system supporting UDP, HTTP, Kafka, NATS, and custom plugins
- **🌐 Universal Data Ingestion**: Accepts any line-based data format from multiple sources
- **⚡ High-Performance Streaming**: Optimized for high-throughput data collection
- **📦 Smart Batching**: Efficient line-based batching with gzip compression
- **🔄 Reliable Delivery**: HTTP forwarding with retry logic and local spooling
- **🏢 Multi-Tenant Architecture**: Isolated data streams with per-tenant authentication
- **📊 Protocol Intelligence**: Optional lightweight parsing for structured protocols
- **🛡️ Enterprise-Ready**: Health monitoring, metrics, and operational APIs

### **Supported Input Plugins**
| Plugin | Protocol | Use Cases |
|--------|----------|-----------|
| **UDP** | UDP/Syslog/NetFlow/sFlow | High-throughput streaming, network monitoring |
| **HTTP** | HTTP Webhooks | Lightweight data submission, API integrations |
| **Kafka** | Apache Kafka | Message queue integration, event streaming |
| **NATS** | NATS messaging | Pub/sub patterns, microservice communication |

### **Supported Data Formats**
| Format | Processing | Use Cases |
|--------|------------|-----------|
| **JSON Logs** | Pass-through | Application logs, structured events |
| **Plain Text** | Metadata wrapping | Legacy logs, free-form messages |
| **CSV/TSV** | Pass-through | Metrics, tabular data exports |
| **Syslog** | Structure extraction | System logs, network device logs |
| **NetFlow/IPFIX** | Binary parsing | Network monitoring, traffic analysis |
| **sFlow** | Binary parsing | Network sampling, performance monitoring |

## Quick Start

### Docker (Recommended)

```bash
# Pull the latest image
docker pull ghcr.io/n0needt0/bytefreezer-proxy:latest

# Run with default configuration
docker run -p 8088:8088 -p 2056:2056/udp -p 8081:8081 \
  ghcr.io/n0needt0/bytefreezer-proxy:latest
```

### Configuration Example

Create a `config.yaml` file:

```yaml
# Global tenant configuration
tenant_id: "my-company"
bearer_token: "your-bearer-token"

# Plugin-based input configuration
inputs:
  # UDP Plugin - High-throughput streaming
  - type: "udp"
    name: "syslog-listener"
    config:
      host: "0.0.0.0"
      port: 2056
      dataset_id: "syslog-data"
      protocol: "syslog"
      
  # HTTP Plugin - Webhook endpoints
  - type: "http"
    name: "webhook-endpoint"
    config:
      host: "0.0.0.0"
      port: 8081
      path: "/webhook"
      dataset_id: "webhook-data"
      max_payload_size: 10485760   # 10MB
      max_lines_per_request: 1000

# Receiver configuration
receiver:
  base_url: "http://your-receiver:8080/data/{tenantid}/{datasetid}"
  timeout_seconds: 30
  retry_count: 3

# API server
server:
  api_port: 8088

# Local spooling for reliability
spooling:
  enabled: true
  directory: "/var/spool/bytefreezer-proxy"
  max_size_bytes: 1073741824  # 1GB
```

### Test the Setup

```bash
# Test UDP syslog
echo "<134>$(date '+%b %d %H:%M:%S') myhost myapp: Test message" | nc -u localhost 2056

# Test HTTP webhook
curl -X POST http://localhost:8081/webhook \
  -H "Content-Type: text/plain" \
  -d "Test webhook message"

# Check health
curl http://localhost:8088/api/v2/health
```

## Building from Source

### Prerequisites
- Go 1.21 or later
- Make (optional)

### Build Steps

```bash
# Clone the repository
git clone https://github.com/n0needt0/bytefreezer-proxy.git
cd bytefreezer-proxy

# Build the binary
go build .

# Or use the build script
./build_local.sh

# Run with configuration
./bytefreezer-proxy --config config.yaml
```

### Build Options

```bash
# Validate configuration only
./bytefreezer-proxy --validate-config --config config.yaml

# Show version
./bytefreezer-proxy --version

# Show help
./bytefreezer-proxy --help
```

## Configuration

The proxy uses a YAML configuration file with plugin-based input definitions.

### Global Settings

```yaml
# Application metadata
app:
  name: bytefreezer-proxy
  version: 0.0.2

# Logging configuration
logging:
  level: info              # debug, info, warn, error
  encoding: console        # console, json

# API server
server:
  api_port: 8088

# Global tenant (used as fallback)
tenant_id: "your-tenant"
bearer_token: "your-token"

# Receiver endpoint
receiver:
  base_url: "http://receiver:8080/data/{tenantid}/{datasetid}"
  timeout_seconds: 30
  retry_count: 3
  retry_delay_seconds: 1
```

### Plugin Configuration

Each input plugin is configured in the `inputs` array:

```yaml
inputs:
  - type: "udp"              # Plugin type
    name: "unique-name"      # Unique identifier
    config:                  # Plugin-specific configuration
      host: "0.0.0.0"
      port: 2056
      dataset_id: "my-data"
      # Plugin-specific options...
```

#### UDP Plugin

```yaml
- type: "udp"
  name: "syslog-udp"
  config:
    host: "0.0.0.0"              # Bind address
    port: 2056                   # UDP port
    dataset_id: "syslog-data"    # Dataset identifier
    tenant_id: "custom-tenant"   # Optional: override global
    bearer_token: "token"        # Optional: override global
    protocol: "syslog"           # udp, syslog, netflow, sflow
    syslog_mode: "rfc3164"       # rfc3164, rfc5424
    read_buffer_size: 65536      # Buffer size in bytes
    worker_count: 4              # Number of worker goroutines
```

#### HTTP Plugin

```yaml
- type: "http"
  name: "webhook-api"
  config:
    host: "0.0.0.0"              # Bind address
    port: 8081                   # HTTP port
    path: "/webhook"             # Endpoint path
    dataset_id: "webhook-data"   # Dataset identifier
    tenant_id: "custom-tenant"   # Optional: override global
    bearer_token: "token"        # Optional: override global
    max_payload_size: 10485760   # 10MB limit
    max_lines_per_request: 1000  # Line count limit
    read_timeout: 30             # Read timeout (seconds)
    write_timeout: 30            # Write timeout (seconds)
    enable_authentication: true  # Require bearer token
```

#### Kafka Plugin

```yaml
- type: "kafka"
  name: "kafka-consumer"
  config:
    brokers: ["kafka1:9092", "kafka2:9092"]
    topics: ["app-logs", "metrics"]
    group_id: "bytefreezer-proxy"
    dataset_id: "kafka-data"
    tenant_id: "custom-tenant"   # Optional: override global
    bearer_token: "token"        # Optional: override global
    auto_offset_reset: "latest"  # earliest, latest
    session_timeout: 30          # Session timeout (seconds)
    heartbeat_interval: 10       # Heartbeat interval (seconds)
```

#### NATS Plugin

```yaml
- type: "nats"
  name: "nats-subscriber"
  config:
    servers: ["nats://nats1:4222", "nats://nats2:4222"]
    subjects: ["logs.>", "metrics.>"]
    queue_group: "bytefreezer"
    dataset_id: "nats-data"
    tenant_id: "custom-tenant"   # Optional: override global
    bearer_token: "token"        # Optional: override global
    max_reconnect: -1            # Unlimited reconnects
    reconnect_wait: 2            # Reconnect wait (seconds)
```

### Spooling Configuration

For reliable data delivery during network issues:

```yaml
spooling:
  enabled: true
  directory: "/var/spool/bytefreezer-proxy"
  max_size_bytes: 1073741824     # 1GB total limit
  retry_attempts: 5              # Max retry attempts
  retry_interval_seconds: 60     # Retry interval
  cleanup_interval_seconds: 300  # Background cleanup
  organization: "tenant_dataset" # flat, tenant_dataset, date_tenant
  per_tenant_limits: false       # Per-tenant vs global limits
  max_files_per_dataset: 1000    # File count limit
  max_age_days: 7                # Age-based cleanup
```

### Monitoring Configuration

```yaml
# OpenTelemetry metrics (optional)
otel:
  enabled: false
  service_name: "bytefreezer-proxy"
  prometheus_mode: true
  metrics_port: 9090
  metrics_host: "0.0.0.0"

# SOC alerting (optional)
soc:
  enabled: false
  endpoint: "https://your-soc-endpoint"
  timeout: 30
```

## Installation

### Docker Deployment

#### Docker Compose

```yaml
version: '3.8'
services:
  bytefreezer-proxy:
    image: ghcr.io/n0needt0/bytefreezer-proxy:latest
    ports:
      - "8088:8088"     # API
      - "2056:2056/udp" # UDP
      - "8081:8081"     # HTTP
    volumes:
      - ./config.yaml:/config.yaml
      - ./spool:/var/spool/bytefreezer-proxy
    restart: unless-stopped
```

#### Docker Run

```bash
# Create spool directory
mkdir -p ./spool

# Run with mounted config and spool
docker run -d \
  --name bytefreezer-proxy \
  -p 8088:8088 \
  -p 2056:2056/udp \
  -p 8081:8081 \
  -v $(pwd)/config.yaml:/config.yaml \
  -v $(pwd)/spool:/var/spool/bytefreezer-proxy \
  ghcr.io/n0needt0/bytefreezer-proxy:latest
```

### AWX/Ansible Deployment

```bash
# Clone repository
git clone https://github.com/n0needt0/bytefreezer-proxy.git
cd bytefreezer-proxy/ansible/playbooks

# Configure inventory
cp inventory.yml.example inventory.yml
# Edit inventory.yml with your hosts

# Deploy via GitHub releases
ansible-playbook -i inventory.yml install.yml

# Or deploy via Docker extraction
ansible-playbook -i inventory.yml docker_install.yml

# Remove deployment
ansible-playbook -i inventory.yml remove.yml
```

### Helm Deployment

```bash
# Add Helm repository (if available)
helm repo add bytefreezer https://charts.bytefreezer.io
helm repo update

# Install with custom values
helm install bytefreezer-proxy bytefreezer/bytefreezer-proxy \
  --set config.tenant_id="my-company" \
  --set config.bearer_token="my-token" \
  --set service.ports.udp=2056 \
  --set service.ports.http=8081

# Or install from local chart
cd helm/bytefreezer-proxy
helm install bytefreezer-proxy . -f values.yaml
```

### GitHub Actions CI/CD

The project includes automated CI/CD workflows:

- **Build**: Builds and tests on every commit
- **Release**: Creates GitHub releases with binaries
- **Docker**: Builds and pushes Docker images
- **Security**: Runs security scans

See `.github/workflows/` for workflow definitions.

## Testing

### End-to-End Testing

Integration tests are available in the `integration-tests/` directory:

```bash
# Run all integration tests
cd integration-tests
./run-all-tests.sh

# Test specific plugins
./test-udp-plugin.sh
./test-http-plugin.sh
./test-kafka-plugin.sh
./test-nats-plugin.sh

# Test with monitoring enabled
./test-with-monitoring.sh
```

### Manual Testing

#### UDP Plugin

```bash
# Test syslog (RFC3164)
echo "<134>$(date '+%b %d %H:%M:%S') myhost myapp: Test syslog message" | nc -u localhost 2056

# Test NetFlow (requires NetFlow generator)
# Test sFlow (requires sFlow generator)

# Test plain UDP
echo "Plain text log message" | nc -u localhost 2056
```

#### HTTP Plugin

```bash
# Test webhook endpoint
curl -X POST http://localhost:8081/webhook \
  -H "Content-Type: text/plain" \
  -H "Authorization: Bearer your-token" \
  -d "Test webhook message"

# Test with JSON data
curl -X POST http://localhost:8081/webhook \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer your-token" \
  -d '{"timestamp":"2024-01-01T00:00:00Z","level":"info","message":"Test"}'

# Test health endpoint
curl http://localhost:8081/health
```

#### Kafka Plugin

```bash
# Send test message to Kafka topic
echo '{"test": "message"}' | kafka-console-producer.sh \
  --broker-list localhost:9092 \
  --topic app-logs

# Monitor consumer group
kafka-consumer-groups.sh \
  --bootstrap-server localhost:9092 \
  --group bytefreezer-proxy \
  --describe
```

#### NATS Plugin

```bash
# Send test message to NATS subject
nats pub logs.app '{"test": "message"}'

# Monitor queue group
nats sub logs.> --queue bytefreezer
```

### Health Checks

```bash
# API health check
curl http://localhost:8088/api/v2/health

# Plugin-specific health
curl http://localhost:8088/api/v2/config

# Spooling statistics
curl http://localhost:8088/api/v2/dlq/stats
```

## Monitoring

See [monitoring.md](monitoring.md) for comprehensive monitoring setup including:

- Prometheus metrics collection
- Grafana dashboards
- Alert rules and notifications
- Performance monitoring
- Log aggregation

### Quick Monitoring Setup

```yaml
# Enable OTEL metrics in config.yaml
otel:
  enabled: true
  prometheus_mode: true
  metrics_port: 9090
  metrics_host: "0.0.0.0"
```

```bash
# Access metrics endpoint
curl http://localhost:9090/metrics

# Key metrics to monitor:
# - bytefreezer_proxy_input_bytes_total
# - bytefreezer_proxy_input_lines_total  
# - bytefreezer_proxy_batch_processing_duration
# - bytefreezer_proxy_http_requests_total
```

## For Developers

### Creating Custom Input Plugins

ByteFreezer Proxy supports custom input plugins. See `plugins-examples/` directory for:

- **ZMQ Plugin Example**: Complete implementation of a ZeroMQ input plugin
- **Plugin Development Guide**: Step-by-step guide for creating new plugins
- **Plugin Template**: Boilerplate code for new plugins
- **Testing Framework**: Testing utilities for plugin development

#### Quick Plugin Development

1. **Create Plugin Structure**:
   ```bash
   mkdir plugins/zmq
   cd plugins/zmq
   ```

2. **Implement Plugin Interface**:
   ```go
   type Plugin struct {
       // Plugin implementation
   }
   
   func (p *Plugin) Name() string { return "zmq" }
   func (p *Plugin) Configure(config map[string]interface{}) error { /* ... */ }
   func (p *Plugin) Start(ctx context.Context, output chan<- *plugins.DataMessage) error { /* ... */ }
   func (p *Plugin) Stop() error { /* ... */ }
   func (p *Plugin) Health() plugins.PluginHealth { /* ... */ }
   ```

3. **Register Plugin**:
   ```go
   // plugins/zmq/init.go
   func init() {
       plugins.GetRegistry().Register("zmq", NewPlugin)
   }
   ```

4. **Import in Main**:
   ```go
   // main.go
   _ "github.com/n0needt0/bytefreezer-proxy/plugins/zmq"
   ```

See the complete ZMQ example in `plugins-examples/zmq/` for a fully functional implementation.

## Architecture

### Plugin System Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    ByteFreezer Proxy                        │
├─────────────────────────────────────────────────────────────┤
│              Plugin Manager & Registry                      │
├─────────────────────────────────────────────────────────────┤
│  UDP Plugin  │ HTTP Plugin │ Kafka Plugin │ NATS Plugin    │
│  ┌─────────┐ │ ┌─────────┐ │ ┌─────────┐  │ ┌─────────┐   │
│  │ Syslog  │ │ │Webhooks │ │ │Consumer │  │ │Subscribe│   │
│  │ NetFlow │ │ │Auth     │ │ │Groups   │  │ │Topics   │   │
│  │ sFlow   │ │ │Limits   │ │ │Topics   │  │ │Queues   │   │
│  └─────────┘ │ └─────────┘ │ └─────────┘  │ └─────────┘   │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│              Batch Processor & Forwarder                   │
│  • Line-based batching  • Compression  • HTTP delivery     │
│  • Multi-tenant routing • Retry logic  • Local spooling    │
└─────────────────────────────────────────────────────────────┘
```

### Data Flow

```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│   Data Sources  │───▶│  ByteFreezer    │───▶│   ByteFreezer   │
│                 │    │     Proxy       │    │    Platform     │
│ • UDP Streams   │    │                 │    │                 │
│ • HTTP Webhooks │    │ • Plugin System │    │ • Store (S3)    │
│ • Kafka Topics  │    │ • Batch & Route │    │ • Process       │
│ • NATS Messages │    │ • Forward       │    │ • Analyze       │
│ • Custom Sources│    │                 │    │                 │
└─────────────────┘    └─────────────────┘    └─────────────────┘
```

## API Reference

### Health Endpoint
- `GET /api/v2/health` - Service health and status

### Configuration Endpoint  
- `GET /api/v2/config` - Current configuration (sensitive values masked)

### DLQ Management
- `GET /api/v2/dlq/stats` - Dead Letter Queue statistics
- `POST /api/v2/dlq/retry` - Retry failed messages

### Documentation
- `GET /v2/docs` - Interactive API documentation (Swagger UI)

## Troubleshooting

### Common Issues

**Plugin Registration Errors**
```bash
# Error: "unknown plugin type: zmq"
# Solution: Ensure plugin is imported in main.go
_ "github.com/n0needt0/bytefreezer-proxy/plugins/zmq"
```

**Configuration Validation Failures**
```bash
# Error: Invalid tenant_id/dataset_id
# Solution: Use only alphanumeric characters, hyphens, underscores
tenant_id: "valid-tenant-name"  # ✅
dataset_id: "valid_dataset"     # ✅
```

**Network Connectivity Issues**
```bash
# Check if proxy is listening
netstat -tulpn | grep :8088

# Test connectivity to receiver
curl http://receiver:8080/api/v1/health

# Check spooling for failed batches
ls -la /var/spool/bytefreezer-proxy/
```

**High Memory Usage**
```bash
# Reduce batch sizes in configuration
max_batch_lines: 10000      # Reduce from default
max_batch_bytes: 10485760   # Reduce from default

# Enable compression
enable_compression: true
compression_level: 1        # Fast compression
```

### Performance Tuning

**UDP Buffer Optimization**
```bash
# Increase system UDP buffers
sudo sysctl -w net.core.rmem_max=134217728
sudo sysctl -w net.core.wmem_max=134217728
```

**Plugin-Specific Tuning**
```yaml
# UDP Plugin
worker_count: 8              # Match CPU cores
read_buffer_size: 131072     # Larger buffers for high volume

# HTTP Plugin  
read_timeout: 10             # Faster timeouts
write_timeout: 10

# Kafka Plugin
session_timeout: 60          # Longer sessions for stability
heartbeat_interval: 20

# NATS Plugin
max_reconnect: 10            # Limit reconnect attempts
reconnect_wait: 5            # Longer wait between attempts
```

## Contributing

1. Fork the repository
2. Create a feature branch: `git checkout -b feature/new-plugin`
3. Commit changes: `git commit -am 'Add new plugin'`
4. Push to branch: `git push origin feature/new-plugin`  
5. Submit a Pull Request

### Development Guidelines

- Follow Go best practices and conventions
- Add comprehensive tests for new features
- Update documentation for any API changes
- Ensure plugins follow the established interface
- Add integration tests for new plugins

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## Support

- **Documentation**: See `docs/` directory for detailed guides
- **Issues**: Report bugs and request features on GitHub
- **Discussions**: Community discussions on GitHub Discussions
- **Examples**: Plugin examples in `plugins-examples/` directory
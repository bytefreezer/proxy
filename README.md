# ByteFreezer Proxy

High-performance UDP log proxy for the ByteFreezer platform. This proxy is required to collect data internal to your network efficiently via UDP, and forward it effectively via TCP to the ByteFreezer receiver.

## Overview

ByteFreezer Proxy is designed to be installed on-premises for heavy UDP users. It:
- **Multi-protocol support**: Raw UDP, RFC3164/RFC5424 syslog, NetFlow v5/v9/IPFIX, and sFlow v5 parsing
- **Supports up to 10 concurrent datasets** on dedicated UDP ports (2056-2065)
- **Intelligent syslog parsing**: Automatic RFC3164/RFC5424 detection and JSON conversion
- **Smart batching**: Line count takes precedence, with byte limits as additional constraints
- Compresses and forwards batches to bytefreezer-receiver via HTTP
- **Multi-tenant support**: Port-based tenant isolation with per-tenant authentication
- Provides health and configuration APIs

### Port Allocation & Protocol Architecture

ByteFreezer Proxy uses a **pre-allocated port range** system with **multi-protocol support**:

- **Port Range**: 2056-2065 (10 ports reserved for datasets)
- **Protocol Support**: Each port can be configured as `udp` (raw), `syslog` (parsed), `netflow`, or `sflow`
- **Smart Activation**: Only ports with configured `dataset_id` values are bound and consume resources
- **Zero-Resource Inactive Ports**: Unconfigured ports remain completely inactive
- **Per-Dataset Isolation**: Each dataset gets its own dedicated port and processing pipeline
- **Multi-Format Parsing**: Syslog (RFC3164/RFC5424), NetFlow (v5/v9/IPFIX), sFlow (v5), all with automatic JSON conversion
- **Scalable Design**: Easily expand from 3 to 10 datasets by uncommenting configuration lines

#### Protocol Configuration Examples:
```yaml
listeners:
  - port: 2056
    dataset_id: "syslog-messages" 
    protocol: "syslog"
    syslog_mode: "rfc3164"
  
  - port: 2057
    dataset_id: "raw-udp-data"
    protocol: "udp"  # Default
    
  - port: 2058
    dataset_id: "netflow-data"
    protocol: "netflow"  # NetFlow v5/v9/IPFIX
    
  - port: 2059
    dataset_id: "sflow-data" 
    protocol: "sflow"   # sFlow v5
```

## Installation

### Docker (Recommended)

Pull and run the latest version:
```bash
# Pull the latest image
docker pull ghcr.io/n0needt0/bytefreezer-proxy:latest

# Run with default configuration (3 active ports)
docker run -p 8088:8088 -p 2056-2065:2056-2065/udp ghcr.io/n0needt0/bytefreezer-proxy:latest
```

With custom configuration:
```bash
# Create your config file
wget https://raw.githubusercontent.com/n0needt0/bytefreezer-proxy/main/config.yaml

# Run with custom config (expose all potential UDP ports)
docker run -p 8088:8088 -p 2056-2065:2056-2065/udp -v $(pwd)/config.yaml:/config.yaml ghcr.io/n0needt0/bytefreezer-proxy:latest
```

### Binary Extraction

Extract the binary from the container for direct use:
```bash
# Extract binary
docker run --rm -v $(pwd):/output ghcr.io/n0needt0/bytefreezer-proxy:latest sh -c "cp /bytefreezer-proxy /output/"

# Make executable and run
chmod +x bytefreezer-proxy
./bytefreezer-proxy --config config.yaml
```

### Production Deployment

**Ansible (Recommended):**

*Two installation methods available:*

```bash
# Clone repository
git clone https://github.com/n0needt0/bytefreezer-proxy.git
cd bytefreezer-proxy/ansible/playbooks

# Method 1: Install from GitHub releases (default)
ansible-playbook -i inventory.yml install.yml

# Method 2: Install from Docker image (requires Docker on target hosts)
# More reliable, uses same binary as containers
ansible-playbook -i inventory.yml docker_install.yml

# Remove service (works with both installation methods)
ansible-playbook -i inventory.yml remove.yml
```

**Kubernetes with MetalLB:**
```bash
# Deploy to Kubernetes cluster
cd bytefreezer-proxy/ansible/playbooks/kubernetes
ansible-playbook -i localhost, deploy.yml

# Or use kubectl directly
kubectl apply -k ../../kubernetes/
```

**Docker Compose:**
```yaml
version: '3.8'
services:
  bytefreezer-proxy:
    image: ghcr.io/n0needt0/bytefreezer-proxy:latest
    ports:
      - "8088:8088"
      # All 10 potential UDP ports (only configured ones will be active)
      - "2056:2056/udp"
      - "2057:2057/udp"
      - "2058:2058/udp"
      - "2059:2059/udp"
      - "2060:2060/udp"
      - "2061:2061/udp"
      - "2062:2062/udp"
      - "2063:2063/udp"
      - "2064:2064/udp"
      - "2065:2065/udp"
    volumes:
      - ./config.yaml:/config.yaml
      - ./logs:/var/log/bytefreezer-proxy
    restart: unless-stopped
```

## Architecture



```
these are example mappings.

Syslog Sources---udp:2056--\
                            \
eBPF Data-------udp:2057-----> bytefreezer-proxy --HTTP--> bytefreezer-receiver
                            /
App Logs--------udp:2058---/
```

*These are example mappings - configure any data type on any port via `config.yaml`*

The proxy follows the same architectural patterns as bytefreezer-receiver:
- `api/` - HTTP API handlers and routing
- `config/` - Configuration management 
- `domain/` - Data models and types
- `services/` - Business logic and HTTP forwarding
- `udp/` - UDP listener and data batching
- `alerts/` - SOC alerting integration
- `syslog/` - RFC3164/RFC5424 syslog parsing
- `netflow/` - NetFlow v5/v9/IPFIX parsing
- `sflow/` - sFlow v5 parsing

## Syslog Integration

ByteFreezer Proxy provides native syslog server capabilities for enterprise log collection:

### Supported Standards
- **RFC3164** (Traditional): `<priority>timestamp hostname tag: message`
- **RFC5424** (Modern): `<priority>version timestamp hostname app-name procid msgid structured-data message`

### Usage Examples

**Configure rsyslog to forward to proxy:**
```bash
# Add to /etc/rsyslog.conf
*.info @proxy-host:2056    # RFC3164 to port 2056
*.* @@proxy-host:2058     # RFC5424 to port 2058 (TCP-style over UDP)
```

**Configure syslog-ng to forward to proxy:**
```bash
destination bytefreezer_proxy {
    syslog("proxy-host" port(2056) transport("udp"));
};
log { source(s_src); destination(bytefreezer_proxy); };
```

**Test with logger command:**
```bash
# Send RFC3164 message
logger -n proxy-host -P 2056 -p local0.info "Test syslog message"

# Send with specific facility/severity
logger -n proxy-host -P 2056 -p daemon.warn "System daemon warning"
```

### Syslog Processing Flow

1. **Receive** syslog packet on configured port
2. **Parse** according to RFC3164 or RFC5424 standards
3. **Convert** to structured JSON format
4. **Batch** with other messages for efficiency
5. **Forward** to bytefreezer-receiver via HTTP webhook
6. **Store** in S3 as `raw/format=syslog/tenant=.../dataset=...`

### JSON Output Format

Parsed syslog messages are converted to JSON:
```json
{
  "priority": 13,
  "facility": 1,
  "severity": 5,
  "timestamp": "2024-01-15T10:05:30Z",
  "hostname": "web01",
  "tag": "nginx",
  "process_id": "1234",
  "message": "192.168.1.1 GET /api/status HTTP/1.1 200",
  "format": "rfc3164",
  "raw": "<13>Jan 15 10:05:30 web01 nginx[1234]: 192.168.1.1 GET /api/status HTTP/1.1 200",
  "metadata": {
    "parsed_at": "2024-01-15T10:05:30.123Z",
    "parser": "rfc3164"
  }
}
```

## NetFlow Integration

ByteFreezer Proxy provides comprehensive NetFlow collection and parsing capabilities for network monitoring:

### Supported NetFlow Versions
- **NetFlow v5**: Fixed format with basic flow information
- **NetFlow v9**: Template-based format with flexible field definitions
- **IPFIX (v10)**: Standards-based IP Flow Information Export

### NetFlow Configuration Example
```yaml
udp:
  listeners:
    - port: 2055  # Standard NetFlow port
      dataset_id: "netflow-data"
      protocol: "netflow"
```

### Exporter Configuration Examples

**Cisco Router/Switch:**
```
! Enable NetFlow v9 on interface
interface GigabitEthernet0/1
 ip flow ingress
 ip flow egress

! Configure NetFlow export
ip flow-export version 9
ip flow-export destination 192.168.1.100 2055
ip flow-export source GigabitEthernet0/0
```

**pfSense NetFlow Export:**
```
Status > System Logs > Settings
- Enable "Firewall Log Entries"
- Remote Log Servers: 192.168.1.100:2055
- IP Protocol: UDP
```

**SoftFlow Export (Linux):**
```bash
# Install softflowd
apt-get install softflowd

# Configure and start
softflowd -i eth0 -n 192.168.1.100:2055 -v 9 -t maxlife=60
```

### NetFlow JSON Output Format
```json
{
  "version": 5,
  "src_ip": "192.168.1.10",
  "dst_ip": "10.0.1.5",
  "src_port": 80,
  "dst_port": 52341,
  "protocol": 6,
  "packets": 15,
  "bytes": 1024,
  "flow_start": "2024-01-15T10:30:45Z",
  "flow_end": "2024-01-15T10:31:12Z",
  "input_interface": 1,
  "output_interface": 2,
  "next_hop": "192.168.1.1",
  "src_as": 65001,
  "dst_as": 65002,
  "tcp_flags": 24,
  "tos": 0,
  "received_at": "2024-01-15T10:31:15.123Z",
  "exporter_addr": "192.168.1.254"
}
```

## sFlow Integration

ByteFreezer Proxy supports sFlow v5 for network and system monitoring via packet sampling:

### sFlow v5 Features
- **Flow Samples**: Packet header sampling with network metadata
- **Counter Samples**: Interface and system statistics
- **Multi-layer Analysis**: Ethernet, IP, TCP/UDP protocol parsing

### sFlow Configuration Example
```yaml
udp:
  listeners:
    - port: 6343  # Standard sFlow port
      dataset_id: "sflow-data"
      protocol: "sflow"
```

### sFlow Agent Configuration Examples

**Open vSwitch (OVS):**
```bash
# Enable sFlow on bridge
ovs-vsctl -- set Bridge br0 sflow=@sf \
  -- --id=@sf create sFlow agent=eth0 \
     target="192.168.1.100:6343" \
     header=128 sampling=64 polling=10
```

**sFlowTrend Agent:**
```bash
# Install sFlow agent
wget https://host-sflow.sourceforge.io/sflow-agent.tar.gz
tar -xzf sflow-agent.tar.gz && cd sflow-agent
make && sudo make install

# Configure /etc/sflow/sflowagent.conf
sflow {
  collector {
    ip = 192.168.1.100
    udpport = 6343
  }
  sampling = 400
  polling = 30
}
```

**Cumulus Linux:**
```bash
# Enable sFlow
net add sflow agent interface eth0
net add sflow collector ip 192.168.1.100
net add sflow collector port 6343
net add sflow sampling-rate 1000
net commit
```

### sFlow JSON Output Format

**Flow Sample:**
```json
{
  "type": "flow",
  "version": 5,
  "agent_addr": "192.168.1.254",
  "sample_type": "flow_sample",
  "sampling_rate": 1000,
  "input_interface": 1,
  "output_interface": 2,
  "src_ip": "192.168.1.10",
  "dst_ip": "10.0.1.5",
  "src_port": 80,
  "dst_port": 52341,
  "protocol": 6,
  "packet_size": 1500,
  "src_mac": "aa:bb:cc:dd:ee:ff",
  "dst_mac": "11:22:33:44:55:66",
  "vlan": 100,
  "received_at": "2024-01-15T10:31:15.123Z"
}
```

**Counter Sample:**
```json
{
  "type": "counter", 
  "version": 5,
  "agent_addr": "192.168.1.254",
  "sample_type": "counter_sample",
  "counter_records": {
    "counter_1": {
      "format": 1,
      "length": 88,
      "data_hex": "..."
    }
  },
  "received_at": "2024-01-15T10:31:15.123Z"
}
```

### Network Flow Processing Pipeline

1. **Receive** NetFlow/sFlow packets on configured ports
2. **Parse** according to version-specific format (v5/v9/IPFIX for NetFlow, v5 for sFlow)
3. **Extract** flow information, packet samples, and interface counters
4. **Convert** to structured JSON format with network metadata
5. **Batch** flow records for efficient processing
6. **Forward** to bytefreezer-receiver via HTTP webhook
7. **Store** in S3 as `raw/format=netflow/` or `raw/format=sflow/`

### Common Network Flow Use Cases

- **Bandwidth Analysis**: Top talkers, traffic patterns, utilization monitoring
- **Security Monitoring**: DDoS detection, anomalous traffic patterns, threat hunting
- **Capacity Planning**: Interface utilization trends, growth projections
- **Application Performance**: Response times, connection patterns, QoS analysis
- **Compliance**: Traffic auditing, data retention, regulatory reporting

## Configuration

The service is configured via `config.yaml` file. Key configuration sections:

### UDP Listeners

**Up to 10 datasets supported on ports 2056-2065. Only ports with dataset_id configured will be active:**

```yaml
udp:
  enabled: true
  host: "0.0.0.0"
  read_buffer_size_bytes: 134217728  # 128MB
  max_batch_lines: 1000000           # Primary limit - batches sent when line count reached
  max_batch_bytes: 268435456         # 256MB - Additional constraint, whichever limit hit first  
  batch_timeout_seconds: 30
  enable_compression: true
  compression_level: 6
  
  # Port configuration with dataset mapping (up to 10 datasets supported)
  # Only ports with dataset_id configured will be activated
  # Ports 2056-2065 are pre-allocated for dataset use
  listeners:
    - port: 2056
      dataset_id: "syslog-data"
      # tenant_id: "custom-tenant"        # Optional: override global tenant
      # bearer_token: "custom-token"      # Optional: override global bearer token
    - port: 2057  
      dataset_id: "ebpf-data"
    - port: 2058
      dataset_id: "application-logs"
    # Additional dataset slots (uncomment and configure as needed):
    # - port: 2059
    #   dataset_id: "security-logs"
    # - port: 2060
    #   dataset_id: "network-data"
    # - port: 2061
    #   dataset_id: "performance-metrics"
    # - port: 2062
    #   dataset_id: "audit-logs"
    # - port: 2063
    #   dataset_id: "container-logs"
    # - port: 2064
    #   dataset_id: "database-logs"
    # - port: 2065
    #   dataset_id: "custom-dataset"
```

#### Expanding Beyond 3 Datasets

**Default Configuration**: 3 active datasets on ports 2056-2058

**To add more datasets (up to 10 total)**:
1. Uncomment additional port configurations in `config.yaml`
2. Set unique `dataset_id` values for each new port
3. Restart the service - new ports will automatically activate
4. Ensure firewall/container ports are exposed for the new UDP ports

**Important Limits**:
- **Maximum**: 10 concurrent datasets (ports 2056-2065)
- **Minimum**: 1 dataset (any single port from the range)
- **Port Range**: Fixed at 2056-2065 (cannot use other ports)
- **Resource Usage**: Only configured ports consume memory/CPU

**Example: Adding a 4th dataset**:
```yaml
# Add to your listeners array:
- port: 2059
  dataset_id: "security-logs"  # Must be unique and non-empty
```

#### Installation Method Comparison

| Method | Playbook | Requirements | Benefits |
|--------|----------|--------------|----------|
| **GitHub Releases** | `install.yml` | Internet access | Standard, works everywhere |
| **Docker Extraction** | `docker_install.yml` | Docker installed | More reliable, consistent with containers |

**Use Docker extraction when**:
- Hosts already have Docker installed
- You want identical binaries to your container deployments
- GitHub releases are blocked or unreliable in your environment

**Use GitHub releases when**:
- Clean hosts without Docker
- Minimal dependencies preferred
- Air-gapped environments (download releases separately)

### Multi-Tenant Configuration

ByteFreezer Proxy supports multi-tenant deployments where different tenants can have separate authentication credentials and port isolation:

#### Per-Listener Tenant & Authentication Override

Each UDP listener can override both `tenant_id` and `bearer_token` for tenant-specific authentication:

```yaml
udp:
  listeners:
    # Tenant A: Financial Services
    - port: 2056
      dataset_id: "financial-logs"
      tenant_id: "financial-corp"
      bearer_token: "financial-corp-bearer-token"
      protocol: "syslog"
      syslog_mode: "rfc5424"
    
    # Tenant B: Healthcare Organization  
    - port: 2057
      dataset_id: "healthcare-data"
      tenant_id: "healthcare-org"
      bearer_token: "healthcare-org-bearer-token"
      protocol: "udp"
    
    # Tenant C: Uses global tenant & token (fallback)
    - port: 2058
      dataset_id: "default-tenant-logs"
      protocol: "syslog"

# Global configuration (used as fallback for listeners without tenant_id/bearer_token)
tenant_id: "default-tenant"
bearer_token: "default-bearer-token"
```

#### Multi-Tenant Benefits

- **Port-based Isolation**: Each tenant gets dedicated UDP ports for network segregation
- **Separate Authentication**: Different bearer tokens per tenant for security isolation
- **Independent Data Flows**: Each tenant's data is processed and forwarded independently
- **Tenant-specific Spooling**: Failed uploads are spooled with tenant context for proper recovery
- **Monitoring & Metrics**: All metrics are tagged with tenant information for observability

#### Authentication Flow

1. **Global Fallback**: If listener has no `bearer_token`, uses global `bearer_token`
2. **Per-Tenant Override**: Listener-specific `bearer_token` takes precedence
3. **HTTP Forwarding**: Each batch uses the appropriate bearer token for its tenant
4. **Spooling Persistence**: Bearer tokens are stored with spooled files for retry operations

See `examples/multi-tenant-config.yaml` for a complete multi-tenant setup example.

### Receiver Configuration  
```yaml
receiver:
  base_url: "http://localhost:8080"
  timeout_seconds: 30
  retry_count: 3
  retry_delay_seconds: 1

# Global tenant configuration (used as fallback)
tenant_id: "customer-1"
bearer_token: "your-bearer-token-here"
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

### Spooling & DLQ Configuration

**Critical for Data Safety**: Configure local spooling to handle network interruptions and receiver outages:

```yaml
spooling:
  enabled: true                          # Enable local disk spooling for failed uploads
  directory: "/var/spool/bytefreezer-proxy"  # Local storage directory
  max_size_bytes: 1073741824            # 1GB total spool directory limit
  retry_attempts: 5                     # Maximum retry attempts before DLQ
  retry_interval_seconds: 60            # Wait time between retries (1 minute)
  cleanup_interval_seconds: 300         # Background cleanup interval (5 minutes)
  
  # Organization settings
  organization: "tenant_dataset"         # Options: flat, tenant_dataset, date_tenant, protocol_tenant
  per_tenant_limits: false              # Apply size limits per tenant vs globally
  max_files_per_dataset: 1000          # Max files per dataset (0 = unlimited)
  max_age_days: 7                      # Max age before cleanup (0 = unlimited)
```

#### Spooling Configuration Options

**Organization Strategies:**
- `flat`: All files in root spool directory (simple, but can get cluttered)
- `tenant_dataset`: Organized by tenant/dataset subdirectories (recommended)
- `date_tenant`: Date-based organization with tenant subdirectories
- `protocol_tenant`: Protocol-based organization (udp/syslog/netflow/sflow)

**Sizing Guidelines:**
- **Small deployments** (<1GB/day): 1GB spool limit, 3 retry attempts
- **Medium deployments** (1-10GB/day): 5GB spool limit, 5 retry attempts  
- **Large deployments** (>10GB/day): 20GB+ spool limit, consider per-tenant limits

**Directory Structure Examples:**

*tenant_dataset organization:*
```
/var/spool/bytefreezer-proxy/
├── customer-1/
│   ├── syslog-data/
│   │   ├── 20240115-103045-batch-abc.ndjson
│   │   └── 20240115-103045-batch-abc.meta
│   └── ebpf-data/
│       ├── 20240115-103047-batch-def.ndjson
│       └── 20240115-103047-batch-def.meta
└── DLQ/
    ├── 20240115-103045-batch-failed.ndjson
    └── 20240115-103045-batch-failed.meta
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

### Prometheus Metrics

ByteFreezer Proxy provides comprehensive Prometheus metrics on port 9099:

- **UDP Metrics**: Bytes/packets/lines received per tenant and dataset
- **HTTP Metrics**: Bytes/lines forwarded, request success rates, response times  
- **Batch Metrics**: Batch size distributions, processing durations
- **Spool Metrics**: Queue sizes, disk usage for failed batches
- **System Metrics**: Service health, resource usage

**Quick Start with Docker Compose:**
```bash
# Start ByteFreezer Proxy with Prometheus monitoring
docker-compose -f docker-compose.prometheus.yml up -d

# Access services:
# - ByteFreezer Proxy: http://localhost:8088/health
# - Prometheus: http://localhost:9090
# - Grafana: http://localhost:3000 (admin/admin123)
# - AlertManager: http://localhost:9093
```

**Key Metrics Available:**
```prometheus
# Throughput metrics
bytefreezer_proxy_udp_bytes_received_total{tenant_id="customer-1", dataset_id="syslog-data"}
bytefreezer_proxy_http_bytes_forwarded_total{tenant_id="customer-1", dataset_id="syslog-data"}

# Performance metrics
bytefreezer_proxy_forward_duration_seconds{tenant_id="customer-1", dataset_id="syslog-data"}
bytefreezer_proxy_batch_size_bytes{tenant_id="customer-1", dataset_id="syslog-data"}

# Reliability metrics
bytefreezer_proxy_http_requests_total{tenant_id="customer-1", dataset_id="syslog-data", status="success"}
bytefreezer_proxy_spool_queue_size{tenant_id="customer-1", dataset_id="syslog-data"}
```

### Alerting Rules

Pre-configured alerts for:
- **Service Down**: ByteFreezer Proxy instance offline
- **No UDP Traffic**: No data received for 5+ minutes
- **High Error Rate**: HTTP forwarding failures >10/sec
- **Slow Forwarding**: 95th percentile >30s processing time
- **Spool Queue Full**: >1000 queued files or >500MB disk usage

### Kubernetes Monitoring

For Kubernetes deployments with Prometheus Operator:
```bash
# Enable ServiceMonitor in kustomization.yaml
kubectl apply -k kubernetes/

# Or apply monitoring separately
kubectl apply -f kubernetes/servicemonitor.yaml
kubectl apply -f kubernetes/podmonitor.yaml
```

### Legacy OpenTelemetry Integration

Also supports OTLP gRPC export by setting `prometheus_mode: false`:
- OpenTelemetry integration for metrics and tracing
- SOC alerting for operational issues
- Structured logging with configurable levels

## Troubleshooting

### Port Configuration Issues

**Problem**: "No UDP listeners configured" message
- **Cause**: All configured ports have empty or missing `dataset_id` values
- **Solution**: Ensure at least one port has a non-empty `dataset_id`

**Problem**: Need more than 10 datasets
- **Limitation**: Hard limit of 10 concurrent datasets per proxy instance
- **Solution**: Deploy multiple proxy instances with different port ranges, or use multiple network interfaces

**Problem**: Cannot use custom port numbers
- **Limitation**: Port range is fixed at 2056-2065 for architectural consistency
- **Solution**: Use port forwarding or load balancer to map external ports to the reserved range

### Performance Optimization

**For high-throughput environments (>10GB/day per dataset)**:
- Increase `read_buffer_size_bytes` to 256MB or 512MB
- **Batching Strategy**: Line count (`max_batch_lines`) takes precedence - set to ~1M for high volume
- **Size Safety**: Byte limit (`max_batch_bytes`) prevents oversized batches - both limits enforced
- Monitor system UDP buffer limits with `ss -u -l -n`

## Error Handling & Data Recovery

### Automatic Retry System

ByteFreezer Proxy implements a robust retry mechanism for failed data forwards:

- **Spooling**: Failed batches are automatically saved to local disk storage
- **Exponential backoff**: Retries with increasing intervals to prevent overwhelming the receiver
- **Configurable limits**: Maximum retry attempts and intervals can be tuned per deployment
- **SOC alerting**: Persistent failures trigger alerts for operational visibility

### Dead Letter Queue (DLQ) 

**Critical for Data Recovery**: When batches exceed the maximum retry attempts, they are moved to a Dead Letter Queue for manual recovery.

#### DLQ Directory Structure
```
/var/spool/bytefreezer-proxy/
├── [active retry files...]
└── DLQ/
    ├── 20240115-103045-batch-abc123.ndjson      # Failed data
    ├── 20240115-103045-batch-abc123.meta        # Failure metadata
    ├── 20240115-103047-batch-def456.ndjson      # Failed data  
    └── 20240115-103047-batch-def456.meta        # Failure metadata
```

#### DLQ Behavior
- **Automatic Movement**: Files are moved to DLQ when retry limit is exceeded
- **No Further Processing**: DLQ files are excluded from retry attempts (performance optimization)
- **Preserved Indefinitely**: Files remain in DLQ until manually processed or removed
- **Complete Context**: Both data and metadata files are preserved with failure reasons

#### DLQ Metadata Format
Each `.meta` file contains detailed failure information:
```json
{
  "id": "20240115-103045-batch-abc123",
  "tenant_id": "customer-1", 
  "dataset_id": "syslog-data",
  "filename": "20240115-103045-batch-abc123.ndjson",
  "size": 1048576,
  "line_count": 1000,
  "created_at": "2024-01-15T10:30:45Z",
  "last_retry": "2024-01-15T10:35:45Z", 
  "retry_count": 5,
  "status": "dlq",
  "failure_reason": "Moved to DLQ after exceeding maximum retry attempts"
}
```

#### Manual DLQ Recovery

**1. Inspect Failed Batches:**
```bash
# List DLQ files
ls -la /var/spool/bytefreezer-proxy/DLQ/

# View failure details
cat /var/spool/bytefreezer-proxy/DLQ/batch-abc123.meta | jq '.'

# Check data content
head -5 /var/spool/bytefreezer-proxy/DLQ/batch-abc123.ndjson
```

**2. Manual Reprocessing Options:**

**Option A: Direct HTTP POST to Receiver**
```bash
# Extract metadata for context
TENANT=$(jq -r '.tenant_id' /var/spool/bytefreezer-proxy/DLQ/batch-abc123.meta)
DATASET=$(jq -r '.dataset_id' /var/spool/bytefreezer-proxy/DLQ/batch-abc123.meta)

# Manual forward to receiver
curl -X POST \
  -H "Content-Type: application/x-ndjson" \
  -H "Content-Encoding: gzip" \
  --data-binary @/var/spool/bytefreezer-proxy/DLQ/batch-abc123.ndjson \
  "http://your-receiver:8080/data/${TENANT}/${DATASET}"
```

**Option B: Move Back to Active Spool (for retry)**
```bash
# Move files back to main spool directory
mv /var/spool/bytefreezer-proxy/DLQ/batch-abc123.* /var/spool/bytefreezer-proxy/

# Update metadata status for retry (optional)
jq '.status = "pending" | .retry_count = 0' \
  /var/spool/bytefreezer-proxy/batch-abc123.meta > temp.meta && \
  mv temp.meta /var/spool/bytefreezer-proxy/batch-abc123.meta
```

#### DLQ Monitoring

**Check DLQ Status:**
```bash
# Count DLQ files
echo "DLQ Files: $(find /var/spool/bytefreezer-proxy/DLQ -name "*.ndjson" | wc -l)"

# Calculate DLQ size
du -sh /var/spool/bytefreezer-proxy/DLQ/

# Recent DLQ entries
find /var/spool/bytefreezer-proxy/DLQ -name "*.meta" -mtime -1 | \
  xargs -I {} sh -c 'echo "=== {} ==="; jq ".tenant_id,.dataset_id,.failure_reason" {}'
```

**Prometheus Metrics for DLQ:**
```prometheus
# DLQ file count (from filesystem monitoring)
node_filesystem_files{mountpoint="/var/spool/bytefreezer-proxy"}

# DLQ disk usage  
node_filesystem_avail_bytes{mountpoint="/var/spool/bytefreezer-proxy"}
```

#### DLQ Best Practices

**1. Regular Monitoring**
- Set up alerts for DLQ directory size growth
- Monitor DLQ file creation rate as a service health indicator
- Include DLQ status in operational dashboards

**2. Operational Procedures**  
- Establish SLAs for DLQ file investigation (e.g., <24 hours)
- Document data recovery procedures for your team
- Test recovery procedures regularly during maintenance windows

**3. Preventive Measures**
- Monitor receiver health and connectivity
- Set appropriate retry limits based on your recovery SLA
- Consider receiver scaling if DLQ files accumulate frequently

**4. Data Retention**
- Implement DLQ file rotation based on your compliance requirements  
- Archive old DLQ files to cold storage if needed for audit purposes
- Consider automated cleanup of successfully reprocessed DLQ files

### Additional Error Handling Features

- **Graceful handling** of oversized payloads
- **Connection pooling** and timeout management  
- **Circuit breaker pattern** for receiver failures
- **Structured logging** with failure context and correlation IDs
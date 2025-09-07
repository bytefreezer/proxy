# ByteFreezer Proxy

High-performance UDP log proxy for the ByteFreezer platform. This proxy is required to collect data internal to your network efficiently via UDP, and forward it effectively via TCP to the ByteFreezer receiver.

## Overview

ByteFreezer Proxy is designed to be installed on-premises for heavy UDP users. It:
- Listens for UDP data from external sources (syslog, eBPF, etc.)
- **Supports up to 10 concurrent datasets** on dedicated UDP ports (2056-2065)
- **Smart batching**: Line count takes precedence, with byte limits as additional constraints
- Compresses and forwards batches to bytefreezer-receiver via HTTP
- Provides health and configuration APIs

### Port Allocation Architecture

ByteFreezer Proxy uses a **pre-allocated port range** system for optimal performance and resource management:

- **Port Range**: 2056-2065 (10 ports reserved for UDP datasets)
- **Smart Activation**: Only ports with configured `dataset_id` values are bound and consume resources
- **Zero-Resource Inactive Ports**: Unconfigured ports remain completely inactive
- **Per-Dataset Isolation**: Each dataset gets its own dedicated UDP port and processing pipeline
- **Scalable Design**: Easily expand from 3 to 10 datasets by uncommenting configuration lines

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

## Error Handling

- Automatic retry with exponential backoff for failed forwards
- SOC alerting for persistent failures
- Graceful handling of oversized payloads
- Connection pooling and timeout management
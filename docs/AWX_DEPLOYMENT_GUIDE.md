# AWX Deployment Guide

Complete setup guide for deploying ByteFreezer Proxy using AWX with support for multiple data format hints.

## Setup Steps

### 1. Create Project
```yaml
Name: ByteFreezer Proxy
SCM Type: Git
SCM URL: https://github.com/n0needt0/bytefreezer-proxy.git
SCM Subdirectory: ansible/playbooks
Branch: main
```

### 2. Create Credential
```yaml
Name: ByteFreezer SSH
Type: Machine
Username: your-user
SSH Private Key: [paste key]
Privilege Escalation: sudo
```

### 3. Create Inventory
```yaml
Name: ByteFreezer Servers
Hosts:
  - proxy-01.example.com
  - proxy-02.example.com
```

### 4. Create Job Templates

**Install Service:**
```yaml
Name: ByteFreezer Proxy - Install
Playbook: install.yml
Inventory: ByteFreezer Servers
Credential: ByteFreezer SSH
Options: ☑ Become Privilege Escalation
```

**Docker Install:**
```yaml
Name: ByteFreezer Proxy - Docker Install
Playbook: docker_install.yml
Inventory: ByteFreezer Servers
Credential: ByteFreezer SSH
Options: ☑ Become Privilege Escalation
```

**Remove Service:**
```yaml
Name: ByteFreezer Proxy - Remove
Playbook: remove.yml
Inventory: ByteFreezer Servers
Credential: ByteFreezer SSH
Options: ☑ Become Privilege Escalation
```

## Configuration

Variables are in `group_vars/all.yml`. Override in AWX job templates as needed:

```yaml
# Example override variables for AWX
bytefreezer_proxy_version: "v1.2.3"
tenant_id: "customer-prod"
bearer_token: "prod-token-123"

# Receiver configuration
config:
  receiver:
    base_url: "http://receiver.company.com:8082/{tenantid}/{datasetid}"
    timeout_seconds: 30
    upload_worker_count: 5

  # Plugin configuration with data_hint support
  inputs:
    # Production eBPF data (NDJSON format)
    - type: "udp"
      name: "ebpf-collector"
      config:
        host: "0.0.0.0"
        port: 2056
        dataset_id: "ebpf-data"
        data_hint: "ndjson"
        worker_count: 4

    # sFlow network monitoring (parsed to NDJSON)
    - type: "sflow"
      name: "sflow-collector"
      config:
        host: "0.0.0.0"
        port: 6343
        dataset_id: "network-sflow"
        protocol: "sflow"
        data_hint: "ndjson"
        worker_count: 4

    # System logs (syslog format)
    - type: "udp"
      name: "syslog-collector"
      config:
        host: "0.0.0.0"
        port: 514
        dataset_id: "system-logs"
        data_hint: "syslog"
        worker_count: 4

    # Application webhooks (HTTP/NDJSON)
    - type: "http"
      name: "webhook-endpoint"
      config:
        host: "0.0.0.0"
        port: 8081
        path: "/webhook"
        dataset_id: "webhook-events"
        data_hint: "ndjson"
        max_request_size: 1048576
```

### Data Format Hints

The proxy supports automatic format detection for downstream processing via `data_hint`:

| Hint | Format | Description |
|------|--------|-------------|
| `ndjson` | Newline-Delimited JSON | Structured application logs, API events |
| `sflow` | sFlow v5 | Network monitoring (binary) |
| `netflow` | NetFlow v5 | Network flow data (binary) |
| `ipfix` | IPFIX | IP Flow Information Export (binary) |
| `syslog` | RFC3164/RFC5424 | System logs |
| `csv` | Comma-Separated Values | Tabular data |
| `tsv` | Tab-Separated Values | Tabular data |
| `apache` | Apache Common Log | Web server logs |
| `nginx` | Nginx access logs | Web server logs |
| `raw` | Unstructured | Legacy/custom formats |

## Usage

1. Run "ByteFreezer Proxy - Install" to deploy native service
2. Run "ByteFreezer Proxy - Docker Install" to deploy containerized service
3. Run "ByteFreezer Proxy - Remove" to uninstall service

### Service Endpoints

After deployment, service will be available on:
- **Health Check**: `http://server:8088/health`
- **API Stats**: `http://server:8088/api/v2/stats`
- **Plugin Health**: `http://server:8088/api/v2/plugins/health`
- **DLQ Status**: `http://server:8088/api/v2/dlq/stats`
- **Metrics** (if enabled): `http://server:9090/metrics`

### Default Ports

| Service | Port | Protocol | Data Hint |
|---------|------|----------|-----------|
| eBPF Data | 2056 | UDP | `ndjson` |
| sFlow | 6343 | UDP | `sflow` |
| Syslog | 514 | UDP | `syslog` |
| Webhooks | 8081 | HTTP | `ndjson` |

## Testing

Use the included fake feed scripts for testing:

```bash
# Test different data formats
./fake_feeds/udp_sflow.sh server-ip 6343 10 1
./fake_feeds/udp_syslog.sh server-ip 514 10 1
./fake_feeds/udp_ndjson.sh server-ip 2056 10 1
./fake_feeds/http_ndjson.sh server-ip 8081 /webhook 5 2
```
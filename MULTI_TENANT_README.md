# ByteFreezer Proxy - Multi-Tenant Architecture

ByteFreezer Proxy now supports **comprehensive multi-tenant deployments** through port-based tenant isolation combined with hierarchical data organization.

## 🏗️ Architecture Overview

```
                    ┌─────────────────────────┐
                    │  ByteFreezer Proxy      │
                    │  Multi-Tenant Engine    │
                    └─────────────────────────┘
                              │
        ┌─────────────────────┼─────────────────────┐
        │                     │                     │
   Port Group 1          Port Group 2         Port Group 3
   Tenant A              Tenant B             Tenant C
   ┌─────────────┐       ┌─────────────┐      ┌─────────────┐
   │ 2055: NetFlow│       │ 2058: sFlow │      │ 2061: Syslog│
   │ 2056: Syslog │  ──►  │ 2059: Syslog│  ──► │ 2062: NetFlow│
   │ 2057: sFlow  │       │ 2060: UDP   │      │ 2063: UDP   │
   └─────────────┘       └─────────────┘      └─────────────┘
           │                     │                     │
           ▼                     ▼                     ▼
   /spool/tenant-a/      /spool/tenant-b/      /spool/tenant-c/
   ├── netflow-data/     ├── network-mon/      ├── pos-logs/
   ├── app-logs/         ├── audit-logs/       ├── traffic/
   └── performance/      └── telemetry/        └── metrics/
```

## ✨ Key Features

### 🔒 **Network Isolation**
- **Dedicated ports per tenant** - Each tenant gets exclusive UDP port(s)
- **Protocol flexibility** - Different protocols (syslog, NetFlow, sFlow, UDP) per tenant
- **Independent configuration** - Per-tenant protocol settings and modes

### 📁 **Hierarchical Data Organization**
- **Tenant/Dataset structure** - `/tenant-id/dataset-id/` organization
- **Multiple organization strategies** - tenant_dataset, date_tenant, protocol_tenant
- **Backward compatibility** - Supports legacy flat structure

### 🛡️ **Resource Isolation**
- **Per-tenant storage limits** - Independent disk quotas
- **Separate cleanup policies** - Tenant-specific retention and cleanup
- **Independent retry logic** - Per-tenant failure handling

### 📊 **Comprehensive Monitoring**
- **Tenant-specific statistics** - Detailed per-tenant metrics
- **Multi-tenant dashboards** - Overview across all tenants
- **Tenant health monitoring** - Status tracking per tenant

## 🚀 Quick Start

### 1. **Basic Multi-Tenant Configuration**

```yaml
udp:
  enabled: true
  listeners:
    # Tenant A
    - port: 2055
      tenant_id: "company-a"
      dataset_id: "logs"
      protocol: "syslog"
    
    # Tenant B  
    - port: 2056
      tenant_id: "company-b" 
      dataset_id: "flows"
      protocol: "netflow"

spooling:
  organization: "tenant_dataset"  # Hierarchical structure
  per_tenant_limits: true        # Separate limits per tenant
```

### 2. **Advanced Multi-Dataset Setup**

```yaml
udp:
  listeners:
    # Enterprise Corp - Multiple datasets
    - port: 2055
      tenant_id: "enterprise-corp"
      dataset_id: "web-logs"
      protocol: "syslog"
      syslog_mode: "rfc5424"
    
    - port: 2056
      tenant_id: "enterprise-corp"
      dataset_id: "network-flows"
      protocol: "netflow"
    
    - port: 2057
      tenant_id: "enterprise-corp"
      dataset_id: "performance-data"
      protocol: "sflow"
```

### 3. **Start and Monitor**

```bash
# Start with multi-tenant config
./bytefreezer-proxy --config multi-tenant-config.yaml

# Monitor tenant status
curl http://localhost:8080/api/tenants

# Get tenant-specific statistics
curl http://localhost:8080/api/tenants/company-a/stats
```

## 📋 Configuration Examples

### Small Business (2-3 Tenants)
- **Ports**: 2055-2064 (3-4 ports per tenant)
- **Use case**: Managed service provider
- **Example**: [examples/multi-tenant-config.yaml](examples/multi-tenant-config.yaml)

### Enterprise (5+ Tenants)
- **Ports**: 2055-2199 (flexible allocation)
- **Use case**: Large MSP or internal enterprise
- **Features**: Per-tenant limits, hierarchical organization

### Compliance-Heavy (Healthcare/Finance)
- **Isolation**: Maximum isolation with dedicated ports
- **Features**: Per-tenant encryption, audit trails
- **Monitoring**: Enhanced SOC alerting per tenant

## 🔧 Supported Protocols Per Tenant

| Protocol | Formats | Use Cases |
|----------|---------|-----------|
| **syslog** | RFC3164, RFC5424 | Application logs, system events, security logs |
| **netflow** | v5, v9, IPFIX | Network monitoring, traffic analysis, DDoS detection |
| **sflow** | v5 | Network performance, sampling, bandwidth monitoring |
| **udp** | Raw UDP | Custom telemetry, IoT data, proprietary protocols |

## 📊 Data Organization Strategies

### 1. **tenant_dataset** (Recommended)
```
/spool/financial-corp/network-flows/20250910-143022-batch123.ndjson
/spool/healthcare-org/audit-logs/20250910-143023-batch124.ndjson
```

### 2. **date_tenant** (Time-based)
```
/spool/2025/09/10/financial-corp/network-flows/143022-batch123.ndjson
/spool/2025/09/10/healthcare-org/audit-logs/143023-batch124.ndjson
```

### 3. **protocol_tenant** (Protocol-based)
```
/spool/netflow/financial-corp/flows/20250910-143022-batch123.ndjson
/spool/syslog/healthcare-org/logs/20250910-143023-batch124.ndjson
```

## 🎯 Migration Path

### From Single-Tenant to Multi-Tenant

1. **Update Configuration**
   ```yaml
   # Add tenant_id to existing listeners
   udp:
     listeners:
       - port: 2055
         tenant_id: "existing-tenant"  # Add this
         dataset_id: "logs"
   ```

2. **Enable Hierarchical Spooling**
   ```yaml
   spooling:
     organization: "tenant_dataset"  # Enable hierarchy
     per_tenant_limits: true        # Enable per-tenant limits
   ```

3. **Add New Tenants**
   ```yaml
   # Add new tenant with different ports
   - port: 2056
     tenant_id: "new-tenant"
     dataset_id: "logs"
   ```

## 🔍 Monitoring & Management

### API Endpoints

```bash
# Tenant overview
GET /api/tenants
{
  "tenant_count": 3,
  "total_ports": 8,
  "tenants": {...}
}

# Tenant details
GET /api/tenants/{tenant-id}
{
  "tenant_id": "financial-corp",
  "ports": [2055, 2056, 2057],
  "datasets": ["flows", "logs", "metrics"],
  "status": "active"
}

# Spooling statistics
GET /api/spooling/tenants
{
  "financial-corp": {
    "size_bytes": 1048576,
    "file_count": 42,
    "datasets": {...}
  }
}
```

### Metrics & Alerting

- **Per-tenant metrics** via OpenTelemetry
- **SOC alerts** with tenant context
- **Resource usage** monitoring per tenant
- **Health checks** for tenant-specific services

## 📚 Documentation

- **[Complete Setup Guide](docs/MULTI_TENANT_SETUP.md)** - Detailed configuration and deployment
- **[Configuration Example](examples/multi-tenant-config.yaml)** - Ready-to-use multi-tenant config
- **[API Documentation](API.md)** - Multi-tenant API endpoints and usage

## 🛠️ Implementation Details

### Validation & Safety
- **Port conflict detection** - Prevents duplicate port assignments
- **Protocol validation** - Ensures supported protocols per tenant
- **Tenant ID validation** - Enforces required tenant identification
- **Configuration consistency** - Validates tenant/dataset/protocol combinations

### Performance & Scaling
- **Concurrent processing** - Independent goroutines per port/tenant
- **Resource isolation** - Per-tenant memory and disk limits
- **Efficient routing** - Direct tenant routing without cross-tenant overhead
- **Horizontal scaling** - Support for N tenants with proper port allocation

---

**Ready to deploy multi-tenant ByteFreezer Proxy?** Start with the [examples/multi-tenant-config.yaml](examples/multi-tenant-config.yaml) and follow the [setup guide](docs/MULTI_TENANT_SETUP.md)!
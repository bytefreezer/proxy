# ByteFreezer Proxy - Multi-Tenant Setup Guide

ByteFreezer Proxy supports **multi-tenant deployments** through port-based tenant isolation combined with hierarchical data organization. This approach provides strong network isolation while maintaining efficient resource utilization.

## Architecture Overview

```
                    ┌─────────────────┐
                    │ ByteFreezer     │
                    │ Proxy           │
                    │ (Multi-Tenant)  │
                    └─────────────────┘
                             │
        ┌────────────────────┼────────────────────┐
        │                    │                    │
   Port 2055-2057       Port 2058-2060      Port 2061-2064
   ┌─────────────┐      ┌─────────────┐      ┌─────────────┐
   │ Tenant A    │      │ Tenant B    │      │ Tenant C    │
   │ Financial   │      │ Healthcare  │      │ Retail      │
   │ Corp        │      │ Org         │      │ Chain       │
   └─────────────┘      └─────────────┘      └─────────────┘
```

## Key Features

### 🔒 **Network Isolation**
- Each tenant gets dedicated UDP port(s)
- No cross-tenant network traffic mixing
- Independent protocol support per tenant

### 📁 **Hierarchical Data Organization**
```
/data/spool/
├── financial-corp/
│   ├── network-flows/     (NetFlow data)
│   ├── app-logs/          (Application syslog)
│   └── security-events/   (Security syslog)
├── healthcare-org/
│   ├── network-monitoring/ (sFlow data)
│   ├── audit-logs/        (HIPAA audit syslog)
│   └── device-telemetry/  (Device UDP data)
└── retail-chain/
    ├── pos-logs/          (POS syslog)
    ├── traffic-analysis/  (NetFlow data)
    ├── store-metrics/     (Store UDP metrics)
    └── inventory-events/  (Inventory syslog)
```

### 🛡️ **Resource Isolation**
- Per-tenant storage limits
- Independent retry policies
- Separate cleanup schedules
- Tenant-specific alerting

## Configuration Guide

### Basic Multi-Tenant Setup

```yaml
udp:
  enabled: true
  host: "0.0.0.0"
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
  organization: "tenant_dataset"  # /tenant/dataset/ hierarchy
  per_tenant_limits: true        # Separate limits per tenant
```

### Advanced Multi-Tenant Setup

```yaml
udp:
  listeners:
    # Multiple datasets per tenant
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
      dataset_id: "performance-metrics"
      protocol: "sflow"

spooling:
  organization: "tenant_dataset"
  per_tenant_limits: true
  max_size_bytes: 1073741824      # 1GB per tenant
  max_files_per_dataset: 500      # 500 files per dataset
  max_age_days: 3                 # 3-day retention
```

## Supported Organization Strategies

### 1. **tenant_dataset** (Recommended)
```
/spool/tenant-a/dataset-1/20250910-143022-batch123.ndjson
/spool/tenant-a/dataset-2/20250910-143023-batch124.ndjson
/spool/tenant-b/dataset-1/20250910-143024-batch125.ndjson
```

### 2. **date_tenant**
```
/spool/2025/09/10/tenant-a/dataset-1/143022-batch123.ndjson
/spool/2025/09/10/tenant-b/dataset-1/143023-batch124.ndjson
```

### 3. **protocol_tenant**
```
/spool/netflow/tenant-a/flows/20250910-143022-batch123.ndjson
/spool/syslog/tenant-a/logs/20250910-143023-batch124.ndjson
/spool/sflow/tenant-b/metrics/20250910-143024-batch125.ndjson
```

## Protocol Support Per Tenant

| Protocol | Description | Use Cases |
|----------|-------------|-----------|
| `syslog` | RFC3164/RFC5424 syslog messages | Application logs, system logs, security events |
| `netflow` | NetFlow v5/v9 network flow data | Network monitoring, traffic analysis |
| `sflow` | sFlow v5 network sampling | Network performance, DDoS detection |
| `udp` | Raw UDP data | Custom telemetry, IoT data, metrics |

## Port Allocation Guidelines

### Recommended Port Ranges

- **Single Tenant**: 2055-2059 (5 ports max)
- **Small Multi-Tenant**: 2055-2069 (3-5 tenants, 3-5 ports each)  
- **Large Multi-Tenant**: 2055-2199 (up to 20+ tenants)

### Port Planning Example

```yaml
# Tenant A: Financial Services (Ports 2055-2059)
- port: 2055  # NetFlow from core routers
- port: 2056  # syslog from application servers  
- port: 2057  # syslog from security appliances
- port: 2058  # sFlow from edge switches
- port: 2059  # Custom telemetry data

# Tenant B: Healthcare (Ports 2060-2064)  
- port: 2060  # Medical device syslog
- port: 2061  # Network flow monitoring
- port: 2062  # HIPAA audit logs
- port: 2063  # Performance metrics
- port: 2064  # IoT sensor data
```

## Monitoring and Management

### Tenant Statistics

Access tenant-specific statistics via the API:

```bash
# Get overall tenant information
curl http://localhost:8080/api/tenants

# Get statistics for specific tenant
curl http://localhost:8080/api/tenants/company-a/stats

# Get spooling statistics per tenant
curl http://localhost:8080/api/spooling/tenants
```

### Example API Response

```json
{
  "tenants": {
    "financial-corp": {
      "port_count": 3,
      "dataset_count": 3,
      "protocols": {
        "netflow": 1,
        "syslog": 2
      },
      "datasets": ["network-flows", "app-logs", "security-events"],
      "ports": [2055, 2056, 2057],
      "status": "active"
    },
    "healthcare-org": {
      "port_count": 3,
      "dataset_count": 3,
      "protocols": {
        "sflow": 1,
        "syslog": 1,
        "udp": 1
      },
      "datasets": ["network-monitoring", "audit-logs", "device-telemetry"],
      "ports": [2058, 2059, 2060],
      "status": "active"
    }
  }
}
```

## Security Considerations

### Network Isolation
- **Firewall Rules**: Configure firewall rules to restrict tenant port access
- **VLANs**: Use VLANs to provide additional network segregation
- **Source IP Validation**: Implement source IP restrictions per tenant

### Data Isolation
- **Directory Permissions**: Set appropriate file system permissions on spool directories
- **Encryption**: Enable encryption for sensitive tenant data
- **Access Controls**: Implement tenant-specific access controls

### Monitoring
- **Tenant-specific Alerts**: Configure SOC alerts per tenant
- **Resource Monitoring**: Monitor per-tenant resource usage
- **Audit Logging**: Enable comprehensive audit logging for multi-tenant operations

## Deployment Patterns

### 1. **Shared Infrastructure**
- Single proxy instance handling multiple tenants
- Cost-effective for smaller tenants
- Shared resources with isolation boundaries

### 2. **Dedicated Infrastructure**  
- Separate proxy instances per major tenant
- Higher isolation but more resource usage
- Recommended for compliance-sensitive tenants

### 3. **Hybrid Model**
- Mix of shared and dedicated based on tenant requirements
- Flexible scaling and isolation options
- Tiered service offerings

## Troubleshooting

### Common Issues

1. **Port Conflicts**
   ```
   Error: listener 2 (port 2055): port already in use
   ```
   Solution: Ensure unique ports per listener

2. **Missing Tenant ID**
   ```
   Error: tenant_id must be specified either per-listener or globally
   ```
   Solution: Set `tenant_id` globally or per listener

3. **Protocol Conflicts**
   ```
   Error: dataset 'logs' for tenant 'company-a' uses conflicting protocols
   ```
   Solution: Use different dataset names for different protocols

### Debug Commands

```bash
# Check configuration validation
./bytefreezer-proxy --config config.yaml --validate-only

# View tenant configuration
curl http://localhost:8080/api/config/tenants

# Monitor real-time tenant stats
watch -n 5 'curl -s http://localhost:8080/api/tenants | jq'
```

## Migration from Single-Tenant

### Step 1: Update Configuration
```yaml
# Before (single tenant)
tenant_id: "company-a"
udp:
  listeners:
    - port: 2055
      dataset_id: "logs"

# After (multi-tenant)
udp:
  listeners:
    - port: 2055
      tenant_id: "company-a"  # Explicit tenant ID
      dataset_id: "logs"
    - port: 2056
      tenant_id: "company-b"  # New tenant
      dataset_id: "logs"
```

### Step 2: Enable Hierarchical Spooling
```yaml
spooling:
  organization: "tenant_dataset"  # Enable hierarchy
  per_tenant_limits: true        # Enable per-tenant limits
```

### Step 3: Migrate Existing Data
```bash
# Move existing flat structure to hierarchical
mkdir -p /data/spool/company-a/logs/
mv /data/spool/*.ndjson /data/spool/company-a/logs/
```

## Best Practices

1. **Start Small**: Begin with 2-3 tenants and expand gradually
2. **Plan Port Allocation**: Reserve port ranges for future tenant growth
3. **Monitor Resource Usage**: Track per-tenant resource consumption
4. **Implement Gradual Rollout**: Test with non-critical tenants first
5. **Document Tenant Mappings**: Maintain clear tenant-to-port documentation
6. **Regular Cleanup**: Implement automated cleanup policies for tenant data
7. **Backup Tenant Configurations**: Backup multi-tenant configurations regularly

---

For more information, see the [examples/plugin-config.yaml](../examples/plugin-config.yaml) file for a complete configuration example.
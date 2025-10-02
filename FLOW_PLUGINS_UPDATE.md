# Flow Plugins Update - sFlow, NetFlow, IPFIX

## Overview

The proxy now includes dedicated plugins for parsing network flow data (sFlow, NetFlow, IPFIX) to NDJSON format using the goflow2 library. This replaces the previous binary pass-through approach.

## What Changed

### New Plugins Added
- **sflow plugin** - Parses sFlow v5 packets to NDJSON
- **netflow plugin** - Parses NetFlow v5/v9 packets to NDJSON
- **ipfix plugin** - Parses IPFIX packets to NDJSON

All plugins use the industry-standard [goflow2](https://github.com/netsampler/goflow2) library for reliable flow parsing.

### Migration Required

**OLD Configuration (UDP plugin with binary data):**
```yaml
inputs:
  - type: "udp"
    name: "sflow-udp-listener"
    config:
      host: "0.0.0.0"
      port: 2060
      dataset_id: "sflow-data"
      protocol: "udp"
      data_hint: "sflow"        # Binary sFlow data
      read_buffer_size: 65536
      worker_count: 4
```

**NEW Configuration (sflow plugin with JSON parsing):**
```yaml
inputs:
  - type: "sflow"
    name: "sflow-udp-listener"
    config:
      host: "0.0.0.0"
      port: 2060
      dataset_id: "sflow-data"
      protocol: "sflow"
      data_hint: "ndjson"       # Parsed to NDJSON
      read_buffer_size: 65536
      worker_count: 4
```

## Configuration Examples

### sFlow Collection
```yaml
- type: "sflow"
  name: "sflow-collector"
  config:
    host: "0.0.0.0"
    port: 6343              # Standard sFlow port
    dataset_id: "network-sflow"
    protocol: "sflow"
    data_hint: "ndjson"
    read_buffer_size: 65536
    worker_count: 4
```

### NetFlow v5/v9 Collection
```yaml
- type: "netflow"
  name: "netflow-collector"
  config:
    host: "0.0.0.0"
    port: 2055              # Standard NetFlow port
    dataset_id: "network-netflow"
    protocol: "netflow"
    data_hint: "ndjson"
    read_buffer_size: 65536
    worker_count: 4
```

### IPFIX Collection
```yaml
- type: "ipfix"
  name: "ipfix-collector"
  config:
    host: "0.0.0.0"
    port: 4739              # Standard IPFIX port
    dataset_id: "network-ipfix"
    protocol: "ipfix"
    data_hint: "ndjson"
    read_buffer_size: 65536
    worker_count: 4
```

## Benefits

1. **Structured Data**: Flow data is automatically converted to JSON for easier querying
2. **Standards Compliant**: Uses goflow2 library for reliable flow parsing
3. **Better Analytics**: NDJSON format enables efficient downstream processing
4. **Consistent Format**: All flow types output the same JSON structure
5. **No Binary Data**: Eliminates need for specialized binary parsers downstream

## Architecture

### Data Flow
```
Network Device (sFlow/NetFlow/IPFIX)
    ↓ UDP packets
Flow Plugin (goflow2 decoder)
    ↓ Parse binary protocol
JSON Conversion
    ↓ NDJSON format
Spooling Service
    ↓ .ndjson files
Receiver
    ↓
S3 Storage
```

### Plugin Features
- Direct filesystem spooling (zero data loss)
- Concurrent upload worker pool
- Retry/DLQ handling
- Health monitoring
- Metrics collection

## Testing

### Validate Configuration
```bash
./bytefreezer-proxy --validate-config --config config.yaml
```

### Test with Sample Data
```bash
# Use the test configuration
./bytefreezer-proxy --config config-test-flows.yaml
```

### Check Plugin Registration
```bash
./bytefreezer-proxy --version
# Should show: "Registered input plugin: sflow/netflow/ipfix"
```

## Ansible/AWX Deployment

The Ansible playbook has been updated with the new configuration:

**File**: `ansible/playbooks/group_vars/all.yml`

The sflow configuration is already active on port 2060. NetFlow and IPFIX examples are provided as comments.

### To Deploy
1. Update `group_vars/all.yml` with your flow collectors
2. Run the playbook:
   ```bash
   ansible-playbook -i inventory install.yml
   ```

## Documentation Updates

The following documentation has been updated:
- ✅ `README.md` - Updated plugin table and data formats
- ✅ `docs/AWX_DEPLOYMENT_GUIDE.md` - Updated sFlow example
- ✅ `docs/DATA_HINTS.md` - Updated flow format documentation
- ✅ `ansible/playbooks/group_vars/all.yml` - Updated default configuration

## Backward Compatibility

**Breaking Change**: The old UDP plugin approach with `data_hint: "sflow"` creating binary `.sflow` files is no longer supported.

**Migration Path**:
1. Update configuration to use the new flow plugins
2. Update downstream consumers to expect NDJSON format instead of binary
3. Test the new configuration before deploying to production

## Support

For issues or questions:
- Check plugin health: `GET /api/v2/plugins/health`
- Review logs for parsing errors
- Verify flow data format matches expected protocol version

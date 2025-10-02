# Data Hints Documentation

## Overview

Data hints provide ByteFreezer components with information about the format and structure of incoming data. This enables downstream components like `bytefreezer-packer` to automatically parse and convert data into optimized formats for storage and analysis.

## Purpose

- **Automatic Parsing**: Enable downstream components to understand data structure
- **Format Detection**: Inform processing pipelines about expected data formats
- **Optimization**: Allow format-specific processing optimizations
- **Parquet Conversion**: Support automatic conversion from structured formats to Parquet

## Default Behavior

When no `data_hint` is specified, the system defaults to `"raw"`, meaning no specific format processing will be applied downstream.

## Supported Data Hints

### Text-Based Formats

#### **ndjson** - Newline-Delimited JSON
- **Description**: Each line contains a valid JSON object
- **Use Case**: Structured logs, API responses, event streams
- **Example**: `{"timestamp": "2025-01-15T10:30:45Z", "level": "info", "message": "User logged in"}`
- **Processing**: Automatic schema detection and Parquet conversion

#### **csv** - Comma-Separated Values
- **Description**: Tabular data with comma-separated fields
- **Use Case**: Data exports, spreadsheet data, analytics feeds
- **Example**: `timestamp,user_id,action,status\n2025-01-15T10:30:45Z,123,login,success`
- **Processing**: Header detection, type inference, Parquet conversion

#### **tsv** - Tab-Separated Values
- **Description**: Tabular data with tab-separated fields
- **Use Case**: Database exports, log parsing, data interchange
- **Example**: `timestamp\tuser_id\taction\tstatus\n2025-01-15T10:30:45Z\t123\tlogin\tsuccess`
- **Processing**: Header detection, type inference, Parquet conversion

### Log Formats

#### **apache** - Apache Access/Error Logs
- **Description**: Standard Apache web server log formats (Common Log Format, Combined Log Format)
- **Use Case**: Web server access logs, error logs, traffic analysis
- **Example**: `127.0.0.1 - - [15/Jan/2025:10:30:45 +0000] "GET /api/v1/status HTTP/1.1" 200 1234`
- **Processing**: Log parsing, field extraction, timestamp normalization

#### **nginx** - Nginx Access/Error Logs
- **Description**: Nginx web server log formats
- **Use Case**: Web server logs, reverse proxy logs, load balancer logs
- **Example**: `127.0.0.1 - - [15/Jan/2025:10:30:45 +0000] "GET /health HTTP/1.1" 200 15`
- **Processing**: Log parsing, custom format support, field extraction

#### **iis** - Windows IIS Web Server Logs
- **Description**: Microsoft IIS web server log format
- **Use Case**: Windows-based web applications, SharePoint, Exchange logs
- **Example**: `2025-01-15 10:30:45 GET /api/health - 80 - 127.0.0.1 200 0 0 15`
- **Processing**: W3C log format parsing, field mapping

#### **squid** - Squid Proxy Cache Logs
- **Description**: Squid proxy server access logs
- **Use Case**: Proxy server logs, cache analysis, network monitoring
- **Example**: `1642248645.123    150 127.0.0.1 TCP_MISS/200 1234 GET http://example.com/`
- **Processing**: Squid format parsing, cache statistics extraction

#### **syslog** - Syslog RFC5424
- **Description**: Structured system log messages following RFC5424 standard
- **Use Case**: System logs, application logs, security events
- **Example**: `<165>1 2025-01-15T10:30:45.123Z myhost myapp 1234 ID47 - User login successful`
- **Processing**: RFC5424 parsing, facility/severity extraction, structured data support

#### **log** - CLF/NCSA Combined Log Format
- **Description**: Common Log Format and NCSA Combined Log Format
- **Use Case**: Generic web server logs, proxy logs
- **Example**: `127.0.0.1 - user [15/Jan/2025:10:30:45 +0000] "GET /index.html HTTP/1.1" 200 2326`
- **Processing**: Standard log format parsing, field normalization

### Metrics and Monitoring Formats

#### **influx** - InfluxDB Line Protocol
- **Description**: Time-series data in InfluxDB Line Protocol format
- **Use Case**: Metrics collection, IoT data, monitoring systems
- **Example**: `cpu,host=server01,region=us-west value=0.64 1642248645000000000`
- **Processing**: Measurement parsing, tag/field separation, timestamp handling

#### **prom** - Prometheus Text Format
- **Description**: Prometheus metrics exposition format
- **Use Case**: Application metrics, infrastructure monitoring
- **Example**: `http_requests_total{method="GET",handler="/api"} 1027 1642248645123`
- **Processing**: Metric parsing, label extraction, histogram/summary handling

#### **statsd** - StatsD Protocol
- **Description**: StatsD metrics protocol format
- **Use Case**: Application metrics, performance monitoring
- **Example**: `user.login.count:1|c\nresponse.time:245|ms|@0.1`
- **Processing**: Metric type detection, sampling rate handling

#### **graphite** - Graphite Plaintext Protocol
- **Description**: Graphite metrics format with dot-notation paths
- **Use Case**: System metrics, application performance data
- **Example**: `servers.web01.cpu.usage 65.4 1642248645`
- **Processing**: Metric path parsing, timestamp normalization

### Security and Event Formats

#### **cef** - Common Event Format (ArcSight)
- **Description**: ArcSight Common Event Format for security events
- **Use Case**: Security information and event management (SIEM)
- **Example**: `CEF:0|Microsoft|Windows|6.1|4624|User Logon|3|src=10.0.0.1 suser=jdoe`
- **Processing**: CEF parsing, extension field extraction, security event normalization

#### **gelf** - Graylog Extended Log Format
- **Description**: Structured logging format used by Graylog
- **Use Case**: Application logging, centralized log management
- **Example**: `{"version":"1.1","host":"web01","short_message":"User login","level":6}`
- **Processing**: JSON parsing with GELF-specific field handling

#### **leef** - Log Event Extended Format (IBM)
- **Description**: IBM's Log Event Extended Format for security events
- **Use Case**: QRadar SIEM, security event processing
- **Example**: `LEEF:2.0|Microsoft|Windows|6.1|4624|src=10.0.0.1|usrName=jdoe`
- **Processing**: LEEF parsing, field extraction, security context preservation

### Specialized Protocol Formats

#### **fix** - FIX Protocol
- **Description**: Financial Information eXchange protocol messages
- **Use Case**: Financial trading systems, order management
- **Example**: `8=FIX.4.4|9=122|35=D|49=SENDER|56=TARGET|52=20250115-10:30:45|`
- **Processing**: FIX message parsing, field extraction, message type identification

#### **hl7** - HL7 v2 Healthcare Messaging
- **Description**: Healthcare Level 7 version 2 messages
- **Use Case**: Healthcare systems integration, patient data exchange
- **Example**: `MSH|^~\&|LAB|HOSPITAL|EMR|CLINIC|20250115103045||ORU^R01|12345|P|2.3`
- **Processing**: HL7 segment parsing, field extraction, message validation

### Network Flow Formats (Parsed to NDJSON)

**Note**: These formats are now handled by dedicated plugins that parse binary flow data into NDJSON format for downstream processing.

#### **sflow** - sFlow Network Flow Monitoring
- **Description**: sFlow v5 network flow data parsed to NDJSON
- **Use Case**: Network traffic analysis, bandwidth monitoring
- **Plugin**: Use `type: "sflow"` plugin (not UDP plugin)
- **Processing**: Binary sFlow parsing via goflow2, output as NDJSON
- **Data Hint**: `ndjson` (flows are automatically converted to JSON)
- **Example Config**:
  ```yaml
  - type: "sflow"
    name: "sflow-collector"
    config:
      port: 6343
      dataset_id: "network-sflow"
      protocol: "sflow"
      data_hint: "ndjson"
  ```

#### **netflow** - NetFlow v5/v9 Network Flow Monitoring
- **Description**: Cisco NetFlow v5/v9 flow data parsed to NDJSON
- **Use Case**: Network traffic analysis, security monitoring
- **Plugin**: Use `type: "netflow"` plugin (not UDP plugin)
- **Processing**: Binary NetFlow parsing via goflow2, output as NDJSON
- **Data Hint**: `ndjson` (flows are automatically converted to JSON)
- **Example Config**:
  ```yaml
  - type: "netflow"
    name: "netflow-collector"
    config:
      port: 2055
      dataset_id: "network-netflow"
      protocol: "netflow"
      data_hint: "ndjson"
  ```

#### **ipfix** - IP Flow Information Export
- **Description**: IPFIX (IP Flow Information Export) data parsed to NDJSON
- **Use Case**: Standards-based network monitoring, traffic engineering
- **Plugin**: Use `type: "ipfix"` plugin (not UDP plugin)
- **Processing**: Binary IPFIX parsing via goflow2, output as NDJSON
- **Data Hint**: `ndjson` (flows are automatically converted to JSON)
- **Example Config**:
  ```yaml
  - type: "ipfix"
    name: "ipfix-collector"
    config:
      port: 4739
      dataset_id: "network-ipfix"
      protocol: "ipfix"
      data_hint: "ndjson"
  ```

### Default Format

#### **raw** - Raw Data (Default)
- **Description**: No specific format processing applied
- **Use Case**: Unknown formats, binary data, custom processing
- **Processing**: Stored as-is without parsing or transformation
- **Note**: This is the default when no data_hint is specified

## Configuration Examples

### Plugin Configuration with Data Hints

```yaml
inputs:
  - type: "udp"
    name: "syslog-receiver"
    config:
      port: 514
      dataset_id: "system-logs"
      data_hint: "syslog"  # Enable syslog parsing downstream

  - type: "http"
    name: "api-logs"
    config:
      port: 8080
      dataset_id: "api-events"
      data_hint: "ndjson"  # Enable JSON parsing and Parquet conversion

  - type: "kafka"
    name: "metrics-stream"
    config:
      topic: "application-metrics"
      dataset_id: "app-metrics"
      data_hint: "prom"    # Enable Prometheus metrics parsing
```

### Legacy UDP Listener Configuration

```yaml
udp:
  enabled: true
  listeners:
    - port: 514
      dataset_id: "syslog-data"
      data_hint: "syslog"
    - port: 9999
      dataset_id: "custom-logs"
      data_hint: "raw"     # Default processing
```

## Downstream Processing

### ByteFreezer-Packer Integration

The `bytefreezer-packer` component uses data hints to:

1. **Format Detection**: Automatically identify data structure
2. **Schema Inference**: Detect field types and relationships
3. **Parquet Conversion**: Convert structured formats to Parquet
4. **Compression Optimization**: Apply format-specific compression
5. **Index Creation**: Build appropriate indices for query optimization

### Processing Pipeline

```
Data Input → Plugin (data_hint) → Proxy → Receiver → Packer
                                                     ↓
                                            Format-Specific Processing
                                                     ↓
                                              Parquet Output
```

## Best Practices

### Choosing the Right Data Hint

1. **Match Source Format**: Use the hint that most closely matches your data format
2. **Consider Downstream Usage**: Think about how the data will be queried and analyzed
3. **Start Specific**: Begin with specific hints and fall back to `raw` if parsing fails
4. **Test Performance**: Some formats may have processing overhead

### Performance Considerations

- **Structured Formats** (`ndjson`, `csv`, `tsv`): Enable efficient Parquet conversion
- **Log Formats**: Provide automatic field extraction and indexing
- **Binary Formats**: Require more processing resources
- **Raw Format**: Minimal processing overhead but limited query capabilities

### Troubleshooting

If data processing fails with a specific hint:
1. Verify data format matches the expected structure
2. Check for malformed data or encoding issues
3. Consider using `raw` as a fallback
4. Review packer logs for parsing errors


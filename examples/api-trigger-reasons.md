# ByteFreezer Proxy API - Trigger Reason Examples

This document shows examples of API responses that include trigger reason information for batch processing analysis.

## DLQ Files API with Trigger Reasons

### Endpoint
```
GET /api/v2/dlq/files
```

### Example Response

```json
{
  "spooling_enabled": true,
  "files": [
    {
      "id": "batch_20250921_143022",
      "tenant_id": "customer-1",
      "dataset_id": "ebpf-data",
      "filename": "batch_20250921_143022.ndjson.gz",
      "size": 8192,
      "line_count": 50,
      "created_at": "2025-09-21T14:30:22Z",
      "last_retry": "2025-09-21T14:35:15Z",
      "retry_count": 3,
      "status": "dlq",
      "failure_reason": "HTTP 500: receiver temporary unavailable",
      "trigger_reason": "timeout"
    },
    {
      "id": "batch_20250921_143128",
      "tenant_id": "customer-1",
      "dataset_id": "web-logs",
      "filename": "batch_20250921_143128.ndjson.gz",
      "size": 10485760,
      "line_count": 2500,
      "created_at": "2025-09-21T14:31:28Z",
      "last_retry": "2025-09-21T14:36:20Z",
      "retry_count": 3,
      "status": "dlq",
      "failure_reason": "HTTP 401: authentication failed",
      "trigger_reason": "size_limit_reached"
    },
    {
      "id": "batch_20250921_143045",
      "tenant_id": "customer-2",
      "dataset_id": "metrics",
      "filename": "batch_20250921_143045.ndjson.gz",
      "size": 1024,
      "line_count": 5,
      "created_at": "2025-09-21T14:30:45Z",
      "last_retry": "2025-09-21T14:35:00Z",
      "retry_count": 2,
      "status": "dlq",
      "failure_reason": "connection timeout",
      "trigger_reason": "service_shutdown"
    }
  ],
  "message": "Found 3 files in DLQ"
}
```

## Trigger Reason Analysis

The `trigger_reason` field helps you understand batch processing behavior:

### Trigger Reason Values

| Value | Description | Analysis |
|-------|-------------|----------|
| `timeout` | Batch created due to timeout | May indicate low data volume or long timeout settings |
| `size_limit_reached` | Batch created due to size limits | Normal for high-volume data ingestion |
| `service_shutdown` | Batch created during shutdown | Should be minimal in healthy systems |
| `single_message` | Individual message processing | Indicates batching is disabled |
| `service_restart` | Recovered on startup | May indicate unclean shutdowns |

### Performance Analysis Examples

#### High Timeout Rate Analysis
```bash
# Check timeout ratio in DLQ
curl -s http://localhost:8088/api/v2/dlq/files | \
  jq '.files | group_by(.trigger_reason) | map({trigger: .[0].trigger_reason, count: length})'
```

Expected output:
```json
[
  {"trigger": "timeout", "count": 15},
  {"trigger": "size_limit_reached", "count": 3},
  {"trigger": "service_shutdown", "count": 1}
]
```

**Analysis**: High timeout count suggests:
- Low data volume
- Timeout too short for data patterns
- Consider increasing `batching.timeout_seconds` or decreasing `batching.max_bytes`

#### Size Limit Optimization
```bash
# Analyze size triggers vs timeout triggers
curl -s http://localhost:8088/api/v2/dlq/files | \
  jq '.files | map(select(.trigger_reason == "size_limit_reached")) | length'
```

**Analysis**: High size limit triggers indicate:
- Healthy high-volume processing
- May want to increase `batching.max_bytes` if receiver can handle larger batches
- Good data throughput

### Monitoring Queries

#### Check for Service Issues
```bash
# Find service restart triggers (may indicate instability)
curl -s http://localhost:8088/api/v2/dlq/files | \
  jq '.files | map(select(.trigger_reason == "service_restart"))'
```

#### Performance Tuning Data
```bash
# Get average batch sizes by trigger reason
curl -s http://localhost:8088/api/v2/dlq/files | \
  jq '.files | group_by(.trigger_reason) | map({
    trigger: .[0].trigger_reason,
    avg_size: (map(.size) | add / length),
    avg_lines: (map(.line_count) | add / length),
    count: length
  })'
```

### Configuration Tuning Recommendations

Based on trigger reason analysis:

#### Mostly `timeout` triggers:
```yaml
batching:
  timeout_seconds: 60  # Increase from 30
  max_bytes: 5242880   # Decrease from 10MB to trigger sooner
```

#### Mostly `size_limit_reached` triggers:
```yaml
batching:
  max_bytes: 20971520  # Increase from 10MB to 20MB
  timeout_seconds: 30  # Keep same - size is primary trigger
```

#### Many `service_restart` triggers:
- Investigate service stability
- Check for memory leaks or crashes
- Review system resources and limits

### Integration Examples

#### Prometheus Metrics Query
```promql
# Batch trigger reason distribution (if metrics implemented)
bytefreezer_proxy_batch_triggers_total
```

#### Grafana Dashboard Query
```sql
-- For visualization in Grafana with JSON data source
SELECT
  trigger_reason,
  COUNT(*) as count,
  AVG(size) as avg_size
FROM dlq_files
GROUP BY trigger_reason
```

### Troubleshooting Guide

#### Problem: All batches showing "timeout"
**Symptoms**: High timeout triggers, small batch sizes
**Solution**: Increase timeout or decrease size limits

#### Problem: All batches showing "size_limit_reached"
**Symptoms**: All batches hit size limit, consistent large sizes
**Solution**: May be optimal, or increase size limits if receiver can handle

#### Problem: Frequent "service_restart" triggers
**Symptoms**: Batches created on startup
**Solution**: Investigate service stability, check logs for crashes

#### Problem: Unexpected "single_message" triggers
**Symptoms**: No batching occurring
**Solution**: Check if batching is properly enabled in configuration
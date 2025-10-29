# ByteFreezer API Examples with New Filename Format

## HTTP Webhook Data Ingestion

### Basic UDP to HTTP Forwarding

```bash
# Send UDP data to proxy
echo "<134>$(date '+%b %d %H:%M:%S') myhost myapp: Test log message" | nc -u localhost 2056

# Proxy forwards to receiver as HTTP POST
# Generated filename: acme--logs--1736938245123456789--raw.gz
curl -X POST http://receiver:8080/webhook/acme/logs \
  -H "Content-Type: application/octet-stream" \
  -H "Content-Encoding: gzip" \
  -H "X-Proxy-Filename: acme--logs--1736938245123456789--raw.gz" \
  -H "X-Proxy-Data-Hint: raw" \
  -H "X-Proxy-Batch-ID: batch_1736938245123456789" \
  --data-binary @compressed_data.gz
```

### Different Data Types

#### Raw Log Data
```bash
# Filename: company--syslogs--1736938245123456789--raw.gz
curl -X POST http://receiver:8080/webhook/company/syslogs \
  -H "X-Proxy-Filename: company--syslogs--1736938245123456789--raw.gz" \
  -H "X-Proxy-Data-Hint: raw" \
  --data-binary @logs.raw.gz
```

#### CSV Metrics
```bash
# Filename: analytics--metrics--1736938245123456789--csv.gz
curl -X POST http://receiver:8080/webhook/analytics/metrics \
  -H "X-Proxy-Filename: analytics--metrics--1736938245123456789--csv.gz" \
  -H "X-Proxy-Data-Hint: csv" \
  --data-binary @metrics.csv.gz
```

#### NDJSON Events
```bash
# Filename: events--api--1736938245123456789--ndjson.gz
curl -X POST http://receiver:8080/webhook/events/api \
  -H "X-Proxy-Filename: events--api--1736938245123456789--ndjson.gz" \
  -H "X-Proxy-Data-Hint: ndjson" \
  --data-binary @events.ndjson.gz
```

## API Responses

### Successful Upload
```json
{
  "status": "success",
  "tenant_id": "acme",
  "dataset_id": "logs",
  "bytes_received": 2048,
  "bytes_processed": 1024,
  "storage_mode": "queue",
  "queue_file": "acme--logs--1736938245123456789--raw.gz",
  "file_id": "acme--logs--1736938245123456789--raw.gz",
  "lines_original": 25,
  "lines_processed": 25,
  "processing_time_ms": 12,
  "format_detected": "raw",
  "content_type": "application/octet-stream",
  "message": "Data stored in queue for batch processing",
  "filename_source": "proxy"
}
```

### Filename Validation Error
```json
{
  "error": "Filename does not match URL tenant/dataset",
  "status_code": 400,
  "details": "Expected tenant=acme, dataset=logs, got filename=company--metrics--123--raw.gz"
}
```

### Malformed Filename Error
```json
{
  "error": "Malformed filename detected - file stored for investigation",
  "status_code": 400,
  "details": "Filename contains invalid pattern: acme--logs--123..gz"
}
```

## File Upload API

### Direct File Upload
```bash
curl -X POST http://receiver:8080/upload/tenant1/dataset1 \
  -F "file=@data.csv" \
  -H "Authorization: Bearer your-token-here"
```

Response:
```json
{
  "status": "success",
  "tenant_id": "tenant1",
  "dataset_id": "dataset1",
  "filename": "data.csv",
  "bytes_received": 5120,
  "bytes_processed": 2560,
  "upload_id": "upload_1736938245123456789",
  "lines_processed": 100,
  "processing_time_ms": 45,
  "format_detected": "raw",
  "content_type": "text/csv",
  "s3_location": "s3://bucket/tenant1/dataset1/data.csv",
  "cached": true
}
```

## Health Check APIs

### Proxy Health
```bash
curl http://proxy:8088/api/v1/health
```

Response:
```json
{
  "status": "ok",
  "service": "bytefreezer-proxy",
  "version": "v2.0.0",
  "uptime": "2h34m12s",
  "plugins": {
    "udp": "active",
    "syslog": "active"
  },
  "spooling": {
    "enabled": true,
    "queue_files": 15,
    "retry_files": 3,
    "dlq_files": 0
  }
}
```

### Receiver Health
```bash
curl http://receiver:8080/health
```

Response:
```json
{
  "status": "ok",
  "service": "webhook"
}
```

## Monitoring APIs

### DLQ Statistics
```bash
curl http://proxy:8088/api/v1/dlq/stats
```

Response:
```json
{
  "total_files_in_queue": 42,
  "total_files_in_dlq": 3,
  "total_queue_size_bytes": 1048576,
  "total_dlq_size_bytes": 12288,
  "tenant_stats": {
    "acme": {
      "queue_files": 30,
      "dlq_files": 2,
      "queue_size_bytes": 786432,
      "dlq_size_bytes": 8192,
      "dataset_stats": {
        "logs": {
          "queue_files": 25,
          "dlq_files": 1,
          "oldest_file": "acme--logs--1736935245123456789--raw.gz",
          "oldest_age_seconds": 3600
        }
      }
    }
  }
}
```

### DLQ File Retry
```bash
curl -X POST http://proxy:8088/api/v1/dlq/retry/acme/logs
```

Response:
```json
{
  "files_retried": 2,
  "details": [
    {
      "file_id": "acme--logs--1736935245123456789--raw.gz",
      "success": true,
      "error": ""
    },
    {
      "file_id": "acme--logs--1736935345123456789--raw.gz",
      "success": true,
      "error": ""
    }
  ]
}
```

## Configuration Examples

### Proxy Configuration
```yaml
# config.yaml
app:
  name: "bytefreezer-proxy"
  version: "v2.0.0"

receiver:
  base_url: "http://receiver:8080/webhook/{tenantid}/{datasetid}"
  timeout_seconds: 30

spooling:
  enabled: true
  directory: "/var/spool/bytefreezer-proxy"
  max_size_bytes: 5368709120  # 5GB
  retry_attempts: 3
  retry_interval_seconds: 60

plugins:
  udp:
    enabled: true
    bind_address: "0.0.0.0:2056"
    tenant_mapping:
      "10.0.1.0/24": "acme"
      "10.0.2.0/24": "company"
    dataset: "logs"
    data_hint: "raw"
```

### Receiver Configuration
```yaml
# config.yaml
bytefreezer:
  spool_path: "/var/spool/bytefreezer-receiver"
  upload_worker_count: 5
  cache_threshold: 90.0

protocols:
  webhook:
    enabled: true
    max_payload_size: 104857600  # 100MB
  upload:
    enabled: true
    max_file_size: 104857600     # 100MB

s3:
  endpoint: "https://s3.amazonaws.com"
  region: "us-west-2"
  bucket: "bytefreezer-data"
  access_key_id: "${AWS_ACCESS_KEY_ID}"
  secret_access_key: "${AWS_SECRET_ACCESS_KEY}"
```

## File System Layout

### Proxy Spool Structure
```
/var/spool/bytefreezer-proxy/
├── acme/
│   └── logs/
│       ├── queue/
│       │   ├── acme--logs--1736938245123456789--raw.gz
│       │   └── acme--logs--1736938246123456789--raw.gz
│       ├── retry/
│       │   ├── acme--logs--1736935245123456789--raw.gz
│       │   └── acme--logs--1736935245123456789--raw.gz.meta
│       └── dlq/
│           ├── acme--logs--1736932245123456789--raw.gz
│           └── acme--logs--1736932245123456789--raw.gz.meta
└── company/
    └── metrics/
        └── queue/
            └── company--metrics--1736938245123456789--csv.gz
```

### Receiver Spool Structure
```
/var/spool/bytefreezer-receiver/
├── acme/
│   └── logs/
│       ├── queue/
│       │   └── acme--logs--1736938245123456789--raw.gz
│       ├── retry/
│       │   ├── acme--logs--1736938245123456789--raw.gz
│       │   └── acme--logs--1736938245123456789--raw.gz.meta
│       └── dlq/
│           ├── acme--logs--1736932245123456789--raw.gz
│           └── acme--logs--1736932245123456789--raw.gz.meta
├── malformed_proxy/
│   └── malformed_1736938245123456789_acme--logs--123..gz
└── malformed_local/
    └── malformed_1736938245123456789_corrupt_file.gz
```

## S3 Output Structure

```
s3://bytefreezer-data/
├── acme/
│   └── logs/
│       ├── acme--logs--1736938245123456789--raw.gz
│       └── acme--logs--1736938246123456789--raw.gz
└── company/
    └── metrics/
        └── company--metrics--1736938245123456789--csv.gz
```

Each S3 object includes comprehensive metadata:
- `tenant`, `dataset`, `data-type`
- `original-timestamp`, `original-filename`
- `compressed-size`, `uncompressed-size`, `line-count`
- `source`, `upload-timestamp`, `retry-count`
# ByteFreezer Proxy - Concurrent Upload Process Flow

## Overview
This document provides a detailed technical explanation of how the concurrent upload system works in the ByteFreezer proxy, including exact code paths, data flow, and error handling.

## System Architecture

### Components
1. **Plugin System**: UDP/TCP/HTTP data ingestion
2. **Batch Processor**: Data accumulation and batching
3. **Upload Worker Pool**: Concurrent upload processing (5 workers by default)
4. **Spooling Service**: File-based persistence and retry logic
5. **HTTP Forwarder**: Connection-pooled HTTP client for receiver communication

### Worker Pool Pattern (Aligned with Receiver)
```go
type UploadWorker struct {
    id            int
    pluginService *PluginService
    uploadChannel <-chan *domain.DataBatch
}

func (w *UploadWorker) run(ctx context.Context, wg *sync.WaitGroup) {
    // Worker loop processing upload channel
}
```

## Detailed Process Flow

### 1. Data Ingestion Phase

**Entry Point**: `plugin_service.go:processPluginMessage()`

```
UDP Packet → Plugin Manager → Input Channel → processPluginMessage()
```

**Code Path**:
1. Plugin receives UDP data and creates `DataMessage`
2. Message sent to `inputChannel` (buffered, 10000 capacity)
3. `processInputMessages()` goroutine processes channel
4. Calls `processPluginMessage()` which routes to batch processor

### 2. Batching Phase

**Entry Point**: `plugin_service.go:processPluginMessage()` → `batchProcessor.AddMessage()`

```
DataMessage → BatchProcessor → Batch Accumulation → Trigger Threshold → forwardBatch()
```

**Batch Triggers** (with trigger reason tracking):
- **Size**: Lines disabled (0) OR 20MB bytes → `trigger_reason: "size_limit_reached"`
- **Time**: 60 seconds since first message in batch → `trigger_reason: "timeout"`
- **Force**: Manual flush or shutdown → `trigger_reason: "service_shutdown"`
- **Single**: Batching disabled → `trigger_reason: "single_message"`

### 3. Spool-First Architecture

**Entry Point**: `plugin_service.go:forwardBatch()`

```
DataBatch → spoolBatch() → File Written to /queue → uploadChannel ← Notification
```

**Critical Path** (`plugin_service.go:forwardBatch()`):
```go
// SPOOL FIRST - preserve data safety
if err := ps.spoolBatch(batch); err != nil {
    log.Errorf("CRITICAL: Failed to spool batch %s - DATA LOSS: %v", batch.ID, err)
    return
}

// NOTIFY UPLOADER - trigger immediate upload attempt
select {
case ps.uploadChannel <- batch:
    log.Debugf("Batch %s queued for immediate upload", batch.ID)
default:
    // Channel at capacity - move to retry
    ps.spoolingService.MoveQueueToRetry(batch.TenantID, batch.DatasetID, batch.ID, "channel_capacity_exceeded")
}
```

**File Operations**:
1. **Generate File Name**: `{tenant}--{dataset}--{timestamp}--{extension}.gz`
2. **Spool Path**: `/var/spool/bytefreezer-proxy/{tenant}/{dataset}/queue/{tenant}--{dataset}--{timestamp}--{extension}.gz`
3. **Data Safety**: File written with compressed data BEFORE upload attempt
4. **No Metadata**: Queue files are data-only (no .json metadata)

### 4. Concurrent Upload Processing

**Entry Point**: `plugin_service.go:UploadWorker.run()`

```
uploadChannel → Worker Pool (5 workers) → attemptImmediateUpload() → HTTP Forwarder
```

**Worker Loop**:
```go
func (w *UploadWorker) run(ctx context.Context, wg *sync.WaitGroup) {
    for {
        select {
        case batch := <-w.uploadChannel:
            w.pluginService.attemptImmediateUpload(batch)
        case <-ctx.Done():
            return
        }
    }
}
```

**Upload Process** (`plugin_service.go:attemptImmediateUpload()`):
1. **Read Spooled File**: Load compressed data from `/queue/{tenant}--{dataset}--{timestamp}--{extension}.gz`
2. **HTTP Forward**: `forwarder.ForwardBatch(batch)` with connection pooling
3. **Success Path**: `RemoveFromQueue()` - delete file from `/queue`
4. **Failure Path**: `MoveQueueToRetry()` - move to `/retry` with metadata including trigger reason

### 5. HTTP Connection Pooling

**Entry Point**: `forwarder.go:NewHTTPForwarder()`

```
HTTP Transport → Connection Pool → Persistent Connections → Receiver
```

**Connection Configuration**:
```go
transport := &http.Transport{
    DialContext: (&net.Dialer{
        Timeout:   30 * time.Second,
        KeepAlive: 30 * time.Second,
    }).DialContext,
    MaxIdleConns:        cfg.GetMaxIdleConns(),        // 10
    MaxIdleConnsPerHost: cfg.GetMaxConnsPerHost(),     // 6
    MaxConnsPerHost:     cfg.GetMaxConnsPerHost(),     // 6
    IdleConnTimeout:     90 * time.Second,
    TLSHandshakeTimeout: 10 * time.Second,
    DisableCompression:  false,
}
```

**Performance Benefits**:
- **Connection Reuse**: Eliminates TCP handshake overhead
- **Concurrency**: 5 workers × 6 connections = up to 30 concurrent uploads
- **Efficiency**: Persistent connections with 90s keep-alive

### 6. Retry Processing

**Entry Point**: `spooling.go:retryWorker()` (timer-based, every 30s)

```
Timer Trigger → collectRetryJobs() → Worker Pool → processRetryJob() → Upload or DLQ
```

**Retry Job Collection**:
1. **Scan Directories**: `/var/spool/bytefreezer-proxy/{tenant}/{dataset}/retry/`
2. **Find Metadata**: Files ending with `.meta` (includes trigger_reason)
3. **Validate Data**: Ensure corresponding data file exists
4. **Create Jobs**: `RetryJob{TenantID, DatasetID, BatchID, FilePath, MetaPath}`

**Metadata Structure** (`.meta` files):
```json
{
  "id": "20250921-143022--customer-1--ebpf-data",
  "tenant_id": "customer-1",
  "dataset_id": "ebpf-data",
  "trigger_reason": "timeout",
  "status": "retry",
  "retry_count": 1,
  "failure_reason": "HTTP 500: receiver temporary error"
}
```

**Concurrent Retry Processing**:
```go
// Create job channel and worker pool
jobChannel := make(chan RetryJob, len(jobs))
numWorkers := s.config.GetUploadWorkerCount()  // Same as upload workers

for i := 0; i < numWorkers; i++ {
    go s.retryJobWorker(i, jobChannel, resultChannel, &wg)
}
```

**Retry Decision Logic**:
1. **Check Retry Count**: `metadata.RetryCount >= s.config.Spooling.RetryAttempts`
2. **Under Limit**: Attempt upload, increment retry count
3. **Over Limit**: Move to DLQ with reason "max_retries_exceeded"

### 7. Dead Letter Queue (DLQ)

**Entry Point**: `spooling.go:moveToDLQ()`

```
Max Retries Exceeded → moveToDLQ() → /dlq Directory → SOC Alert
```

**DLQ Process**:
1. **Directory**: `/var/spool/bytefreezer-proxy/{tenant}/{dataset}/dlq/`
2. **Files**: Both data file and `.json` metadata
3. **Metadata Update**: `Status: "dlq"`, `FailureReason: "max_retries_exceeded"`
4. **Protection**: DLQ files are NEVER deleted by cleanup processes
5. **Alerting**: SOC notification for permanent failures

### 8. Queue File Recovery (Startup)

**Entry Point**: `spooling.go:processOrphanedQueueFiles()` (startup only)

```
Startup → Scan /queue → Move to /retry → Fresh Retry Count
```

**Recovery Logic**:
```go
// Queue files have no metadata, so create fresh metadata
metadata := SpooledFile{
    RetryCount:    0,  // Fresh start - gets full retry attempts
    Status:        "retry",
    FailureReason: "Recovered from orphaned queue file on startup",
}
```

**Data Safety**: Prevents data loss from proxy restarts by moving orphaned queue files to retry system.

## Error Handling & Edge Cases

### Channel Overflow
```go
select {
case ps.uploadChannel <- batch:
    // Success
default:
    // Channel at capacity (1000 items) - move directly to retry
    ps.spoolingService.MoveQueueToRetry(batch.TenantID, batch.DatasetID, batch.ID, "channel_capacity_exceeded")
}
```

### File System Errors
- **Spool Failure**: CRITICAL error logged, data loss prevented by not proceeding
- **Move Operations**: Atomic file moves with fallback to copy+delete
- **Cleanup Failures**: Non-fatal, logged for monitoring

### Network Errors
- **HTTP Timeouts**: Configurable via `timeout_seconds` (default: 30s)
- **Connection Failures**: Automatic retry via retry system
- **Receiver Errors**: HTTP status codes logged with full context

### Worker Pool Shutdown
```go
// Graceful shutdown sequence
ps.cancel()           // Cancel context
close(ps.uploadChannel) // Close upload channel
ps.wg.Wait()          // Wait for all workers to finish
```

## Performance Characteristics

### Throughput
- **Sequential (Old)**: 1 upload at a time
- **Concurrent (New)**: 5 uploads simultaneously
- **Theoretical Max**: 5× throughput improvement
- **Real-world**: 3-4× improvement due to network/receiver constraints

### Latency
- **Immediate Mode**: Data uploaded as soon as batch is ready
- **Connection Reuse**: Eliminates TCP handshake latency
- **Buffered Channel**: 1000-item buffer prevents blocking

### Resource Usage
- **Memory**: ~1000 batches buffered in upload channel
- **Connections**: Up to 6 persistent connections to receiver
- **Goroutines**: 5 upload workers + 1 batch processor + 1 retry worker
- **Disk**: Temporary storage in `/queue` until upload succeeds

## Monitoring & Observability

### Log Messages
```
# Worker startup
"Upload worker {id} started"

# Batch processing
"Worker {id} processing batch {batch_id}"

# Upload results
"✅ Immediate upload successful for batch {batch_id} ({bytes} bytes, {lines} lines)"
"Immediate upload failed for batch {batch_id}: {error} - moving to retry"

# Retry processing
"Processing {count} retry jobs with {workers} upload workers"
"✅ Retry upload successful for batch {batch_id} ({bytes} bytes, {lines} lines)"

# DLQ operations
"Batch {batch_id} exceeded max retries ({max}), moving to DLQ"
```

### Metrics (Future)
- Upload success/failure rates
- Worker utilization
- Queue depths
- Retry counts
- DLQ growth

## Configuration Reference

```yaml
receiver:
  # Worker configuration (aligned with receiver)
  upload_worker_count: 5    # Number of concurrent upload workers

  # Connection pooling
  max_idle_conns: 10        # HTTP connection pool size
  max_conns_per_host: 6     # Max connections per receiver host

  # Timeouts and retries
  timeout_seconds: 30       # HTTP request timeout
  retry_count: 3           # Max retry attempts (receiver-level)
  retry_delay_seconds: 1    # Delay between receiver retries

spooling:
  # Retry configuration
  retry_attempts: 4         # Max retry attempts before DLQ
  retry_interval_seconds: 30 # Retry processing interval

  # Cleanup
  cleanup_interval_seconds: 300  # Cleanup interval (5 minutes)

batching:
  # Batch triggers
  max_lines: 1000          # Lines per batch
  max_bytes: 1048576       # Bytes per batch (1MB)
  max_interval_seconds: 30  # Time-based batching
```

## Troubleshooting Guide

### High Upload Channel Usage
**Symptoms**: Log message "Upload channel at capacity"
**Cause**: Receiver is slower than data ingestion rate
**Solutions**:
- Increase `upload_worker_count`
- Increase `max_conns_per_host`
- Check receiver performance

### Files Stuck in Retry
**Symptoms**: Growing `/retry` directory
**Cause**: Receiver connectivity issues
**Solutions**:
- Check receiver availability
- Review receiver error logs
- Verify network connectivity

### DLQ Growth
**Symptoms**: Files accumulating in `/dlq`
**Cause**: Persistent receiver issues or configuration problems
**Solutions**:
- Check receiver configuration
- Verify authentication tokens
- Review network/firewall settings

### High Memory Usage
**Symptoms**: Increasing memory consumption
**Cause**: Large batches or slow upload processing
**Solutions**:
- Reduce `max_lines` or `max_bytes` in batching config
- Increase `upload_worker_count` for faster processing
- Monitor receiver performance

This concurrent upload system provides high-performance, reliable data delivery while maintaining data safety through the spool-first architecture.
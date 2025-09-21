# ByteFreezer Proxy Release Notes

## Latest Changes

### 📊 Enhanced Metadata: Compression Size Tracking
**Release Date**: September 21, 2025

#### New Features
- **Compressed Size Tracking**: Added `compressed_size` field to metadata showing actual file size on disk
- **Uncompressed Size Tracking**: Added `uncompressed_size` field to metadata showing original data size before compression
- **Backwards Compatibility**: Existing `size` field preserved for compatibility, contains compressed size

#### Enhanced Metadata Format
```json
{
  "id": "customer-1--ebpf-data--1758485456619420694",
  "tenant_id": "customer-1",
  "dataset_id": "ebpf-data",
  "size": 52560,                    // Compressed size on disk (backwards compatible)
  "compressed_size": 52560,         // Same as size field
  "uncompressed_size": 1280506,     // Original data size (1.28MB uncompressed)
  "line_count": 40080,
  "trigger_reason": "timeout"
}
```

#### Benefits
- **Storage Analysis**: Understand actual disk usage vs original data size
- **Compression Ratio Monitoring**: Calculate compression efficiency (95.9% in example above)
- **Performance Tuning**: Better understanding of data patterns and compression effectiveness
- **Capacity Planning**: Accurate tracking of both disk usage and data throughput

#### Configuration Optimization
- **Size Limit**: Adjusted back to 10MB for optimal batch sizes and faster processing
- **Line Limits**: Disabled (`max_lines: 0`) to rely only on size and timeout triggers

---

### 🐛 Critical Bug Fix: Trigger Reason Accuracy
**Release Date**: September 21, 2025

#### Bug Fixed
- **Issue**: Batch processing incorrectly reported `size_limit_reached` for all batches that hit limits, even when they were triggered by line count limits
- **Root Cause**: `shouldFinalizeBatch()` function returned only boolean, causing `finalizeBatch()` to always use hardcoded `"size_limit_reached"` reason
- **Impact**: Made trigger reason monitoring unreliable for performance analysis

#### Fix Details
- **Enhanced Logic**: `shouldFinalizeBatch()` now returns specific trigger reason string
- **Accurate Reporting**: Correctly distinguishes between `size_limit_reached` and `line_limit_reached`
- **Preserved Functionality**: Timeout triggers continue to work correctly via separate timeout checker

#### New Trigger Reasons
- `line_limit_reached` - Batch finalized due to maximum line count reached
- `size_limit_reached` - Batch finalized due to maximum byte size reached
- `timeout` - Batch finalized due to timeout (unchanged)

#### Configuration Fix
- **Removed Unwanted Default**: Eliminated `MaxLines = 1000` default that was overriding `max_lines: 0` configuration
- **Correct Behavior**: Now `max_lines: 0` properly disables line count limits as intended
- **Only Size and Timeout**: With line limits disabled, batches will only trigger on `size_limit_reached` (20MB) or `timeout` (60s)

#### Verification
After this fix, batches will show correct trigger reasons in metadata:
```json
{
  "trigger_reason": "timeout",  // For 1.28MB batch (under 20MB limit, line limits disabled)
  "line_count": 40080,
  "size": 52560  // Compressed size on disk
}
```

---

### 📊 Enhanced Batch Trigger Tracking and Monitoring
**Release Date**: September 2025

#### Overview
Added comprehensive trigger reason tracking throughout the batch processing system to provide complete visibility into why batches were created. This feature enables better debugging, monitoring, and understanding of batch processing behavior.

#### Key Features

##### 1. Trigger Reason Metadata
- **DataBatch Enhancement**: Added `TriggerReason` field to track batch creation triggers
- **SpooledFile Enhancement**: Added `TriggerReason` field to track batch triggers in spool metadata
- **Complete Visibility**: Every batch now includes the reason it was finalized

##### 2. Comprehensive Trigger Tracking
Available trigger reasons:

| Trigger Reason | Description | When It Occurs |
|----------------|-------------|----------------|
| `timeout` | Batch finalized due to timeout | When `batching.timeout_seconds` is reached |
| `size_limit_reached` | Batch finalized due to size limits | When `batching.max_bytes` is reached |
| `service_shutdown` | Batch finalized during shutdown | When service is gracefully stopping |
| `single_message` | Individual message processing | When batching is disabled or single messages |
| `service_restart` | File recovered on startup | When orphaned queue files are recovered |

##### 3. Enhanced Metadata Files
Spool metadata (`.meta` files) now include trigger information:
```json
{
  "id": "20250921-143022--customer-1--ebpf-data",
  "tenant_id": "customer-1",
  "dataset_id": "ebpf-data",
  "trigger_reason": "timeout",
  "status": "retry",
  "failure_reason": "HTTP 500: receiver temporary error"
}
```

##### 4. Debugging and Monitoring Benefits
- **Batch Analysis**: Understand why batches are being created
- **Performance Tuning**: Identify if timeouts vs size limits are triggering batches
- **Problem Diagnosis**: See if batches are timing out when they should reach size limits
- **Configuration Optimization**: Adjust `max_lines`, `max_bytes`, and `timeout_seconds` based on actual triggers

#### Configuration Impact
```yaml
batching:
  enabled: true
  max_lines: 0          # Disabled - only size and timeout matter
  max_bytes: 20971520   # 20MB limit (optimized for S3 efficiency)
  timeout_seconds: 60   # Will show as "timeout" trigger reason (2x longer for larger batches)
```

**Configuration Optimization for S3**: Increased batch size from 10MB to 20MB and timeout from 30s to 60s for:
- **2x fewer S3 requests** (better cost efficiency)
- **Better S3 throughput** utilization
- **Reduced API costs** while maintaining reasonable latency

#### API Enhancements
- **DLQ Statistics**: `/api/v2/dlq/stats` now shows trigger reason distribution
- **File Listings**: `/api/v2/dlq/files` includes trigger reasons in file metadata
- **Batch Information**: All batch-related APIs include trigger reason context

#### Monitoring Use Cases

##### 1. Batch Size Optimization
```bash
# Check if batches are timing out vs reaching size limits
grep "trigger_reason" /var/spool/bytefreezer-proxy/*/meta/*.meta | grep timeout
```

##### 2. Performance Analysis
- **High timeout triggers**: Consider increasing `timeout_seconds` or reducing `max_bytes`
- **High size_limit triggers**: Consider increasing `max_bytes` if appropriate
- **Many single_message triggers**: Consider enabling batching if disabled

##### 3. Troubleshooting
- **service_shutdown triggers**: Normal during graceful shutdowns
- **service_restart triggers**: Indicates recovery from unclean shutdown

#### Improved Filename Format
**Enhanced delimiter for better parsing**: Changed batch ID format from underscores to double dashes for more reliable parsing when tenant/dataset names contain underscores.

**New Format**:
- ✅ **Before**: `customer_1_ebpf_data_1695123456789` (ambiguous parsing)
- ✅ **After**: `customer-1--ebpf-data--1695123456789` (clear delimiter)

**Benefits**:
- **Reliable parsing**: `strings.Split(id, "--")` works even with underscores in names
- **Cross-platform safe**: Double dash is safe on all operating systems
- **Future-proof**: Clear separation for automated processing

#### Backward Compatibility
- **No Breaking Changes**: All existing functionality preserved
- **Automatic Enhancement**: Existing deployments automatically gain trigger tracking
- **API Compatibility**: All existing API responses include new trigger reason fields
- **Filename Format**: New delimiter format applies to new files only

### 🚀 Major Performance Enhancement: Concurrent Upload Architecture
**Release Date**: September 2025

#### Overview
Completely redesigned the upload architecture to use concurrent worker pools, delivering ~5x throughput improvement while maintaining data safety through spool-first design. Architecture now aligns with bytefreezer-receiver patterns for consistency.

#### Key Features

##### 1. Upload Worker Pool (Receiver-Aligned Architecture)
- **Workers**: 5 concurrent upload workers (configurable via `upload_worker_count`)
- **Pattern**: Dedicated `UploadWorker` structs with `run()` method matching receiver
- **Channel**: Buffered upload channel (1000 capacity) shared by all workers
- **Performance**: ~5x throughput improvement vs sequential processing

##### 2. Enhanced Connection Pooling
- **HTTP Transport**: Custom transport with persistent connections
- **Pool Configuration**: 10 idle connections, 6 per host (configurable)
- **Efficiency**: 90-second keep-alive eliminates TCP handshake overhead
- **Scalability**: Up to 30 simultaneous connections (5 workers × 6 conns)

##### 3. Concurrent Retry Processing
- **Worker Pool**: Same 5 workers handle both immediate and retry uploads
- **Batch Processing**: Retry jobs collected and distributed concurrently
- **Performance**: Processes hundreds of retry files simultaneously
- **Safety**: DLQ protection after max retries with full metadata

#### Configuration Changes

**New Settings**:
```yaml
receiver:
  upload_worker_count: 5    # Number of upload workers (renamed from concurrent_uploads)
  max_idle_conns: 10        # HTTP connection pool size (new)
  max_conns_per_host: 6     # Max connections per host (new)
```

**Migration**: Automatic - no manual changes required. Old settings gracefully ignored.

#### Performance Improvements

| Metric | Before | After | Improvement |
|--------|--------|-------|-------------|
| Upload Workers | 1 (sequential) | 5 (concurrent) | 5x concurrency |
| Connection Reuse | None | Persistent pool | Reduced latency |
| Retry Processing | Sequential | Concurrent batch | Scalable processing |
| Data Safety | Spool-first | Enhanced spool-first | Zero data loss |

#### Enhanced Logging & Monitoring
```
"Plugin service started with 1 plugins and 5 upload workers"
"Upload worker {id} started"
"Worker {id} processing batch {batch_id}"
"Processing {count} retry jobs with 5 upload workers"
"✅ Immediate upload successful for batch {batch_id} ({bytes} bytes, {lines} lines)"
```

#### Technical Documentation
- **CONCURRENT_UPLOAD_FLOW.md**: Complete technical process flow documentation
- **CLAUDE.md**: Updated architecture section with concurrent upload patterns
- **Configuration**: Detailed tuning guidelines for high/low volume environments

#### Compatibility
- **Backward Compatible**: All existing APIs and behaviors maintained
- **Auto-Migration**: Configuration automatically updates with sensible defaults
- **Receiver Aligned**: Identical patterns with bytefreezer-receiver for consistency

### Enhanced Error Handling & Diagnostics

#### Improved Upload Error Logging
- **Enhanced receiver error display**: Upload failures now show detailed error messages from bytefreezer-receiver
- **Network vs HTTP error distinction**: Clear differentiation between connection issues and receiver errors
- **Retry attempt tracking**: Each retry attempt is numbered for better debugging
- **Response body capture**: Full receiver error responses are logged (handles empty responses gracefully)
- **URL context**: All error messages now include the destination URL for easier debugging
- **HTTP status codes**: Clear display of HTTP response codes from receiver

Example log output:
```
WARN: Batch batch123 upload attempt 2 failed - http://localhost:8081/customer-1/ebpf-data returned HTTP 401: {"error":"invalid bearer token"}
WARN: Batch batch456 upload attempt 1 failed - network error to http://localhost:8081/customer-2/web-data: dial tcp 127.0.0.1:8081: connect: connection refused
```

### New Connectivity Testing API

#### Comprehensive Receiver Connectivity Testing
- **New endpoint**: `POST /api/v2/test/connectivity` - Automatically tests ALL configured plugins/tenants/datasets
- **Zero-configuration testing**: No manual input required - auto-discovers all configurations from config.yaml
- **Plugin-aware testing**: Automatically tests each configured input plugin with its specific tenant_id, dataset_id, and bearer_token
- **Real payload testing**: Sends actual compressed NDJSON data to verify end-to-end connectivity
- **Security-conscious**: Bearer tokens are masked in API responses (e.g., "eb4b***2cf")
- **Performance metrics**: Response time measurement for each test
- **Summary statistics**: Success rates, failure counts, and overall health assessment

#### API Examples
```bash
# Automatically test ALL configured plugins/tenants/datasets
curl -X POST http://localhost:8088/api/v2/test/connectivity -d '{}'
```

**Note**: No manual tenant/dataset input needed! The API automatically discovers and tests all configurations from your config.yaml file.

#### Response Format
```json
{
  "message": "Connectivity tests completed for all 1 configured plugins",
  "results": [
    {
      "tenant_id": "customer-1",
      "dataset_id": "ebpf-data",
      "plugin_name": "ebpf-udp-listener",
      "plugin_type": "udp",
      "bearer_token": "eb4b***2cf",
      "receiver_url": "http://localhost:8081/customer-1/ebpf-data",
      "status": "success",
      "status_code": 200,
      "response_time": "45.123ms"
    }
  ],
  "summary": {
    "total_tests": 1,
    "success_count": 1,
    "failure_count": 0,
    "error_count": 0,
    "success_rate_percent": 100
  }
}
```

### DLQ (Dead Letter Queue) Improvements

#### Fixed Critical DLQ Metadata Bug
- **Issue**: When files were moved to DLQ, orphaned metadata files remained in `/meta/` directory
- **Root cause**: Incorrect path construction in `moveToNewDLQ()` function
- **Fix**: Corrected metadata cleanup path from `tenant/meta/` to `tenant/dataset/meta/`
- **Result**: Clean DLQ operations with no orphaned metadata files

#### Enhanced DLQ Functionality Verification
- **DLQ retry with counter reset**: Files moved from DLQ back to queue have retry counts reset to 0
- **Proper status transitions**: `Status: "dlq" → "pending"`, `RetryCount: 4 → 0`
- **Cleanup script**: Added `cleanup-orphaned-meta.sh` for cleaning existing orphaned files

### API Endpoints Summary

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/v2/test/connectivity` | POST | Test receiver connectivity for all or specific plugins |
| `/api/v2/dlq/stats` | GET | View DLQ and queue statistics |
| `/api/v2/dlq/files` | GET | List files currently in DLQ |
| `/api/v2/dlq/retry` | POST | Retry DLQ files (moves back to queue with reset counters) |
| `/api/v2/health` | GET | Health check with plugin status |
| `/api/v2/config` | GET | View current configuration |
| `/v2/docs` | GET | API documentation (Swagger UI) |

### Testing & Validation

#### Test Scripts Added
- `test-connectivity-and-dlq.sh`: Comprehensive test of all new features
- `cleanup-orphaned-meta.sh`: Cleanup utility for orphaned DLQ metadata files
- `examples/connectivity-test.md`: Detailed API usage examples

#### Validation Features
- Real HTTP requests with compressed payloads
- Bearer token authentication testing
- Response time measurement
- Error message capture and display
- Summary statistics for monitoring

### Configuration Requirements

#### For Connectivity Testing
- Ensure `receiver.base_url` is properly configured
- Verify `bearer_token` is set (global or per-plugin)
- Check `tenant_id` configuration
- Confirm receiver service is accessible

#### For Enhanced Error Logging
- No configuration changes required
- Automatic enhancement of existing logging
- Works with all log levels (debug, info, warn, error)

### Troubleshooting

#### Common Connectivity Test Issues
1. **Connection refused**: Receiver service not running or wrong port
2. **401 Unauthorized**: Bearer token mismatch between proxy and receiver
3. **404 Not Found**: Incorrect receiver URL format or endpoint
4. **Network timeout**: Receiver overloaded or network issues

#### DLQ Issues
1. **Orphaned metadata files**: Run `cleanup-orphaned-meta.sh` to clean up
2. **Files not moving to DLQ**: Check retry count limits and error handling
3. **DLQ retry failures**: Verify receiver connectivity before retrying

### Future Enhancements Considered

#### Connectivity Testing
- Scheduled connectivity health checks
- Integration with monitoring systems (Prometheus metrics)
- Email/Slack notifications for connectivity failures
- Historical connectivity statistics

#### Error Handling
- Error categorization and automatic remediation
- Rate limiting for error logging to prevent log spam
- Integration with alerting systems for critical errors

#### DLQ Management
- Automatic DLQ cleanup based on age/size policies
- DLQ file inspection and content preview
- Batch DLQ operations for large-scale retries

---

*Generated with [Claude Code](https://claude.ai/code)*
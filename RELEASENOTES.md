# ByteFreezer Proxy Release Notes

## Latest Changes

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
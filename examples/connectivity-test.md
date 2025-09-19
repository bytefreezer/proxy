# Connectivity Testing Examples

## Test All Configured Plugins/Tenants

Test connectivity for all configured input plugins:

```bash
curl -X POST http://localhost:8088/api/v2/test/connectivity \
  -H "Content-Type: application/json" \
  -d '{}'
```

## Test Specific Tenant/Dataset

Test connectivity for a specific tenant and dataset:

```bash
curl -X POST http://localhost:8088/api/v2/test/connectivity \
  -H "Content-Type: application/json" \
  -d '{
    "tenant_id": "customer-1",
    "dataset_id": "ebpf-data"
  }'
```

## Sample Response

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
      "response_time": "45.123ms",
      "response_body": "{\"status\":\"success\",\"tenant_id\":\"customer-1\"}"
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

## Error Response Example

When receiver is down or authentication fails:

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
      "status": "failed",
      "status_code": 401,
      "response_time": "12.456ms",
      "error_message": "HTTP 401: Authentication required",
      "response_body": "{\"error\":\"invalid bearer token\"}"
    }
  ],
  "summary": {
    "total_tests": 1,
    "success_count": 0,
    "failure_count": 1,
    "error_count": 0,
    "success_rate_percent": 0
  }
}
```

## Status Types

- **success**: Connection successful (HTTP 2xx)
- **failed**: Connection failed with HTTP error (HTTP 4xx/5xx)
- **error**: Network error, timeout, or other system error

## Features

- Tests actual HTTP POST with compressed NDJSON payload
- Uses correct bearer tokens for each plugin/tenant
- Shows detailed error messages from receiver
- Measures response time
- Masks bearer tokens in output for security
- Provides summary statistics
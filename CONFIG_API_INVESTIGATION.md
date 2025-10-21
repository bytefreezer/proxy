# ByteFreezer Control API Configuration Endpoints Investigation

## Executive Summary

The bytefreezer-control service exposes comprehensive REST APIs for proxy configuration management and health monitoring. Proxies identify themselves using hostname-based instance IDs and poll/push their status to control. Plugin schemas are discovered dynamically from proxy health reports.

---

## API Architecture Overview

### Key Components
1. **Control Service**: Provides API endpoints for managing configuration, health monitoring, and service discovery
2. **Health Service**: Stores and manages health status for all services (stored in PostgreSQL)
3. **Storage Service**: Manages accounts, tenants, datasets, and user data
4. **Health Reporting Service** (Proxy-side): Sends health reports and plugin schemas to control

### Communication Pattern
```
Proxy (startup)
  ├─ Register with Control: POST /api/v1/health/register
  ├─ Poll tenant config: GET /api/v1/tenants/{tenantId}
  └─ Report health periodically: POST /api/v1/services/report
  
Control
  ├─ Store health records in database
  ├─ Poll proxy health: GET /api/v1/health/status
  └─ Aggregate plugin schemas: GET /api/v1/plugins
```

---

## API Endpoint Reference

### 1. Configuration Endpoints

#### Get Control Configuration
```
GET /api/v1/config

Response (ConfigResponse):
{
  "app": {
    "name": "bytefreezer-control",
    "version": "1.0.0"
  },
  "database": {
    "enabled": true,
    "type": "postgresql",
    "host": "localhost",
    "port": 5432
  },
  "auth": {
    "enabled": true,
    "token_expiry_hours": 24,
    "admin_users_count": 1
  },
  "rate_limit": {
    "enabled": true,
    "requests_per_minute": 1000,
    "burst_size": 100
  }
}
```

#### Update Control Configuration
```
PUT /api/v1/config

Request Body:
{
  "auth": {
    "enabled": true,
    "token_expiry_hours": 48
  },
  "rate_limit": {
    "enabled": true,
    "requests_per_minute": 2000,
    "burst_size": 200
  }
}

Response: ConfigResponse (same as GET)
```

---

### 2. Tenant Configuration Endpoints

#### Get Tenant (Direct Lookup - For Proxy Validation)
```
GET /api/v1/tenants/{tenantId}

Path Parameters:
  - tenantId: Tenant identifier (required)

Response (Tenant):
{
  "id": "acme-prod",
  "account_id": "acc-12345",
  "name": "ACME Production",
  "display_name": "ACME Production Tenant",
  "description": "Production tenant for ACME Corp",
  "active": true,
  "config": {
    "metadata": {
      "environment": "production",
      "region": "us-west-2"
    },
    "max_datasets": 50,
    "storage_quota_gb": 1000,
    "custom_settings": {}
  },
  "created_at": "2024-10-21T12:00:00Z",
  "updated_at": "2024-10-21T12:00:00Z"
}

Status Codes:
  - 200: Success
  - 404: Tenant not found
  - 500: Internal server error
```

**Purpose**: Proxies use this endpoint to validate their tenant_id and fetch tenant configuration without needing account context.

#### Get Tenant (Account-Scoped)
```
GET /api/v1/accounts/{accountId}/tenants/{tenantId}

Path Parameters:
  - accountId: Account identifier (required)
  - tenantId: Tenant identifier (required)

Response: Same as direct lookup above

Note: Requires account context; primarily used by UI and admin tools
```

---

### 3. Health Monitoring & Registration Endpoints

#### Register Service Instance
```
POST /api/v1/health/register

Request Body (ServiceRegistrationRequest):
{
  "service_type": "bytefreezer-proxy",
  "instance_id": "hostname-1",
  "instance_api": "hostname-1:8080",
  "status": "Starting",
  "configuration": {
    // Full configuration data - see section below
  }
}

Response (ServiceRegistrationResponse):
{
  "success": true,
  "message": "Service bytefreezer-proxy registered successfully",
  "instance_id": "hostname-1",
  "service_type": "bytefreezer-proxy"
}
```

**Instance Identification**:
- If `instance_id` not provided, control uses hostname from `os.Hostname()`
- `instance_api` format: `{hostname}:{port}` (no protocol)
- Used for all subsequent health reports and lookups

#### Report Service Health
```
POST /api/v1/services/report

Request Body (ServiceReport):
{
  "service_name": "bytefreezer-proxy",
  "service_id": "hostname-1",
  "instance_api": "hostname-1:8080",
  "healthy": true,
  "configuration": {
    // Updated configuration data
  },
  "metrics": {
    "uptime_seconds": 86400,
    "memory_mb": 512,
    "cpu_percent": 25.5,
    "active_connections": 10
  }
}

Response (ServiceReportResponse):
{
  "success": true,
  "message": "Health report received for service bytefreezer-proxy"
}

Note: Naming flexibility - accepts both "service_name"/"service_id" 
      and "service_type"/"instance_id" field names for compatibility
```

#### Get Health Status
```
GET /api/v1/health/status

Response (HealthStatusResponse):
{
  "services": [
    {
      "id": 1,
      "service_type": "bytefreezer-proxy",
      "instance_id": "hostname-1",
      "instance_api": "hostname-1:8080",
      "status": "Healthy",
      "configuration": { /* see below */ },
      "metrics": { /* runtime metrics */ },
      "response_time_ms": 45,
      "last_seen": "2024-10-21T12:30:00Z",
      "created_at": "2024-10-21T10:00:00Z",
      "updated_at": "2024-10-21T12:30:00Z"
    }
  ],
  "summary": {
    "total_services": 5,
    "healthy_services": 4,
    "unhealthy_services": 1,
    "healthy_percentage": 80.0
  }
}
```

#### Get Health Summary (Dashboard)
```
GET /api/v1/health/summary

Response (HealthSummaryResponse):
{
  "summary": {
    "total_services": 5,
    "healthy_services": 4,
    "unhealthy_services": 1,
    "healthy_percentage": 80.0,
    "services_by_type": {
      "bytefreezer-proxy": {
        "total": 3,
        "healthy": 2,
        "unhealthy": 1
      },
      "bytefreezer-receiver": {
        "total": 2,
        "healthy": 2,
        "unhealthy": 0
      }
    }
  }
}
```

#### Get Service Type Health
```
GET /api/v1/health/services/{serviceType}

Path Parameters:
  - serviceType: Service type to query (e.g., "bytefreezer-proxy")

Response: Same as HealthStatusResponse, but filtered for the service type
```

---

### 4. Plugin Schema Discovery Endpoint

#### Get Plugin Schemas
```
GET /api/v1/plugins

Response (PluginSchemasResponse):
{
  "plugins": [
    {
      "name": "http-listener",
      "display_name": "HTTP Listener",
      "description": "Receive data via HTTP POST",
      "category": "input",
      "transport": "http",
      "default_port": 8080,
      "fields": [
        {
          "name": "bind_address",
          "type": "string",
          "required": true,
          "description": "IP address to bind to",
          "default": "0.0.0.0",
          "placeholder": "0.0.0.0 or specific IP"
        },
        {
          "name": "port",
          "type": "integer",
          "required": true,
          "description": "Port to listen on",
          "default": 8080,
          "validation": "^(1|[1-9][0-9]{1,3}|[1-5][0-9]{4}|6[0-4][0-9]{3}|65[0-4][0-9]{2}|655[0-2][0-9]|6553[0-5])$"
        },
        {
          "name": "format",
          "type": "string",
          "required": false,
          "description": "Data format",
          "options": ["json", "ndjson", "csv"],
          "default": "json"
        }
      ]
    },
    {
      "name": "udp-listener",
      "display_name": "UDP Listener",
      "description": "Receive data via UDP",
      "category": "input",
      "transport": "udp",
      "default_port": 514,
      "fields": [ /* ... */ ]
    }
  ],
  "count": 12
}

Note: Schemas are extracted from the "plugin_schemas" field
      in proxy configuration health reports
```

---

## Instance Identification

### Instance ID Format
- **Hostname-based**: Uses result of `os.Hostname()` on the service machine
- **Format**: Must be unique within a service type
- **Examples**: `proxy-prod-1`, `proxy-dev-2`, `control-1`

### Instance API Format
- **No protocol**: `{hostname}:{port}` (not `http://hostname:port`)
- **Port**: From service configuration (typically 8080 for API)
- **Used for**: Health polling and direct communication
- **Example**: `prod-proxy-01:8080`

### Multi-Instance Scenarios
- Each proxy instance registers separately with its own hostname/instance_id
- Control maintains separate health records per instance
- Plugin schemas are deduplicated across instances (by plugin name)
- Health polling targets each instance's `instance_api` for `/api/v1/health`

---

## Configuration Data Structure

### Proxy Health Configuration (What Proxies Send)

When proxies register and report health, they send comprehensive configuration:

```json
{
  "service_type": "bytefreezer-proxy",
  "version": "1.0.0",
  "git_commit": "abc123def456",
  "instance_api": "hostname:8080",
  "report_interval": 300,
  "timeout": "30s",
  "tenant_id": "acme-prod",
  "bearer_token": "ab****ef",  // Masked sensitive data
  
  "api": {
    "port": 8080
  },
  
  "udp": {
    "enabled": true,
    "read_buffer_size_bytes": 65536,
    "max_batch_lines": 1000,
    "max_batch_bytes": 1048576,
    "batch_timeout_seconds": 5,
    "compression_level": 6,
    "channel_buffer_size": 10000,
    "worker_count": 4,
    "listener_count": 2
  },
  
  "batching": {
    "enabled": true,
    "max_lines": 1000,
    "max_bytes": 1048576,
    "timeout_seconds": 5,
    "compression_enabled": true,
    "compression_level": 6
  },
  
  "receiver": {
    "base_url": "http://receiver:8080",
    "timeout_seconds": 30,
    "upload_worker_count": 5,
    "max_idle_conns": 10,
    "max_conns_per_host": 6
  },
  
  "spooling": {
    "enabled": true,
    "directory": "/var/spool/bytefreezer-proxy",
    "max_size_bytes": 10737418240,
    "retry_attempts": 3,
    "retry_interval_seconds": 60,
    "cleanup_interval_seconds": 300,
    "keep_src": false,
    "queue_processing_interval_seconds": 10,
    "organization": "acme",
    "per_tenant_limits": true,
    "max_files_per_dataset": 1000,
    "max_age_days": 7
  },
  
  "housekeeping": {
    "enabled": true,
    "interval_seconds": 3600
  },
  
  "otel": {
    "enabled": true,
    "endpoint": "http://otel-collector:4318",
    "service_name": "bytefreezer-proxy",
    "prometheus_mode": false,
    "metrics_port": 9090
  },
  
  "soc": {
    "enabled": false,
    "endpoint": "http://soc:9000",
    "timeout": 5
  },
  
  "multi_tenant": true,
  
  "capabilities": [
    "udp_ingestion",
    "multi_protocol",
    "batching",
    "spooling",
    "http_forwarding",
    "plugin_system"
  ],
  
  "plugin_schemas": [
    // See plugin schema structure below
  ]
}
```

### Plugin Schema Structure

```json
{
  "name": "http-listener",
  "display_name": "HTTP Listener",
  "description": "Receive data via HTTP POST requests",
  "category": "input",
  "transport": "http",
  "default_port": 8080,
  "fields": [
    {
      "name": "bind_address",
      "type": "string",
      "required": true,
      "description": "IP address to bind to",
      "default": "0.0.0.0",
      "placeholder": "0.0.0.0 or specific IP",
      "group": "network"
    },
    {
      "name": "port",
      "type": "integer",
      "required": true,
      "description": "Port to listen on",
      "default": 8080,
      "validation": "port_range_regex",
      "group": "network"
    },
    {
      "name": "format",
      "type": "string",
      "required": false,
      "description": "Data format to expect",
      "default": "json",
      "options": ["json", "ndjson", "csv", "xml"],
      "group": "data"
    },
    {
      "name": "max_request_size_mb",
      "type": "integer",
      "required": false,
      "description": "Maximum request body size",
      "default": 10,
      "validation": "^[1-9][0-9]*$"
    }
  ]
}
```

---

## Authentication & Authorization

### Authentication Mechanisms

1. **Bearer Token** (Proxy to Control)
   - Format: `Authorization: Bearer {token}`
   - Used by: Proxies when fetching tenant config
   - Configured in: `proxy.config.yaml` → `bearer_token`
   - Applied to: Tenant lookup and configuration endpoints

2. **JWT Tokens** (Control API)
   - Format: `Authorization: Bearer {jwt_token}`
   - Used for: UI and user-facing API endpoints
   - Generated by: Login endpoint (`POST /api/v1/login`)
   - Validated by: ConditionalAuthMiddleware

### Public Endpoints (No Auth Required)
- `GET /api/v1/health`
- `POST /api/v1/login`
- `POST /api/v1/refresh`
- `POST /api/v1/password-reset`
- `GET /api/v1/tenants/{tenantId}` (direct lookup)

### Proxy Access Pattern
```
1. Proxy starts with configured bearer_token
2. Proxy fetches tenant: GET /api/v1/tenants/{tenantId}
   Header: Authorization: Bearer {bearer_token}
3. Proxy registers: POST /api/v1/health/register
4. Proxy reports health: POST /api/v1/services/report
5. Control polls proxy: GET http://{instance_api}/api/v1/health
```

---

## Health Monitoring Flow

### Detailed Workflow

#### 1. Proxy Startup (Initial Registration)
```
Proxy (main.go):
  1. Load configuration from config.yaml
  2. Determine instance_id from hostname
  3. Build health configuration (buildHealthConfiguration)
  4. Create HealthReportingService
  5. Call RegisterService()
     POST /api/v1/health/register
     {
       "service_type": "bytefreezer-proxy",
       "instance_id": "prod-proxy-01",
       "instance_api": "prod-proxy-01:8080",
       "status": "Starting",
       "configuration": { /* full config */ }
     }

Control (api/health_handlers.go):
  1. Receive registration request
  2. Store in database via upsert_health_current()
  3. Set initial status to "Starting"
  4. Return success response
```

#### 2. Periodic Health Reporting
```
Proxy (services/health_reporting.go):
  1. Start ticker at reportInterval (default 5 minutes)
  2. Loop every interval:
     POST /api/v1/services/report
     {
       "service_name": "bytefreezer-proxy",
       "service_id": "prod-proxy-01",
       "instance_api": "prod-proxy-01:8080",
       "healthy": true/false,
       "configuration": { /* current config */ },
       "metrics": { /* runtime metrics */ }
     }

Control (api/handlers.go::ReceiveServiceReport):
  1. Receive health report
  2. Update health record via UpdateServiceHealth
  3. Store configuration (updated each report)
  4. Store metrics
  5. Mark stale services as unhealthy
```

#### 3. Control Polling (Active Monitoring)
```
Control (main.go::startHealthPolling):
  1. Every 10 minutes:
     FOR EACH service in health_current:
       GET http://{instance_api}/api/v1/health

Control (services/health.go::pollSingleService):
  1. Skip services in "Starting" status
  2. Make HTTP request to service health endpoint
  3. Record response time
  4. Update health status to "Healthy" or "Unhealthy"
  5. Store metrics from response if available

Services:
  GET /api/v1/health
  Response:
  {
    "status": "ok",
    "service": "bytefreezer-proxy",
    "version": "1.0.0",
    "timestamp": "2024-10-21T12:30:00Z",
    "uptime": "2h30m"
  }
```

#### 4. Plugin Schema Discovery
```
Control (api/handlers.go::GetPluginSchemas):
  1. Query health_current for all bytefreezer-proxy instances
  2. Extract "plugin_schemas" from configuration JSON
  3. Deduplicate schemas by plugin name (multiple proxies)
  4. Return merged list to client
  
Client (UI/Admin):
  GET /api/v1/plugins
  Response:
  {
    "plugins": [ /* deduplicated schemas */ ],
    "count": 12
  }
```

---

## Request/Response Examples

### Example 1: Proxy Fetching Tenant Configuration
```
Request:
GET /api/v1/tenants/acme-prod HTTP/1.1
Host: control:8080
Authorization: Bearer proxy_token_12345

Response:
HTTP/1.1 200 OK
Content-Type: application/json

{
  "id": "acme-prod",
  "account_id": "acc-f1k2j3",
  "name": "ACME Production",
  "display_name": "ACME Production Tenant",
  "description": "Production tenant for ACME Corp",
  "active": true,
  "config": {
    "metadata": {
      "environment": "production",
      "region": "us-west-2",
      "max_datasets": 100
    },
    "custom_settings": {
      "soc_enabled": true,
      "sampling_rate": 0.1
    }
  },
  "created_at": "2024-09-15T08:00:00Z",
  "updated_at": "2024-10-20T14:30:00Z"
}
```

### Example 2: Proxy Registering with Control
```
Request:
POST /api/v1/health/register HTTP/1.1
Host: control:8080
Content-Type: application/json

{
  "service_type": "bytefreezer-proxy",
  "instance_id": "prod-proxy-01",
  "instance_api": "prod-proxy-01:8080",
  "status": "Starting",
  "configuration": {
    "service_type": "bytefreezer-proxy",
    "version": "1.0.0",
    "tenant_id": "acme-prod",
    "multi_tenant": false,
    "capabilities": ["udp_ingestion", "batching", "spooling"],
    "plugin_schemas": [
      {
        "name": "udp-listener",
        "display_name": "UDP Listener",
        "category": "input",
        "transport": "udp",
        "fields": [...]
      }
    ]
  }
}

Response:
HTTP/1.1 200 OK
Content-Type: application/json

{
  "success": true,
  "message": "Service bytefreezer-proxy registered successfully",
  "instance_id": "prod-proxy-01",
  "service_type": "bytefreezer-proxy"
}
```

### Example 3: Fetching Plugin Schemas
```
Request:
GET /api/v1/plugins HTTP/1.1
Host: control:8080

Response:
HTTP/1.1 200 OK
Content-Type: application/json

{
  "plugins": [
    {
      "name": "http-listener",
      "display_name": "HTTP Listener",
      "description": "Receive data via HTTP POST",
      "category": "input",
      "transport": "http",
      "default_port": 8080,
      "fields": [
        {
          "name": "bind_address",
          "type": "string",
          "required": true,
          "description": "IP address to bind to",
          "default": "0.0.0.0",
          "placeholder": "0.0.0.0"
        },
        {
          "name": "port",
          "type": "integer",
          "required": true,
          "default": 8080
        }
      ]
    }
  ],
  "count": 1
}
```

---

## Configuration Query Patterns

### Pattern 1: Proxy Startup Configuration
```
1. Proxy reads config.yaml
2. If health_reporting.enabled and control_url set:
   - GET /api/v1/tenants/{tenant_id}  // Validate tenant
   - POST /api/v1/health/register      // Register instance
3. Start listening for data with configured parameters
```

### Pattern 2: UI Configuration Discovery
```
1. User logs in to UI
2. UI calls GET /api/v1/plugins
   - Control queries all proxy health records
   - Extracts plugin_schemas from configuration
   - Deduplicates and returns
3. UI displays available plugins and their configuration schema
4. User can configure plugin instances based on schema
```

### Pattern 3: Health Dashboard
```
1. Dashboard loads GET /api/v1/health/summary
   - Shows total/healthy/unhealthy service counts
2. Drill-down: GET /api/v1/health/services/{serviceType}
   - Shows instances of specific type
   - Includes response times and last seen
3. Detailed: GET /api/v1/health/status
   - Full configuration and metrics for all services
```

---

## Database Schema Integration

### Health Records Table (health_current)
```sql
CREATE TABLE health_current (
  id SERIAL PRIMARY KEY,
  service_type VARCHAR(255),           -- e.g., 'bytefreezer-proxy'
  instance_id VARCHAR(255),             -- e.g., hostname
  instance_api VARCHAR(255),            -- e.g., 'hostname:8080'
  status VARCHAR(50),                   -- 'Healthy', 'Unhealthy', 'Starting'
  configuration JSONB,                  -- Full config including plugin_schemas
  metrics JSONB,                        -- Runtime metrics
  response_time_ms INTEGER,             -- Poll response time
  last_seen TIMESTAMP,
  created_at TIMESTAMP,
  updated_at TIMESTAMP,
  UNIQUE(service_type, instance_id)
);
```

### Upsert Function (upsert_health_current)
- Used for both registration and health updates
- Creates new record if not exists (by service_type + instance_id)
- Updates existing record with latest configuration and metrics
- Tracks last_seen timestamp for stale detection

---

## Key Implementation Details

### Configuration Storage
- **In Memory**: Proxy configuration loaded from file at startup
- **In Database**: Stored in health_current.configuration JSONB for discovery
- **On Network**: Transmitted in registration and health reports
- **Sensitive Data**: Masked with `"ab****ef"` pattern (first 2 + last 2 chars)

### Instance Discovery
- No central registry beyond database health records
- Instances self-identify via hostname
- Control maintains "current" and "history" tables for tracking changes
- Stale detection: Services with no heartbeat >60 seconds marked unhealthy

### Plugin Schema Discovery
- Plugins defined at compile time in proxy codebase
- Each plugin registers schema with GlobalRegistry
- Proxy builds schema array during startup (getPluginSchemas)
- Included in health configuration
- Control aggregates and deduplicates from all proxy instances

### Configuration Refresh
- Proxies don't poll control for configuration changes
- Configuration is read-only from file at startup
- To update: Modify config file → Restart proxy
- Control can serve updated config via API for UI display

---

## Summary Table

| Aspect | Details |
|--------|---------|
| **Identification** | Hostname-based instance IDs, instance_api format: `{host}:{port}` |
| **Configuration Format** | Comprehensive map[string]interface{} with structured sections |
| **Plugin Discovery** | Dynamic via plugin_schemas in health reports, deduplicated by control |
| **Registration** | POST /api/v1/health/register with full configuration |
| **Health Reporting** | POST /api/v1/services/report every N seconds (default 5 min) |
| **Polling** | Control polls each instance GET /api/v1/health every 10 minutes |
| **Tenant Lookup** | GET /api/v1/tenants/{tenantId} (public, no auth) |
| **Direct Tenant Access** | Proxies use direct endpoint (no account_id needed) |
| **Auth Method** | Bearer token for proxies, JWT for users |
| **Schema Format** | name, display_name, description, category, transport, default_port, fields[] |
| **Field Metadata** | type, required, description, default, placeholder, validation, options, group |


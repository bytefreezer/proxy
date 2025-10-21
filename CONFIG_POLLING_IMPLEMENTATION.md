# ByteFreezer Proxy Configuration Polling Implementation

## Overview

This document describes the comprehensive configuration polling system implemented for ByteFreezer Proxy, allowing proxies to fetch configuration from bytefreezer-control, cache it locally, and dynamically reload plugins.

## Architecture

### Configuration Modes

The proxy supports three configuration modes (`config_mode`):

1. **local-only**: Uses only local config.yaml, ignores control
2. **control-only**: Fetches all configuration from control, fails if control unavailable
3. **hybrid** (default): Uses control as primary source, falls back to local cached config if control unreachable

### Components

```
┌─────────────────────────────────────────────────────────────┐
│                    ByteFreezer Proxy                         │
│                                                               │
│  ┌──────────────┐  polls  ┌────────────────────────┐        │
│  │ ConfigPolling├─────────►│ ByteFreezer Control   │        │
│  │ Service      │◄─────────┤ /api/v2/proxies/.../  │        │
│  └──────┬───────┘  config  │ config                 │        │
│         │                  └────────────────────────┘        │
│         │                                                     │
│         │ on change                                           │
│         ▼                                                     │
│  ┌──────────────┐                                            │
│  │ Plugin       │  reload plugins                             │
│  │ Service      ├──────────────┐                             │
│  └──────────────┘              │                             │
│         │                      ▼                             │
│         │            ┌──────────────────┐                    │
│         │            │ Port Conflict    │                    │
│         │            │ Detection        │                    │
│         │            └──────────────────┘                    │
│         │                                                     │
│         ▼                                                     │
│  ┌──────────────┐     cache    ┌─────────────────┐          │
│  │ Local Cache  │◄──────────────┤ /var/cache/...  │          │
│  │ Manager      │───────────────►│ {tenant}-{host}│          │
│  └──────────────┘     load       │ .json           │          │
│                                  └─────────────────┘          │
└───────────────────────────────────────────────────────────────┘
```

## Configuration

### Proxy config.yaml

```yaml
# Configuration mode
config_mode: "hybrid"  # local-only | control-only | hybrid

# Control service URL
control_url: "http://control:8080"

# Config polling settings
config_polling:
  enabled: true
  interval_seconds: 300      # Poll every 5 minutes
  timeout_seconds: 30        # HTTP timeout
  cache_directory: "/var/cache/bytefreezer-proxy"
  retry_on_error: true      # Use cached config if control unavailable

# Tenant and authentication
tenant_id: "my-tenant"
bearer_token: "your-bearer-token"
```

## Control-Side Implementation

### Database Schema

**Migration**: `migrations/004_proxy_configuration.sql`

**Tables**:
- `proxy_instances`: Current proxy configurations
- `proxy_config_history`: Historical configuration changes (audit trail)

**Key Fields**:
```sql
proxy_instances (
    instance_id VARCHAR(255),     -- hostname
    tenant_id VARCHAR(255),        -- associated tenant
    instance_api VARCHAR(255),     -- hostname:port
    config_mode VARCHAR(50),       -- local-only, control-only, hybrid
    plugin_configs JSONB,          -- Array of plugin configurations
    proxy_settings JSONB,          -- Proxy-level settings
    config_version INTEGER,        -- Increments on each update
    config_applied BOOLEAN,        -- Whether proxy has applied this config
    config_hash VARCHAR(64),       -- SHA256 hash for change detection
    ...
)
```

### API Endpoints

**Base URL**: `http://control:8080/api/v2/proxies`

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/v2/proxies` | GET | List all proxy instances |
| `/api/v2/proxies/{instanceId}/config?tenant_id={tid}` | GET | Get proxy configuration |
| `/api/v2/proxies/{instanceId}/config` | PUT | Create/update proxy config |
| `/api/v2/proxies/{instanceId}/config/applied` | POST | Mark config as applied |
| `/api/v2/proxies/{instanceId}/config/history?tenant_id={tid}` | GET | Get config history |
| `/api/v2/proxies/{instanceId}?tenant_id={tid}` | DELETE | Delete proxy instance |

### Example: Create/Update Proxy Configuration

```bash
curl -X PUT http://control:8080/api/v2/proxies/prod-proxy-01/config \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
  "tenant_id": "my-tenant",
  "instance_api": "prod-proxy-01:8080",
  "config_mode": "hybrid",
  "plugin_configs": [
    {
      "type": "sflow",
      "name": "sflow-listener",
      "config": {
        "host": "0.0.0.0",
        "port": 2055,
        "dataset_id": "sflow-data",
        "protocol": "sflow",
        "data_hint": "ndjson"
      }
    }
  ],
  "proxy_settings": {
    "receiver": {
      "base_url": "http://receiver:8081",
      "timeout_seconds": 30,
      "upload_worker_count": 5
    },
    "batching": {
      "enabled": true,
      "max_lines": 10000,
      "max_bytes": 1048576,
      "timeout_seconds": 30,
      "compression_enabled": true,
      "compression_level": 6
    }
  }
}'
```

## Proxy-Side Implementation

### Service: ConfigPollingService

**File**: `services/config_polling.go`

**Responsibilities**:
1. Poll control for configuration updates
2. Cache configuration locally to disk
3. Load cached configuration on startup
4. Detect configuration changes (via hash comparison)
5. Apply new configuration (trigger plugin reload)
6. Detect and log port conflicts
7. Report config application status to control

**Flow**:

1. **Startup**:
   - Try to load cached config from `/var/cache/bytefreezer-proxy/{tenant}-{hostname}.json`
   - If `config_mode` is not `local-only`, perform initial poll

2. **Polling Loop** (every 5 minutes):
   ```
   Poll Control → Compare Hash → Changed? → Apply Config → Cache → Report Applied
                                     ↓ No
                                   Skip
   ```

3. **Config Change Detected**:
   - Calculate SHA256 hash of new config
   - Compare with current hash
   - If different:
     - Call `applyConfiguration()`
     - Detect port conflicts
     - Call `onConfigChange` callback (triggers plugin reload)
     - Cache new config to disk
     - Report to control that config was applied

4. **Port Conflict Detection**:
   - Compares local config ports with remote config ports
   - Logs warnings: "Port {N}: local '{plugin}' vs remote '{plugin}' - REMOTE TAKES PRECEDENCE"
   - Remote configuration always wins

### Service: Plugin Reload

**File**: `services/plugin_service.go`

**Method**: `Reload(newInputConfigs []plugins.PluginConfig)`

**Process**:
1. Stop current plugin manager (stops all running plugins)
2. Create new plugin manager with updated configs
3. Start new plugins

**Example**:
```go
// Called when config changes
func onConfigChange(remoteConfig *ControlProxyConfig) error {
    // Convert remote config to plugin configs
    newConfigs := convertToPluginConfigs(remoteConfig.PluginConfigs)

    // Reload plugin service
    return pluginService.Reload(newConfigs)
}
```

### Local Cache Format

**Location**: `/var/cache/bytefreezer-proxy/{tenant_id}-{instance_id}.json`

**Example**:
```json
{
  "instance_id": "prod-proxy-01",
  "tenant_id": "my-tenant",
  "instance_api": "prod-proxy-01:8080",
  "config_mode": "hybrid",
  "config_version": 3,
  "config_hash": "a1b2c3d4...",
  "plugin_configs": [...],
  "proxy_settings": {...},
  "updated_at": "2025-10-21T10:30:00Z"
}
```

## Configuration Lifecycle

### Scenario 1: First-Time Proxy Startup

```
1. Proxy starts with local config.yaml
2. ConfigPollingService initializes
3. No cached config found → logs warning
4. config_mode = "hybrid" → performs initial poll
5. Control returns 404 (not configured yet)
6. Proxy continues with local config.yaml
7. Polling continues every 5 minutes
```

### Scenario 2: Config Updated in Control

```
1. Admin updates proxy config in control UI
2. Control increments config_version (e.g., v2 → v3)
3. Control updates config_hash
4. Proxy polls control (every 5 minutes)
5. Proxy receives new config with hash "xyz..."
6. Hash comparison: current != remote → changed
7. Proxy applies new configuration:
   - Stops current plugins
   - Starts new plugins with new config
8. Proxy caches new config to disk
9. Proxy reports to control: "config v3 applied"
10. Control updates config_applied = true, config_applied_at = NOW()
```

### Scenario 3: Control Unreachable (Hybrid Mode)

```
1. Proxy polls control
2. HTTP request fails (timeout/connection refused)
3. config_mode = "hybrid" + retry_on_error = true
4. Proxy loads cached config from disk
5. Proxy continues operating with cached config
6. Logs: "Control unreachable, using cached config (v2)"
7. Next poll attempt in 5 minutes
```

### Scenario 4: Port Conflict Detection

```
Local config.yaml:
  - port 2055: sflow-listener

Remote config from control:
  - port 2055: ipfix-listener

Proxy detects conflict:
  - Logs: "Port conflict: Port 2055: local 'sflow[sflow-listener]' vs remote 'ipfix-listener' - REMOTE TAKES PRECEDENCE"
  - Stops sflow-listener
  - Starts ipfix-listener on port 2055
```

## Integration in main.go

**Setup** (pseudo-code):

```go
// 1. Load local config
cfg := config.LoadConfig("config.yaml")

// 2. Create services
svcs := services.NewServices(cfg)

// 3. Start spooling service
svcs.SpoolingService.Start()

// 4. Create plugin service
pluginService := services.NewPluginService(cfg, forwarder, spoolingService)

// 5. Create config polling service with callback
configPollingService := services.NewConfigPollingService(cfg, func(remoteConfig *ControlProxyConfig) error {
    // Convert remote config to plugin configs
    newConfigs := convertRemoteConfigToPluginConfigs(remoteConfig)

    // Reload plugins
    return pluginService.Reload(newConfigs)
})

// 6. Add to services
svcs.PluginService = pluginService
svcs.ConfigPollingService = configPollingService

// 7. Start services
pluginService.Start()
configPollingService.Start()  // Starts polling loop

// 8. Graceful shutdown
signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
<-sigChan
configPollingService.Stop()
pluginService.Stop()
```

## Monitoring & Debugging

### Logs

**Startup**:
```
Config polling service starting (mode: hybrid, interval: 5m0s)
Loaded cached configuration (version 2) from /var/cache/bytefreezer-proxy/my-tenant-prod-proxy-01.json
Config polling enabled - polling every 5m0s
```

**Config Change**:
```
Configuration changed (version 2 -> 3)
Applying new configuration (version 3)
Port conflict: Port 2055: local 'sflow[sflow-listener]' vs remote 'ipfix-listener' - REMOTE TAKES PRECEDENCE
Reloading plugin service with new configuration
Plugin service reloaded successfully with 3 plugins
Configuration cached to /var/cache/bytefreezer-proxy/my-tenant-prod-proxy-01.json
Configuration applied successfully (version 3)
Reported config version 3 as applied to control
```

**Control Unreachable (Hybrid Mode)**:
```
Config poll failed: failed to poll control: dial tcp: connection refused
Control unreachable, using cached config
Loaded cached configuration (version 2) from /var/cache/bytefreezer-proxy/my-tenant-prod-proxy-01.json
```

### API Endpoints

**Get Current Config** (Proxy API):
```bash
curl http://localhost:8080/api/v1/config
```

Returns current running configuration including plugin details.

**Get Control Config** (Control API):
```bash
curl "http://control:8080/api/v2/proxies/prod-proxy-01/config?tenant_id=my-tenant" \
  -H "Authorization: Bearer TOKEN"
```

Returns proxy configuration stored in control.

## Files Changed

### Control (bytefreezer-control/)

| File | Description |
|------|-------------|
| `migrations/004_proxy_configuration.sql` | Database schema for proxy instances |
| `storage/config_types.go` | ProxyInstanceConfig and related types |
| `storage/interface.go` | Storage interface methods for proxy config |
| `storage/postgresql_proxy_config.go` | PostgreSQL implementation of proxy config storage |
| `api/api.go` | API route definitions for proxy config endpoints |
| `api/proxy_config_handlers.go` | HTTP handlers for proxy config operations |

### Proxy (bytefreezer-proxy/)

| File | Description |
|------|-------------|
| `config/config.go` | ConfigMode and ConfigPollingConfig types, defaults |
| `services/config_polling.go` | ConfigPollingService implementation |
| `services/plugin_service.go` | Added Reload() method for dynamic plugin reload |
| `services/services.go` | Added PluginService and ConfigPollingService fields |

## Testing Guide

### 1. Test Local-Only Mode

```yaml
# config.yaml
config_mode: "local-only"
config_polling:
  enabled: false

inputs:
  - type: "sflow"
    name: "sflow-listener"
    config:
      port: 2055
      dataset_id: "sflow-data"
```

**Expected**: Proxy uses only local config, never polls control.

### 2. Test Control-Only Mode

```yaml
# config.yaml
config_mode: "control-only"
control_url: "http://control:8080"
tenant_id: "test-tenant"
bearer_token: "test-token"

config_polling:
  enabled: true
  interval_seconds: 60
```

**Steps**:
1. Start proxy (will fail if control not configured)
2. Use control API to create proxy config
3. Proxy should pick up config on next poll
4. Verify plugins start with control config

### 3. Test Hybrid Mode with Failover

```yaml
# config.yaml
config_mode: "hybrid"
control_url: "http://control:8080"
config_polling:
  retry_on_error: true

inputs:
  - type: "sflow"
    name: "local-fallback"
    config:
      port: 2055
```

**Steps**:
1. Start proxy with control running
2. Create config in control with different plugin
3. Verify proxy switches to control config
4. Stop control
5. Verify proxy falls back to cached config
6. Restart proxy with control still down
7. Verify proxy loads from cache
8. Start control
9. Update config in control
10. Verify proxy picks up new config

### 4. Test Port Conflicts

**Local config**:
```yaml
inputs:
  - type: "sflow"
    port: 2055
```

**Remote config** (via control API):
```json
{
  "plugin_configs": [{
    "type": "ipfix",
    "config": {"port": 2055}
  }]
}
```

**Expected**: Proxy logs port conflict warning and uses remote config (ipfix on 2055).

### 5. Test Config Change Detection

1. Create initial config in control (version 1)
2. Proxy polls and applies
3. Update config in control (version 2)
4. Proxy polls, detects change via hash
5. Proxy reloads plugins
6. Proxy reports "applied" to control
7. Verify control shows `config_applied = true`

## Summary

This implementation provides a robust configuration management system that:

✅ Supports three operating modes (local-only, control-only, hybrid)
✅ Polls control for configuration updates
✅ Caches configuration locally for failover
✅ Detects configuration changes via SHA256 hashing
✅ Dynamically reloads plugins without restart
✅ Detects and resolves port conflicts (remote wins)
✅ Reports configuration application status to control
✅ Maintains configuration history (audit trail)
✅ Provides comprehensive logging and debugging

The system is designed for production use with:
- Graceful degradation (falls back to cached config)
- Zero data loss (plugins reload without dropping data)
- Full audit trail (config history in database)
- Port conflict resolution (prevents startup failures)
- Thread-safe operations (mutex-protected config access)

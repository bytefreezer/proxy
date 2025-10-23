# ByteFreezer Proxy Release Notes

## v4.2.0 - Configuration Cleanup (2025-10-23)

### Critical Bug Fixes
- **FIXED: Config polling service was never started** (main.go)
  - **Impact**: Proxies in control-only mode were not fetching plugin configurations from Control API
  - **Symptom**: Datasets configured in Control were not creating corresponding input plugins on proxy
  - **Root Cause**: ConfigPollingService code existed but was never initialized or started in main.go
  - **Fix**: Added config polling service initialization with dynamic plugin reload callback
  - **Result**: Proxies now automatically fetch and load plugins from Control based on account/tenant datasets
  - Added `onConfigChange` callback that converts dataset configs to plugins and triggers reload
  - Added graceful shutdown for config polling service
  - Proxy now starts even with zero local plugins - waits for remote config from Control
  - **Testing**: Verify with `curl http://<control>/api/v2/proxy/config?account_id=<your_account_id>` and proxy logs

- **FIXED: Plugin service validation prevented zero-input startup** (plugin_service.go:44-46)
  - **Impact**: Proxy crashed at startup when no local plugins were configured in config.yaml
  - **Symptom**: Error "no input plugins configured" even when using control-only mode
  - **Root Cause**: NewPluginService() validation required at least one local input plugin
  - **Fix**: Removed validation check - plugin service can now start with zero plugins
  - **Result**: Proxy starts successfully with empty local config and waits for remote plugins from Control

### Breaking Changes
- **Removed hybrid mode**: Configuration mode now supports only `local-only` or `control-only`
  - `control-only` mode provides automatic fallback to tenant-based polling when account_id is not configured
  - Removed `hybrid` as a valid config_mode value
  - Default config_mode changed from `hybrid` to `control-only`

### Configuration Improvements
- **Made tenant_id optional for account-based polling**: tenant_id is now only required when account_id is not configured
  - When using `account_id`: tenant_id is optional (each dataset has its own tenant_id)
  - When NOT using `account_id`: tenant_id is required as global fallback for plugins
  - Updated validation logic to check account_id before requiring tenant_id
  - Clearer error messages guide users on when tenant_id is needed
- **Removed hardcoded plugins**: Example config file no longer includes hardcoded plugin configurations
  - Plugins should be configured via Control API or local config as needed
- **Clarified TenantID usage**: Updated comments to reflect that `tenant_id` is the global fallback for plugins, not a deprecated field
- **Simplified config modes**: Only two modes supported:
  - `local-only`: Use only local config.yaml (air-gapped deployments)
  - `control-only`: Fetch config from Control with automatic fallback on error

### API Improvements
- **Enhanced /api/v1/config endpoint**: Added new fields to provide comprehensive configuration visibility
  - Added `account_id`: Displays the configured account ID for multi-tenant polling
  - Added `control_url`: Shows the Control service URL
  - Added `config_mode`: Shows current configuration mode (local-only | control-only)
  - Added `config_polling`: Shows configuration polling settings (enabled, intervals, cache directory, retry behavior)
  - Better visibility into whether proxy is using account-based or tenant-based polling

- **FIXED: Plugins section now shows RUNNING plugins** (/api/v1/config endpoint)
  - **Impact**: Config endpoint previously showed only local config.yaml plugins (always empty in control-only mode)
  - **Symptom**: `"total_plugins": 0` even when plugins were running and processing data
  - **Root Cause**: GetConfig handler used cfg.Inputs instead of querying PluginService for active plugins
  - **Fix**: Added GetConfigs() method to plugin manager and service, updated API handler to use running plugins
  - **Result**: Config endpoint now shows all active plugins with their full configurations (tenant_id, dataset_id, port, etc.)
  - Sensitive fields (bearer_token, password, secret, token) are masked as "***MASKED***"

### Code Cleanup
- **Removed legacy UDP configuration system**:
  - Removed `UDP` struct and `UDPListener` struct from config.go (lines 62-83)
  - Removed `validateMultiTenantConfig()`, `GetTenantInfo()`, `IsMultiTenant()` functions
  - Removed `GetEffectiveBearerToken()` and `GetEffectiveTenantID()` helper functions
  - Removed UDP-specific validation from `validateIdentifiers()`
  - Removed UDP default values from `LoadConfig()`
  - Removed UDP section from health report configuration in main.go
  - Removed unused log import from config.go
  - All UDP functionality now provided through UDP plugin system
- Removed all "legacy" and "hybrid" terminology from config polling service
- **Consolidated Ansible configuration**:
  - Merged AWX_EXTRA_VARS_EXAMPLE.yml into group_vars/all.yml
  - Removed AWX_EXTRA_VARS_EXAMPLE.yml file (no longer needed)
  - Removed hardcoded plugins from all.yml (now uses empty inputs: [])
  - Updated config.yaml.j2 template to exactly match config.yaml structure
  - Removed conditional logic and defaults from template (now uses direct values from all.yml)
  - Removed health_reporting.control_url and tenant_validation.control_url (use global control_url)
  - Updated version references from v4.1.0 to v4.2.0
  - Clarified tenant_id as "global fallback" instead of "legacy mode"
  - Simplified configuration comments for better clarity
- **Updated example configurations**:
  - Updated config-with-control.yaml to remove hybrid mode reference
- Improved documentation clarity for configuration modes

### Migration Guide
If you were using `config_mode: "hybrid"`:
- Change to `config_mode: "control-only"` - this provides the same fallback behavior
- Ensure `retry_on_error: true` is set in `config_polling` section for automatic retry

---

## v4.1.0 - Account-Based Configuration Polling (2025-10-23)

### Multi-Tenant Configuration Management

#### 🏢 Account-Based Configuration Mode (NEW)
- **Account-Level Polling**: Proxy polls Control for all tenants + datasets in an account
- **Dynamic Plugin Generation**: Automatically converts dataset configurations to plugin configs
- **Multi-Tenant Support**: Single proxy instance can serve multiple tenants from one account
- **Backwards Compatible**: Legacy single-tenant mode still supported via `tenant_id`

#### 🔄 Configuration Flow

**New Account Mode**:
1. Proxy starts with `account_id`, `bearer_token`, `control_url` in config
2. Polls Control: `GET /api/v2/proxy/config?account_id={accountID}`
3. Receives all tenants + datasets for the account
4. Converts datasets to plugin configurations dynamically
5. Applies plugin configs and starts listeners
6. Reports applied configuration back to Control for each tenant

**Dataset-to-Plugin Conversion**:
- Dataset `source.type` is used as the plugin type (e.g., "ebpf", "sflow", "netflow")
- Dataset `source.custom` contains plugin-specific parameters
- Plugin config includes: `tenant_id`, `dataset_id`, plus all fields from `source.custom`
- Example dataset configuration:
  ```json
  {
    "source": {
      "type": "ebpf",
      "custom": {
        "host": "0.0.0.0",
        "port": 2056,
        "worker_count": 4,
        "read_buffer_size": 65536
      }
    }
  }
  ```

#### 📋 Configuration Modes

**New Account Mode**:
```yaml
account_id: "your-account-id"
bearer_token: "your-bearer-token"
control_url: "http://control:8080"
config_mode: "control-only"  # or hybrid
```

**Legacy Tenant Mode** (still supported):
```yaml
tenant_id: "your-tenant-id"
bearer_token: "your-bearer-token"
control_url: "http://control:8080"
config_mode: "hybrid"
```

#### 🛠️ Implementation Details

**New Configuration Field** (`config/config.go:29`):
```go
AccountID string `mapstructure:"account_id"` // Account ID for multi-tenant polling
```

**New Types** (`services/config_polling.go:102-152`):
- `ControlConfiguration` - Tenants + datasets response from Control
- `TenantWithDatasets` - Tenant with its datasets
- `Tenant` - Tenant metadata
- `Dataset` - Dataset configuration including source.custom plugin config

**Key Methods** (`services/config_polling.go`):
- `pollAccountConfiguration()` - Polls Control for account config (line 286-360)
- `datasetsToPluginConfigs()` - Converts datasets to plugin configs (line 691-714)
- `datasetToPluginConfig()` - Converts single dataset to plugin config (line 716-770)
- `reportConfigAppliedForTenant()` - Reports applied config per tenant (line 779-830)

**Security** (`services/config_polling.go:749`):
- Proxy bearer token automatically included in plugin configs
- Secure path validation prevents cache directory traversal

#### 📊 Example Workflow

1. **Control Setup**:
   - Create account: `account-123`
   - Create tenant: `tenant-A` (belongs to `account-123`)
   - Create dataset: `sflow-data` with source.custom containing plugin config

2. **Proxy Setup**:
   ```yaml
   account_id: "account-123"
   bearer_token: "eb4ba9e3236eaefae736495bd79f0d6de753bd7b6547b184ac0c439ff76982cf"
   control_url: "http://192.168.86.103:8082"
   config_mode: "control-only"
   ```

3. **Runtime**:
   - Proxy polls Control every 5 minutes (configurable)
   - Gets all tenants and datasets for `account-123`
   - Creates sFlow listener on port 2066 for `sflow-data` dataset
   - Data received on port 2066 tagged with `tenant-A` and `sflow-data`
   - Spooled to `/var/spool/bytefreezer-proxy/tenant-A/sflow-data/`

#### 🔧 Files Modified

**Proxy**:
- `config/config.go` - Added AccountID field (line 29)
- `services/config_polling.go` - Account-based polling + conversion logic (680+ lines modified)

**Control**:
- `api/api.go` - Added `/api/v2/proxy/config` endpoint (line 155)
- `api/proxy_config_handlers.go` - GetProxyConfiguration handler (lines 285-340)

### 🛡️ DDoS Prevention for Inactive Accounts

#### Exponential Backoff & Circuit Breaker

Prevents inactive proxy instances from overwhelming Control with continuous polling requests.

**Problem Solved**:
- Proxy with invalid/expired bearer token would poll Control every 60 seconds indefinitely
- Inactive accounts would create unnecessary Control load
- No mechanism to back off when account is deactivated

**Solution**:
- **Exponential Backoff**: Automatically increases polling interval on auth failures
- **Circuit Breaker**: Stops polling after 5 consecutive failures
- **Auto-Recovery**: Immediately resets backoff when account becomes active

#### Backoff Schedule

| Failure Count | Backoff Time | Status |
|---------------|--------------|--------|
| 1st failure | 1 minute | Active polling with backoff |
| 2nd failure | 2 minutes | Active polling with backoff |
| 3rd failure | 4 minutes | Active polling with backoff |
| 4th failure | 8 minutes | Active polling with backoff |
| 5th+ failure | 16 → 30 min (max) | Circuit breaker opened |

#### Triggers

Backoff activates on these HTTP status codes:
- **401 Unauthorized** - Invalid or expired bearer token
- **403 Forbidden** - Account lacks permissions
- **404 Not Found** - Account ID doesn't exist in Control

#### Behavior

**During Backoff**:
```
WARN[0120] Polling failed (status 401), backing off for 1m0s (failure 1/5)
WARN[0180] Polling failed (status 401), backing off for 2m0s (failure 2/5)
...
WARN[0300] Circuit breaker opened after 5 consecutive auth failures (backoff: 16m0s)
```

**On Recovery**:
```
INFO[0360] Polling succeeded, resetting backoff (was: 5 failures, 16m0s backoff)
```

**Fallback to Cached Config**:
- Proxy continues using last known good configuration from cache
- Plugins remain running during backoff period
- No service interruption for data collection

#### Implementation Details

**New Fields** (`services/config_polling.go:173-180`):
```go
consecutiveFailures int           // Count of consecutive auth failures
currentBackoff      time.Duration // Current backoff duration
maxBackoff          time.Duration // Maximum backoff (30 minutes)
circuitOpen         bool          // Circuit breaker state
lastAuthError       time.Time     // Last auth error timestamp
```

**Key Methods** (`services/config_polling.go:844-922`):
- `shouldBackoff()` - Checks if currently in backoff period (line 844)
- `recordFailure(statusCode)` - Records failure and calculates backoff (line 861)
- `recordSuccess()` - Resets backoff on successful poll (line 898)
- `getEffectiveInterval()` - Returns current polling interval (line 913)

**Integration**:
- Both `pollAccountConfiguration()` and `pollTenantConfiguration()` check backoff before polling
- Backoff state tracked separately per proxy instance
- Thread-safe with mutex protection

#### Configuration

Maximum backoff time is hardcoded to 30 minutes:
```go
maxBackoff: 30 * time.Minute
```

Future enhancement: Make this configurable via `config_polling.max_backoff_seconds`

#### Benefits

✅ **Prevents DDoS**: Inactive proxies don't hammer Control indefinitely
✅ **Automatic Recovery**: No manual intervention needed when account reactivated
✅ **Graceful Degradation**: Service continues with cached config during backoff
✅ **Observable**: Clear log messages indicate backoff state and reason
✅ **Progressive**: Backoff increases gradually, allowing quick recovery for transient errors

### Deployment Notes

**Migrating from Tenant Mode to Account Mode**:
1. Update proxy config: replace `tenant_id` with `account_id`
2. Ensure all datasets have `source.custom` with `plugin_type`
3. Restart proxy
4. Proxy will poll for all tenants in the account

**Dataset Configuration Requirements**:
- `source.type` must be `"stream"` for plugin generation
- `source.custom.plugin_type` must be set (e.g., "sflow", "netflow")
- `source.custom` should contain all plugin-specific fields (port, protocol, etc.)
- Dataset must be `active: true`

## v4.0.0 - Configuration Polling & Dynamic Reload (2025-10-21)

### Configuration Management System

#### 🔄 Configuration Polling from Control
- **Three Operating Modes**: local-only, control-only, hybrid (default)
- **Automatic Polling**: Fetches configuration from bytefreezer-control every 5 minutes (configurable)
- **Local Caching**: Caches control configuration to `/var/cache/bytefreezer-proxy/{tenant}-{hostname}.json`
- **Graceful Fallback**: Hybrid mode falls back to cached config if control unreachable
- **Change Detection**: SHA256 hash-based comparison for efficient change detection

#### 🔌 Dynamic Plugin Reload
- **Zero-Downtime Reload**: Dynamically reloads plugins when configuration changes
- **Port Conflict Detection**: Detects and resolves port conflicts (remote config takes precedence)
- **Automatic Application**: New configurations applied automatically without restart
- **Status Reporting**: Reports configuration application status back to control

#### 📋 Configuration Modes

**local-only**:
- Uses only local config.yaml
- Ignores control completely
- Suitable for air-gapped environments

**control-only**:
- Fetches all configuration from control
- Fails if control is unavailable
- Suitable for fully centralized management

**hybrid** (default):
- Primary source: control
- Fallback: local cached configuration
- Best of both worlds - centralized with resilience

#### 🛠️ Implementation Details

**New Configuration Fields** (`config/config.go`):
```yaml
config_mode: "hybrid"  # local-only | control-only | hybrid

config_polling:
  enabled: true
  interval_seconds: 300
  timeout_seconds: 30
  cache_directory: "/var/cache/bytefreezer-proxy"
  retry_on_error: true
```

**New Services** (`services/`):
- `config_polling.go`: ConfigPollingService for polling control
- `plugin_service.go`: Added Reload() method for dynamic plugin reload
- `services.go`: Added PluginService and ConfigPollingService fields

**Key Features**:
- Polls control API: `GET /api/v2/proxies/{instanceId}/config?tenant_id={tenantId}`
- Saves config to: `/var/cache/bytefreezer-proxy/{tenant_id}-{instance_id}.json`
- Reports application: `POST /api/v2/proxies/{instanceId}/config/applied`
- Configuration versioning for tracking changes
- Comprehensive logging for debugging

#### 📊 Port Conflict Resolution
When local and remote configs define plugins on the same port:
```
Port conflict: Port 2055: local 'sflow[sflow-listener]' vs remote 'ipfix-listener'
- REMOTE TAKES PRECEDENCE
```
- Remote configuration always wins
- Automatic plugin reload with new configuration
- Detailed conflict logging for transparency

#### 🔐 Security
- Bearer token authentication for control API
- Configuration hash verification (SHA256)
- Cached config files: permissions 0600
- Sensitive data never logged

#### 📈 Observability
**Startup Logs**:
```
Config polling service starting (mode: hybrid, interval: 5m0s)
Loaded cached configuration (version 2) from /var/cache/...
Config polling enabled - polling every 5m0s
```

**Config Change Logs**:
```
Configuration changed (version 2 -> 3)
Applying new configuration (version 3)
Reloading plugin service with new configuration
Plugin service reloaded successfully with 3 plugins
Configuration applied successfully (version 3)
Reported config version 3 as applied to control
```

### Benefits
- **Centralized Management**: Manage all proxy configurations from control
- **High Availability**: Continues operating even if control is unavailable
- **Zero Downtime**: Updates applied without restarting proxy
- **Audit Trail**: All configuration changes tracked with version history
- **Conflict Resolution**: Automatic detection and resolution of port conflicts
- **Flexible Deployment**: Three modes support different operational requirements

### Documentation
- `CONFIG_POLLING_IMPLEMENTATION.md`: Comprehensive implementation guide
- Architecture diagrams and flow charts
- Testing scenarios for all three modes
- API endpoint documentation
- Troubleshooting guide

---

## v3.0.0 - Comprehensive Health Reporting (2025-10-11)

### Health Monitoring Enhancements

#### 📊 Comprehensive Configuration Reporting
- **Full Configuration Export**: Health reports now include complete service configuration with masked sensitive data
- **Sensitive Data Masking**: Bearer tokens and sensitive fields are masked showing only first 2 and last 2 characters (e.g., "to****en")
- **Comprehensive Metrics**: Health reports include all configuration sections (udp, batching, receiver, spooling, housekeeping, otel, soc)
- **Service Capabilities**: Reports include service capability list for better service discovery
- **Multi-Tenant Detection**: Reports whether service is in multi-tenant mode

#### 🔧 Configuration Sections Reported
- **Service Info**: service_type, version, git_commit, instance_id, instance_api, report_interval, timeout
- **API**: port configuration
- **Tenant Info**: tenant_id, bearer_token (masked)
- **UDP**: all configuration including listener count, buffer sizes, batch settings, compression
- **Batching**: enabled status, max_lines, max_bytes, timeout_seconds, compression settings
- **Receiver**: base_url, timeout, upload_worker_count, connection pool settings
- **Spooling**: all configuration including directory, max_size, retry settings, organization settings
- **Housekeeping**: enabled status, interval_seconds
- **OTEL**: enabled status, endpoint, service_name, prometheus_mode, metrics_port
- **SOC**: enabled status, endpoint, timeout
- **Multi-Tenant**: boolean flag indicating multi-tenant mode
- **Capabilities**: udp_ingestion, multi_protocol, batching, spooling, http_forwarding, plugin_system

### Implementation Details
- `services/health_reporting.go:25`: Added `config map[string]interface{}` field to HealthReportingService struct
- `services/health_reporting.go:59`: Updated NewHealthReportingService() signature to accept config parameter
- `services/health_reporting.go:118`: Modified RegisterService() to use full configuration data
- `services/health_reporting.go:223-238`: Modified generateMetrics() to include configuration in metrics
- `main.go:304-395`: Added buildHealthConfiguration() function with sensitive data masking

### Benefits
- **Better Visibility**: Control service can see full configuration of all proxy instances
- **Security**: Sensitive data (bearer tokens) are masked to prevent exposure
- **Troubleshooting**: Configuration information helps diagnose issues across distributed instances
- **Service Discovery**: Capabilities list enables dynamic service routing and orchestration
- **Multi-Tenant Tracking**: Easy identification of multi-tenant vs single-tenant deployments

---

## Latest Changes

### 🚀 Major Architecture Upgrade: Socket-to-Filesystem with Semantic File Extensions
**Release Date**: September 28, 2025

#### Revolutionary Architecture Changes
- **Socket-to-Filesystem**: Complete transition from channel-based to direct filesystem writes
- **Zero Data Loss**: Eliminated all potential message drops due to channel congestion
- **Semantic File Extensions**: Data hint-based file extensions now match actual content types
- **New Plugin Interfaces**: All input plugins now use `SpoolingInterface` for direct writes

#### Core Architecture Benefits
- **🔒 Zero Drop Guarantee**: Direct filesystem writes ensure no message loss
- **📝 Semantic Correctness**: File extensions match content types (`.log` for syslog, `.ndjson` for JSON, etc.)
- **🔍 Data Hint Preservation**: Format information flows through entire pipeline
- **⚡ Improved Performance**: Eliminated channel bottlenecks and memory buffering

#### Plugin Interface Updates
All plugins now use the new interface:
```go
// Old interface (channels)
Start(ctx context.Context, output chan<- *plugins.DataMessage) error

// New interface (direct filesystem)
Start(ctx context.Context, spooler plugins.SpoolingInterface) error
```

#### Data Hint to File Extension Mapping
| Data Hint | File Extension | Use Case |
|-----------|----------------|-----------|
| `ndjson`, `json` | `.ndjson` | Application logs, structured events |
| `syslog` | `.log` | System logs, network device logs |
| `sflow` | `.sflow` | Network flow monitoring, traffic analysis |
| `netflow` | `.netflow` | Network flow monitoring, Cisco devices |
| `csv`, `tsv` | `.csv`, `.tsv` | Metrics, tabular data exports |
| `apache`, `nginx`, `iis` | `.log` | Web server logs |
| `raw` | `.raw` | Legacy logs, free-form messages |
| Custom hints | Custom extensions | Custom formats (≤10 chars) |

#### File Naming Conventions
- **Raw files**: `{nanoseconds}_{bytes}_{lines}.{extension}` (e.g., `1759099025440823103_91_1.log`)
- **Merged files**: `{tenant}--{dataset}--{timestamp}--{dataHint}.gz` (e.g., `test--syslog-data--1759099032349835515--syslog.gz`)

#### Updated Plugin Configurations
All plugins now support `data_hint` parameter:
```yaml
inputs:
  - type: "udp"
    config:
      data_hint: "syslog"    # Creates .log files
  - type: "http"
    config:
      data_hint: "ndjson"    # Creates .ndjson files
  - type: "kafka"
    config:
      data_hint: "ndjson"    # Creates .ndjson files
```

#### Plugins Updated
- ✅ **UDP Plugin**: Direct filesystem writes with data hints
- ✅ **HTTP Plugin**: Direct filesystem writes with data hints
- ✅ **Kafka Plugin**: Direct filesystem writes with data hints
- ✅ **NATS Plugin**: Direct filesystem writes with data hints
- ✅ **SQS Plugin**: Direct filesystem writes with data hints
- ✅ **Kinesis Plugin**: Direct filesystem writes with data hints

#### Testing & Verification
- All unit tests updated and passing
- End-to-end testing confirms proper file extensions
- Line count accuracy maintained through pipeline
- Zero message loss verified under load

#### Migration Notes
- **Configuration**: Add `data_hint` parameters to existing plugin configurations
- **File Extensions**: Existing deployments will see new semantic file extensions
- **Backward Compatibility**: System handles both old and new filename formats
- **Ansible Playbooks**: Updated with proper data hints for all plugin examples

---

### 📊 Enhanced Metadata: Compression Size Tracking
**Release Date**: September 21, 2025

#### New Features
- **Compressed Size Tracking**: Added `compressed_size` field to metadata showing actual file size on disk
- **Uncompressed Size Tracking**: Added `uncompressed_size` field to metadata showing original data size before compression
- **Clean API**: Removed redundant `size` field to eliminate confusion

#### Enhanced Metadata Format
```json
{
  "id": "customer-1--ebpf-data--1758485456619420694",
  "tenant_id": "customer-1",
  "dataset_id": "ebpf-data",
  "compressed_size": 52560,         // Actual file size on disk
  "uncompressed_size": 1280506,     // Original data size (1.28MB uncompressed)
  "line_count": 40080,
  "trigger_reason": "timeout"
}
```

**Note**: Removed redundant `size` field - now only `compressed_size` and `uncompressed_size` for clarity.

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
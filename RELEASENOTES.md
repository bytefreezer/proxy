# ByteFreezer Proxy - Release Notes

## v0.0.4 - Account-Scoped Health Reporting (2025-10-29)

### Security Enhancement

#### 🔐 Account-Scoped Service Health Reporting
- **Previous Issue**: Health reporting used system-wide service API key (`control_api_key`)
  - System API key exposed to customer-deployed proxies
  - Single key compromise would affect entire system
  - Required distributing sensitive system credentials
- **Solution**: Switched to account-scoped health reporting
  - Now uses account-specific `bearer_token` for ALL authentication
  - Same token used for config polling AND health reporting
  - Reports sent to account-scoped endpoints
  - No system API key required
- **Impact**:
  - Single credential (`bearer_token`) for all proxy operations
  - Account isolation - proxy compromise limited to single account
  - Improved security posture for customer deployments
  - Simplified configuration

### Configuration Changes

**Removed**:
```yaml
control_api_key: "..."  # NO LONGER NEEDED - REMOVED
```

**Updated**:
```yaml
bearer_token: "..."  # Now used for ALL authentication (config polling AND health reporting)
```

### API Endpoint Changes

**Before**:
- Registration: `POST /api/v1/health/register`
- Health Report: `POST /api/v1/services/report`
- Auth: System API key (control_api_key)

**After**:
- Registration: `POST /api/v1/accounts/{accountId}/services/register`
- Health Report: `POST /api/v1/accounts/{accountId}/services/report`
- Auth: Account bearer token (bearer_token)

### Files Changed

- `services/health_reporting.go:15-27,64-86,135-144,190-199` (account-scoped endpoints)
- `config/config.go:27-31` (removed ControlAPIKey field)
- `main.go:154-162` (use AccountID and BearerToken)
- `config.yaml:18-23` (removed control_api_key)
- `ansible/playbooks/templates/config.yaml.j2:22-26` (removed control_api_key)

### Migration Instructions

1. **Remove** `control_api_key` from your proxy configuration
2. Ensure `account_id` and `bearer_token` are set
3. Deploy new proxy binary
4. Apply control service database migration `004_health_account_scoping.sql`

**Before**:
```yaml
account_id: "your-account-id"
bearer_token: "account-specific-token"
control_api_key: "system-api-key"  # REMOVE THIS
```

**After**:
```yaml
account_id: "your-account-id"
bearer_token: "account-specific-token"  # Used for everything
```

## v0.0.3 - Health Reporting Authentication Fix (2025-10-29)

### Bug Fixes

#### 🔧 Health Reporting Service Authentication
- **Issue**: Health reporting to control service was failing with 401 Unauthorized errors
  - HealthReportingService was not sending Authorization header
  - Both service registration and health reports were affected
  - Control service requires `Authorization: Bearer <api_key>` for all non-public endpoints
  - Error logged: "Failed to send health report: health report failed with status 401"
- **Fix**: Added API key authentication to health reporting service
  - Added `control_api_key` configuration field for service-to-service authentication
  - Updated `HealthReportingService` struct to store and use API key
  - Modified `NewHealthReportingService()` to accept API key parameter
  - Replaced `httpClient.Post()` with proper request creation including Authorization header
  - Applied fix to both `RegisterService()` and `SendHealthReport()` methods
- **Impact**: Health reporting and service registration now successfully authenticated
- **Files Changed**:
  - `config/config.go:31`
  - `services/health_reporting.go:17, 63, 132-140, 185-193`
  - `main.go:155`
  - `ansible/playbooks/templates/config.yaml.j2:26`
  - `config.yaml:23`

### Configuration Changes

Added new configuration field:
```yaml
control_api_key: "bytefreezer-service-api-key-..."  # Service API key for health reporting
```

This is distinct from `bearer_token` which is the account-specific API key for data ingestion.

## v0.0.2 - Initial Release

Initial release of ByteFreezer Proxy with:
- Plugin-based input system
- Control service integration
- Health reporting service
- Configuration polling
- Multi-tenant support

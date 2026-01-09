# ByteFreezer Proxy - Release Notes

## 2026-01-09

### Features
- **Receiver Capacity Auto-Adjustment**: Proxy now automatically adjusts batch size based on receiver limits
  - Parses receiver capacity info from Control's GetProxyConfiguration response
  - Matches receiver by URL to find the appropriate max_payload_size limit
  - Adjusts `batching.max_bytes` to 95% of receiver's limit (for overhead margin)
  - Prevents HTTP 413 (Payload Too Large) errors for high-volume data streams (e.g., eBPF)
  - Falls back to minimum receiver limit if no exact URL match found
  - Logs adjustment when batch size is reduced

### Bug Fixes
- **Health Report Configuration Update**: Health reports now reflect adjusted batch size
  - Added `UpdateBatchingConfig()` method to HealthReportingService
  - Config polling service calls callback when batch size is adjusted
  - Health reports now show actual runtime batch size, not just startup configuration

### Files Modified
- `services/config_polling.go` - Added ReceiverInfo struct and applyReceiverCapacityLimits() method
- `services/health_reporting.go` - Added UpdateBatchingConfig() method
- `main.go` - Wired up batch size change callback between config polling and health reporting

---

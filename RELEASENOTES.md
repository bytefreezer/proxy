# ByteFreezer Proxy - Release Notes

## 2026-02-17

### Features
- **DLQ Hard Size Limit with FIFO Eviction**: DLQ files were previously never deleted, allowing unbounded growth (observed 900GB on tp2, 510GB on tp1). New `dlq_max_size_bytes` config enforces a hard ceiling on total DLQ size. When exceeded, the oldest files are removed first (FIFO) during the cleanup cycle.
  - Config: `spooling.dlq_max_size_bytes` (default: 10GB, set to 0 for unlimited/legacy behavior)
  - Enforcement runs every cleanup cycle (default 5 minutes)
  - Removes both `.gz` data and `.meta` files for evicted entries
  - Logs count and bytes freed when eviction occurs

### Files Modified
- `config/config.go` - Added `DLQMaxSizeBytes` field to `Spooling` struct with 10GB default
- `services/spooling.go` - Added `enforceDLQLimit()` method, wired into `cleanupWorker`
- `config.yaml` - Added `dlq_max_size_bytes` setting
- `ansible/playbooks/templates/config.yaml.j2` - Added `dlq_max_size_bytes` to template
- `ansible/playbooks/vars/{demo,managed,onprem}.yml` - Set `dlq_max_size_bytes: 10737418240`

---

## 2026-01-22

### Performance
- **Removed Global Mutex in StoreRawMessage**: Eliminated serialization bottleneck for file writes
  - Each raw message writes to a unique file (nanosecond timestamp + length + linecount)
  - All operations are thread-safe without locking: os.MkdirAll is atomic, file writes use unique paths
  - Enables true parallel writes across all workers (previously only 1 worker could write at a time)
  - Throughput increased from ~1,000 msg/sec to ~15,000+ msg/sec for high-volume UDP plugins

- **Increased eBPF Worker Channel Buffer**: Changed from 10,000 to 100,000 items
  - Absorbs traffic bursts without dropping packets
  - Prevents "Worker queue full" drops during high-volume periods

### Configuration
- **Recommended eBPF Worker Count**: Increased default recommendation from 4 to 32 workers
  - High-volume eBPF sources (multiple hosts) benefit from more parallel workers
  - Combined with mutex removal, enables handling of 15,000+ messages/second

### Files Modified
- `services/spooling.go` - Removed mutex.Lock()/Unlock() from StoreRawMessage()
- `plugins/ebpf/plugin.go` - Increased workChan buffer from 10,000 to 100,000

---

## 2026-01-09

### Features
- **Receiver Capacity Auto-Adjustment**: Proxy now automatically adjusts batch size based on receiver limits
  - Parses receiver capacity info from Control's GetProxyConfiguration response
  - Matches receiver by URL to find the appropriate max_payload_size limit
  - Adjusts `batching.max_bytes` to 90% of receiver's limit when config is within 5% of receiver limit
  - Prevents HTTP 413 (Payload Too Large) errors for high-volume data streams (e.g., eBPF)
  - Falls back to minimum receiver limit if no exact URL match found
  - Logs adjustment when batch size is reduced (includes "10% margin" in log message)

### Performance
- **eBPF JSON Processing Optimization**: Reduced CPU usage from 91% to 30% for pretty-printed JSON
  - Added `compactJSONFast()` - byte-level whitespace stripping without full JSON parsing
  - Avoids unnecessary unmarshal/marshal cycle for pretty-printed eBPF data
  - Safe for eBPF JSON which won't have literal newlines in string values

### Bug Fixes
- **Health Report Configuration Update**: Health reports now reflect adjusted batch size
  - Added `UpdateBatchingConfig()` method to HealthReportingService
  - Config polling service calls callback when batch size is adjusted
  - Health reports now show actual runtime batch size, not just startup configuration

- **BatchRawFiles Size Limiting**: Raw file batching now respects batch size limits
  - Previously BatchRawFiles combined ALL raw files into a single batch regardless of size
  - Now splits into multiple batches based on `batching.max_bytes` config
  - Uses conservative 5:1 compression ratio estimate to stay under receiver limits
  - Fixes HTTP 413 errors for high-volume eBPF data streams
  - Default limit of 9MB if not configured (1MB under typical 10MB receiver limit)
  - Logs compressed size in batch creation messages

### Files Modified
- `services/config_polling.go` - Added ReceiverInfo struct and applyReceiverCapacityLimits() with 10% margin
- `services/health_reporting.go` - Added UpdateBatchingConfig() method
- `services/spooling.go` - Added size-based batch splitting in BatchRawFiles()
- `plugins/ebpf/plugin.go` - Added compactJSONFast() for CPU optimization
- `main.go` - Wired up batch size change callback between config polling and health reporting

---

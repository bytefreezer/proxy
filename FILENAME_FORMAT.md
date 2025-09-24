# ByteFreezer Proxy Filename Format

## New Format (v2.0+): `tenant--dataset--timestamp--extension.gz`

ByteFreezer Proxy now uses a structured filename format that embeds metadata directly into the filename, eliminating the need to store FileExtension separately and preventing malformed filename issues.

### Format Structure

```
{tenant}--{dataset}--{timestamp}--{extension}.gz
```

**Components:**
- `tenant`: Tenant identifier (e.g., `acme`, `company`)
- `dataset`: Dataset identifier (e.g., `logs`, `metrics`)
- `timestamp`: Unix nanosecond timestamp (e.g., `1736938245123456789`)
- `extension`: Data format type (e.g., `raw`, `csv`, `ndjson`, `json`)
- `.gz`: Always compressed with gzip

### Examples

| Data Type | Filename Example |
|-----------|------------------|
| Raw logs  | `acme--logs--1736938245123456789--raw.gz` |
| CSV data  | `company--metrics--1736938245123456789--csv.gz` |
| NDJSON    | `tenant1--events--1736938245123456789--ndjson.gz` |
| JSON API  | `api--requests--1736938245123456789--json.gz` |

### Benefits

✅ **Self-Documenting**: All metadata visible in filename
✅ **No Malformed Files**: Extension always extractable from filename
✅ **Validation**: Receiver can validate tenant/dataset matches URL
✅ **Debugging**: Easy to identify file source and timestamp
✅ **Migration Ready**: Supports both old and new formats during transition

### File Lifecycle

#### 1. **Queue Stage** (Immediate Storage)
```
Location: /var/spool/bytefreezer-proxy/{tenant}/{dataset}/queue/
Filename: acme--logs--1736938245123456789--raw.gz
```

#### 2. **Retry Stage** (Upload Processing)
```
Location: /var/spool/bytefreezer-proxy/{tenant}/{dataset}/retry/
Filename: acme--logs--1736938245123456789--raw.gz
Metadata: acme--logs--1736938245123456789--raw.gz.meta
```

#### 3. **DLQ Stage** (Failed Uploads)
```
Location: /var/spool/bytefreezer-proxy/{tenant}/{dataset}/dlq/
Filename: acme--logs--1736938245123456789--raw.gz
Metadata: acme--logs--1736938245123456789--raw.gz.meta
```

### HTTP Headers to Receiver

When forwarding to bytefreezer-receiver, the proxy sends:

```http
POST /webhook/acme/logs
X-Proxy-Filename: acme--logs--1736938245123456789--raw.gz
X-Proxy-File-Extension: raw
X-Proxy-Batch-ID: batch_1736938245123456789
Content-Encoding: gzip
```

### Code Examples

#### Generate Filename
```go
func generateProxyFilename(tenantID, datasetID string, createdAt time.Time, fileExtension string) string {
    timestamp := createdAt.UnixNano()
    return fmt.Sprintf("%s--%s--%d--%s.gz", tenantID, datasetID, timestamp, fileExtension)
}

// Example usage:
filename := generateProxyFilename("acme", "logs", time.Now(), "raw")
// Result: acme--logs--1736938245123456789--raw.gz
```

#### Extract Extension
```go
func extractFileExtension(filename string) string {
    basename := filepath.Base(filename)
    basename = strings.TrimSuffix(basename, ".gz")

    // New format: tenant--dataset--timestamp--extension
    parts := strings.Split(basename, "--")
    if len(parts) >= 4 {
        return parts[3] // extension is 4th part
    }

    // Fallback for old format: batch_id.extension.gz
    if strings.Contains(basename, ".") {
        parts := strings.Split(basename, ".")
        if len(parts) >= 2 {
            return parts[len(parts)-1]
        }
    }

    return ""
}

// Example usage:
ext := extractFileExtension("acme--logs--1736938245123456789--raw.gz")
// Result: "raw"
```

## Old Format (Legacy): `batch_id.extension.gz`

The legacy format is still supported for backward compatibility during migration:

```
batch_{timestamp}.{extension}.gz
```

**Examples:**
- `batch_20250115103045.raw.gz`
- `batch_20250115103045.csv.gz`
- `batch_20250115103045.ndjson.gz`

### Migration Strategy

1. **Phase 1**: Deploy new format generation (proxy creates new format)
2. **Phase 2**: Both formats supported (receiver handles both)
3. **Phase 3**: Legacy cleanup (optional, old format files processed)

### Malformed Filename Detection

Both proxy and receiver detect malformed filenames:

```go
// Detect double dots (malformed)
if strings.Contains(filename, "..") {
    // Store in malformed_proxy/ or malformed_local/ for investigation
    return fmt.Errorf("malformed filename detected: %s", filename)
}
```

**Common Malformed Examples:**
- `batch_123456789..gz` (missing extension)
- `acme--logs--123456789..gz` (missing extension)
- `batch_123456789.gz` (no extension part)

## Implementation Status

- ✅ **Proxy Generation**: New format implemented in `services/forwarder.go`
- ✅ **Proxy Parsing**: Both formats supported in retry/queue processing
- ✅ **Receiver Validation**: New format validation in webhook handlers
- ✅ **Receiver Parsing**: Extension extraction for S3 metadata
- ✅ **Tests**: Comprehensive test coverage for both formats
- ✅ **Documentation**: Format specification and examples
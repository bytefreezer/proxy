# ByteFreezer Proxy Data Flow - When Files Are Sent to Receiver

## 🔄 Complete Data Flow Timeline

### **Phase 1: Data Ingestion (Real-time)**
```
UDP Packet → Plugin → Batching → Spooling → Receiver
     ↓         ↓        ↓          ↓          ↓
   Instant   Instant  Batched   IMMEDIATE   Async
```

### **Phase 2: Detailed Flow Steps**

#### **Step 1: UDP Data Arrives** (Immediate)
- **Location**: `plugins/udp/plugin.go:packetReceiver()`
- **Action**: Raw UDP packet received
- **Timing**: **Instant** - as soon as UDP packet arrives

#### **Step 2: Plugin Processing** (Immediate)
- **Location**: `plugins/udp/plugin.go:messageProcessor()`
- **Action**: Creates `DataMessage` and sends to output channel
- **Code**: `p.output <- dataMsg` (line 336)
- **Timing**: **Instant** - immediate processing

#### **Step 3: Plugin Service Processing** (Immediate)
- **Location**: `services/plugin_service.go:processInputMessages()`
- **Action**: Receives messages from plugin channel
- **Timing**: **Instant** - continuous monitoring

#### **Step 4: Batching Decision** (Configurable)
- **Location**: `services/plugin_service.go:processPluginMessage()`
- **Two paths**:
  1. **Batching Enabled** (default): `ps.batchProcessor.AddMessage(msg)`
  2. **Batching Disabled**: Immediate forward

#### **Step 5: Batch Creation** (Time or Size Triggered)
- **Location**: `services/batch_processor.go` (referenced)
- **Triggers**:
  - **Time**: `batching.timeout_seconds: 30` (default)
  - **Size**: `batching.max_lines: 1000` or `batching.max_bytes: 1MB`
- **Action**: Creates compressed batch

#### **Step 6: IMMEDIATE SPOOLING** ⚡
- **Location**: `services/plugin_service.go:forwardBatch()` → `spoolBatch()`
- **Key Line**: Line 192-193: "SPOOL FIRST - all data goes to spool before any transmission attempts"
- **Action**: `ps.spoolingService.StoreBatchToQueue()`
- **Timing**: **IMMEDIATE** - as soon as batch is created
- **File System**: Data written to `/tmp/bytefreezer-proxy/tenant/dataset/queue/batch.ndjson.gz`

#### **Step 7: Asynchronous Transmission to Receiver** (Background)
- **Location**: `services/spooling.go:retryWorker()`
- **Timing**: **Every retry_interval_seconds (60s default)**
- **Action**: Background process reads from queue and attempts HTTP POST to receiver
- **Success**: File deleted from queue
- **Failure**: File moved to DLQ after 4 attempts

## 📊 Key Timing Characteristics

### **Immediate (< 1ms)**
- ✅ UDP packet reception
- ✅ Plugin processing
- ✅ Channel communication
- ✅ **File system spooling** ← **DATA IS ON DISK IMMEDIATELY**

### **Batched (Configurable - Default 30s)**
- ⏱️ Batch completion (time/size triggered)
- ⏱️ Compression and batch creation

### **Background/Retry (Default 60s intervals)**
- 🔄 HTTP transmission to receiver
- 🔄 Retry attempts on failure
- 🔄 DLQ movement after 4 failures

## 🎯 **ANSWER: When Does Proxy Send to Receiver?**

### **Spooling (File System) - IMMEDIATE**
```
UDP Data → Plugin → Batch → SPOOL (< 1ms after batch creation)
```
**Result**: Data is **immediately** written to file system in queue directory

### **Transmission (HTTP) - BACKGROUND/RETRY**
```
Spool Queue → Background Worker → HTTP POST → Receiver (every 60s)
```
**Result**: HTTP transmission happens in background, **not immediately**

## 📁 File System Behavior

### **Data Path on File System**:
```
/tmp/bytefreezer-proxy/
├── customer-1/
│   └── ebpf-data/
│       ├── queue/           ← Data lands here IMMEDIATELY
│       ├── meta/            ← Metadata for retry tracking
│       └── dlq/             ← Failed attempts after 4 retries
```

### **File Lifecycle**:
1. **Immediate**: `queue/batch_123.ndjson.gz` created
2. **Background**: HTTP POST attempted every 60s
3. **Success**: File deleted from queue
4. **Failure**: After 4 attempts → moved to `dlq/`

## ⚙️ Configuration Controls

### **Batching Timing** (`config.yaml`):
```yaml
batching:
  enabled: true
  max_lines: 1000          # Batch when 1000 lines accumulated
  max_bytes: 1048576       # Batch when 1MB accumulated
  timeout_seconds: 30      # Batch when 30 seconds elapsed
```

### **Retry Timing** (`config.yaml`):
```yaml
spooling:
  retry_interval_seconds: 60    # Retry every 60 seconds
  retry_attempts: 5             # Try 5 times before DLQ
```

### **Receiver Timing** (`config.yaml`):
```yaml
receiver:
  timeout_seconds: 30      # HTTP request timeout
  retry_count: 3           # HTTP-level retries per attempt
```

## 🚨 **Critical Architecture Points**

### **"Spool-First" Architecture**
The proxy uses a **"spool-first"** architecture (line 192 in plugin_service.go):
- **ALL data goes to file system FIRST**
- **Then background workers attempt transmission**
- **No data loss** even if receiver is down

### **No Immediate Transmission**
- The proxy **does NOT** attempt immediate HTTP transmission
- All transmission is **background/asynchronous**
- This provides **reliability** and **backpressure handling**

### **Batching Benefits**
- **Efficiency**: Multiple UDP packets → single HTTP request
- **Compression**: Gzip compression reduces bandwidth
- **Reliability**: File-based retry mechanism

## 🔧 **Answer Summary**

**Q: When does proxy attempt to send files to receiver?**

**A: The proxy has TWO phases:**

1. **File System Spooling**: **IMMEDIATE** (< 1ms after batch creation)
   - Data is written to disk as soon as batching completes
   - Provides immediate data safety

2. **HTTP Transmission**: **BACKGROUND** (every 60 seconds)
   - Background workers read from spool queue
   - Attempt HTTP POST to receiver
   - Retry failed attempts with exponential backoff

**The proxy prioritizes data safety over immediate transmission!** 🛡️
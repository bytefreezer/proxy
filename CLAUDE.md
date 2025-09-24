# Instructions for Claude - ByteFreezer Project

## Project Overview
This is part of the ByteFreezer ecosystem - a comprehensive data ingestion, processing, and analytics platform. Each component serves a specific purpose in the data pipeline.

## Default Workflow
When working on ANY ByteFreezer project, always follow these steps:

### 1. Before Making Changes
- [ ] Read this file first for any specific instructions
- [ ] Check RELEASENOTES.md to understand recent changes (create if missing)
- [ ] Review the current todo list if applicable
- [ ] Understand which ByteFreezer component this is and its role

### 2. When Adding Features
- [ ] Implement the feature with proper error handling
- [ ] Add appropriate logging (use existing log patterns)
- [ ] Build and test the changes (language-appropriate: `go build`, `npm run build`, etc.)
- [ ] Update RELEASENOTES.md with feature description
- [ ] Create example usage documentation if it's an API feature
- [ ] Consider impact on other ByteFreezer components

### 3. When Fixing Bugs
- [ ] Identify root cause and document it
- [ ] Implement fix with error handling
- [ ] Test the fix thoroughly
- [ ] Update RELEASENOTES.md with bug fix details
- [ ] Check if the bug affects other ByteFreezer components

### 4. Code Standards (Language-Specific)

#### For Go Projects (proxy, receiver, soc, control, packer, piper)
- [ ] Follow existing Go conventions in the codebase
- [ ] Use existing imports and patterns (don't add new dependencies without asking)
- [ ] Add proper error logging for user-facing issues
- [ ] Keep functions focused and well-documented
- [ ] Use structured logging with appropriate levels (debug, info, warn, error)

#### For JavaScript/TypeScript Projects (ui, website)
- [ ] Follow existing TypeScript/JavaScript patterns
- [ ] Use existing component patterns and styling approaches
- [ ] Add proper error handling and user feedback
- [ ] Follow existing import/export patterns

#### For Infrastructure Projects (postgres, localstack)
- [ ] Follow existing configuration patterns
- [ ] Document any infrastructure changes
- [ ] Test configuration changes thoroughly
- [ ] Consider impact on dependent services

### 5. API Changes (For Services with APIs)
- [ ] Follow existing API patterns (usually `/api/v2/` for ByteFreezer)
- [ ] Add new endpoints to appropriate router files
- [ ] Create request/response structs with proper struct tags
- [ ] Test with curl examples or appropriate testing tools
- [ ] Document API changes in RELEASENOTES.md

### 6. Always Update
- [ ] RELEASENOTES.md with clear feature/fix descriptions
- [ ] Any relevant example files or documentation
- [ ] Build the binary/application to ensure it compiles
- [ ] Consider if README.md needs updates

## ByteFreezer Component Roles

### Data Ingestion Layer
- **bytefreezer-proxy**: UDP data collection with concurrent upload worker pool and spool-first architecture (queue → retry → dlq)
- **bytefreezer-receiver**: HTTP webhook receiver with spool-based architecture (queue → retry → dlq) that processes batches and uploads to S3

### Processing Layer
- **bytefreezer-packer**: Data compression and packaging
- **bytefreezer-piper**: Data pipeline orchestration and processing

### Control & Monitoring
- **bytefreezer-control**: Central control plane and configuration management
- **bytefreezer-soc**: Security operations center and alerting

### Infrastructure
- **bytefreezer-postgres**: Database schemas and migrations
- **bytefreezer-localstack**: Local development environment setup

### User Interface
- **bytefreezer-ui**: Main web application interface
- **bytefreezer-website**: Public website and documentation

## Current Priority Areas (All Projects)
1. **Reliability**: Error handling, graceful degradation, retry logic
2. **Observability**: Logging, metrics, health checks, debugging tools
3. **Security**: Authentication, authorization, input validation
4. **Performance**: Optimization, caching, resource management
5. **Documentation**: Clear APIs, examples, troubleshooting guides

## ByteFreezer Standards

### Configuration
- Use YAML for configuration files
- Support environment variable overrides
- Validate configuration on startup
- Document all configuration options

### Logging
- Use structured logging (structured format preferred)
- Log levels: debug, info, warn, error
- Include relevant context (tenant_id, request_id, etc.)
- Don't log sensitive information (tokens, passwords)

### Error Handling
- Always return meaningful error messages
- Include context about what operation failed
- Use appropriate HTTP status codes for APIs
- Log errors with sufficient detail for debugging

### APIs
- Follow REST conventions where applicable
- Use consistent response formats
- Include API versioning (typically `/api/v2/`)
- Provide comprehensive error responses
- Document with examples

### Testing
- Build/compile before finishing work
- Test happy path and error conditions
- Verify integration points between components
- Use appropriate testing tools for the language

## Don't Do This
- Don't add complex automation without asking
- Don't change core architecture without discussion
- Don't add new external dependencies without approval
- Don't modify CI/CD or infrastructure without asking
- Don't break backward compatibility without discussion
- Don't commit secrets or sensitive data

## Component Integration Notes
- **Proxy → Receiver**: HTTP POST with compressed data
- **Receiver → S3**: Raw data storage with metadata
- **Control → All**: Configuration and health monitoring
- **SOC → All**: Security alerts and monitoring
- **UI ↔ Control**: API calls for management interface

## When In Doubt
- Ask before making architectural changes
- Follow existing patterns in the codebase
- Keep it simple and maintainable
- Consider the impact on the entire ByteFreezer ecosystem
- Prioritize reliability and observability

## Proxy Architecture

### Concurrent Upload System
The proxy implements a high-performance concurrent upload system aligned with the receiver patterns:

#### Upload Worker Pool
- **Worker Count**: Configurable via `upload_worker_count` (default: 5)
- **Pattern**: Dedicated `UploadWorker` structs with `run()` method
- **Channel**: Buffered upload channel (1000 capacity) shared by all workers
- **Concurrency**: Multiple workers process uploads simultaneously

#### Spool-First Architecture
1. **Data Reception**: Plugin receives UDP/TCP/HTTP data
2. **Batching**: BatchProcessor accumulates data into batches
3. **Spooling**: Batch saved to `/queue` directory FIRST (data safety)
4. **Notification**: Batch added to upload channel for immediate processing
5. **Upload Attempt**: Worker attempts immediate upload to receiver
6. **Success**: File removed from `/queue`
7. **Failure**: File moved to `/retry` with metadata

#### Directory Structure
```
/var/spool/bytefreezer-proxy/{tenant}/{dataset}/
├── queue/    # New files ready for upload (data files only)
├── retry/    # Failed uploads awaiting retry (data + .meta metadata)
└── dlq/      # Dead letter queue - permanently failed (data + .meta metadata)
```

#### Retry Processing
- **Concurrent Workers**: Same worker pool processes both immediate and retry uploads
- **Batch Processing**: Retry jobs collected and distributed to workers
- **Max Retries**: Configurable retry attempts before DLQ
- **Exponential Backoff**: Built into retry scheduling

#### Connection Pooling
- **HTTP Transport**: Custom transport with connection pooling
- **Persistent Connections**: 90-second idle timeout with keep-alive
- **Pool Configuration**:
  - `max_idle_conns: 10` (total pool size)
  - `max_conns_per_host: 6` (per receiver host)

#### Configuration
```yaml
receiver:
  upload_worker_count: 5    # Number of concurrent upload workers
  max_idle_conns: 10        # HTTP connection pool size
  max_conns_per_host: 6     # Max connections per host
```

#### Performance Benefits
- **Throughput**: ~5x upload concurrency vs sequential processing
- **Efficiency**: Connection reuse reduces TCP handshake overhead
- **Reliability**: Spool-first ensures zero data loss
- **Alignment**: Identical patterns with receiver for consistency

## Current Focus (Update as needed)
- Enhanced error handling and debugging capabilities
- Concurrent upload performance optimization
- API standardization across all components
- Improved observability and monitoring
- Documentation and examples
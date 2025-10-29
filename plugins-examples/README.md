# ByteFreezer Proxy - Plugin Development Guide

This directory contains examples, guides, and resources for developing custom input plugins for ByteFreezer Proxy.

## Overview

ByteFreezer Proxy uses a plugin-based architecture that allows developers to create custom input plugins for different data sources. The plugin system is designed to be simple, efficient, and extensible.

## Plugin Architecture

### Core Interface

All input plugins must implement the `InputPlugin` interface:

```go
type InputPlugin interface {
    Name() string
    Configure(config map[string]interface{}) error
    Start(ctx context.Context, output chan<- *DataMessage) error
    Stop() error
    Health() PluginHealth
}
```

### Data Flow

```
Plugin Input → DataMessage → Batch Processor → HTTP Forwarder → ByteFreezer Receiver
```

### Plugin Lifecycle

1. **Registration**: Plugin registers itself in `init()` function
2. **Configuration**: Proxy calls `Configure()` with plugin config
3. **Starting**: Proxy calls `Start()` with context and output channel
4. **Running**: Plugin processes data and sends to output channel
5. **Health Checks**: Proxy periodically calls `Health()`
6. **Stopping**: Proxy calls `Stop()` during shutdown

## Quick Start - Creating a Plugin

### 1. Create Plugin Directory

```bash
mkdir plugins/myplugin
cd plugins/myplugin
```

### 2. Implement Plugin Interface

Create `plugin.go`:

```go
package myplugin

import (
    "context"
    "time"
    
    "github.com/n0needt0/bytefreezer-proxy/plugins"
)

type Plugin struct {
    config Config
    health plugins.PluginHealth
    // Add plugin-specific fields
}

type Config struct {
    TenantID  string `mapstructure:"tenant_id"`
    DatasetID string `mapstructure:"dataset_id"`
    // Add plugin-specific config fields
}

func NewPlugin() plugins.InputPlugin {
    return &Plugin{
        health: plugins.PluginHealth{
            Status:      plugins.HealthStatusStopped,
            Message:     "Plugin created but not started",
            LastUpdated: time.Now(),
        },
    }
}

func (p *Plugin) Name() string {
    return "myplugin"
}

func (p *Plugin) Configure(config map[string]interface{}) error {
    // Decode configuration
    if err := mapstructure.Decode(config, &p.config); err != nil {
        return fmt.Errorf("failed to decode config: %w", err)
    }
    
    // Validate required fields
    if p.config.TenantID == "" {
        return fmt.Errorf("tenant_id is required")
    }
    if p.config.DatasetID == "" {
        return fmt.Errorf("dataset_id is required")
    }
    
    p.updateHealth(plugins.HealthStatusStopped, "Plugin configured", "")
    return nil
}

func (p *Plugin) Start(ctx context.Context, output chan<- *plugins.DataMessage) error {
    p.updateHealth(plugins.HealthStatusStarting, "Starting plugin", "")
    
    // Start your data collection logic here
    go func() {
        for {
            select {
            case <-ctx.Done():
                return
            default:
                // Collect data and send to output channel
                data := []byte("example data")
                msg := &plugins.DataMessage{
                    Data:      data,
                    TenantID:  p.config.TenantID,
                    DatasetID: p.config.DatasetID,
                    Timestamp: time.Now(),
                    Metadata: map[string]string{
                        "plugin": "myplugin",
                    },
                }
                
                select {
                case output <- msg:
                    // Successfully sent
                case <-ctx.Done():
                    return
                }
            }
        }
    }()
    
    p.updateHealth(plugins.HealthStatusHealthy, "Plugin running", "")
    return nil
}

func (p *Plugin) Stop() error {
    p.updateHealth(plugins.HealthStatusStopping, "Stopping plugin", "")
    // Cleanup resources
    p.updateHealth(plugins.HealthStatusStopped, "Plugin stopped", "")
    return nil
}

func (p *Plugin) Health() plugins.PluginHealth {
    return p.health
}

func (p *Plugin) updateHealth(status plugins.HealthStatus, message, lastError string) {
    p.health = plugins.PluginHealth{
        Status:      status,
        Message:     message,
        LastError:   lastError,
        LastUpdated: time.Now(),
    }
}
```

### 3. Register Plugin

Create `init.go`:

```go
package myplugin

import (
    "github.com/n0needt0/bytefreezer-proxy/plugins"
)

func init() {
    plugins.GetRegistry().Register("myplugin", NewPlugin)
}
```

### 4. Import Plugin

Add to `main.go`:

```go
// Import plugin packages to register them
_ "github.com/n0needt0/bytefreezer-proxy/plugins/myplugin"
```

### 5. Configure Plugin

Add to `config.yaml`:

```yaml
inputs:
  - type: "myplugin"
    name: "my-plugin-instance"
    config:
      tenant_id: "my-tenant"
      dataset_id: "my-dataset"
      # Plugin-specific configuration
```

## Available Examples

### [ZMQ Plugin](zmq/README.md)
Complete ZeroMQ (ØMQ) plugin implementation demonstrating:
- ZMQ socket management
- Message pattern handling (PULL, SUB, DEALER)
- Configuration validation
- Error handling and reconnection
- Health monitoring
- Performance optimization

### [Plugin Template](template/README.md)
Boilerplate template for new plugins including:
- Basic plugin structure
- Configuration handling
- Testing framework
- Documentation template
- Build and deployment scripts

## Plugin Development Best Practices

### Configuration

1. **Use mapstructure for config decoding**:
   ```go
   if err := mapstructure.Decode(config, &p.config); err != nil {
       return fmt.Errorf("failed to decode config: %w", err)
   }
   ```

2. **Validate required fields**:
   ```go
   if p.config.TenantID == "" {
       return fmt.Errorf("tenant_id is required")
   }
   ```

3. **Provide sensible defaults**:
   ```go
   if p.config.BufferSize == 0 {
       p.config.BufferSize = 1024
   }
   ```

### Data Processing

1. **Use context for cancellation**:
   ```go
   for {
       select {
       case <-ctx.Done():
           return
       default:
           // Process data
       }
   }
   ```

2. **Handle output channel blocking**:
   ```go
   select {
   case output <- msg:
       // Successfully sent
   case <-ctx.Done():
       return
   default:
       // Channel full, handle appropriately
       log.Warn("Output channel full, dropping message")
   }
   ```

3. **Add proper metadata**:
   ```go
   msg := &plugins.DataMessage{
       Data:      data,
       TenantID:  p.config.TenantID,
       DatasetID: p.config.DatasetID,
       Timestamp: time.Now(),
       SourceIP:  sourceIP,  // If applicable
       Metadata: map[string]string{
           "plugin":       "myplugin",
           "source":       "example-source",
           "content_type": "application/json",
       },
   }
   ```

### Error Handling

1. **Update health status on errors**:
   ```go
   if err != nil {
       p.updateHealth(plugins.HealthStatusUnhealthy, "Connection failed", err.Error())
       return err
   }
   ```

2. **Implement retry logic**:
   ```go
   for retries := 0; retries < maxRetries; retries++ {
       if err := connectToSource(); err == nil {
           break
       }
       time.Sleep(time.Duration(retries+1) * time.Second)
   }
   ```

3. **Log errors appropriately**:
   ```go
   log.Errorf("Plugin %s failed to process message: %v", p.Name(), err)
   ```

### Performance

1. **Use buffered channels for internal processing**:
   ```go
   internal := make(chan []byte, 1000)
   ```

2. **Batch operations when possible**:
   ```go
   batch := make([]*plugins.DataMessage, 0, batchSize)
   ```

3. **Avoid blocking operations in main goroutine**:
   ```go
   go func() {
       // Long-running or blocking operations
   }()
   ```

### Testing

1. **Create comprehensive tests**:
   ```go
   func TestPluginConfigure(t *testing.T) {
       plugin := NewPlugin()
       config := map[string]interface{}{
           "tenant_id":  "test-tenant",
           "dataset_id": "test-dataset",
       }
       
       err := plugin.Configure(config)
       assert.NoError(t, err)
   }
   ```

2. **Test with mock output channel**:
   ```go
   output := make(chan *plugins.DataMessage, 10)
   ctx, cancel := context.WithCancel(context.Background())
   defer cancel()
   
   go plugin.Start(ctx, output)
   ```

3. **Validate health status transitions**:
   ```go
   assert.Equal(t, plugins.HealthStatusHealthy, plugin.Health().Status)
   ```

## Plugin Configuration Schema

### Required Fields

All plugins must support these configuration fields:

```yaml
- type: "plugin-name"          # Plugin type (must match registered name)
  name: "unique-instance-name" # Unique identifier for this instance
  config:                      # Plugin-specific configuration
    tenant_id: "tenant-1"      # Required: target tenant
    dataset_id: "dataset-1"    # Required: target dataset
    # Plugin-specific fields...
```

### Optional Standard Fields

```yaml
config:
  bearer_token: "token"        # Optional: override global bearer token
  buffer_size: 1024           # Optional: internal buffer size
  worker_count: 4             # Optional: number of worker goroutines
  timeout_seconds: 30         # Optional: operation timeouts
  retry_attempts: 3           # Optional: retry attempts on failure
  retry_delay_seconds: 1      # Optional: delay between retries
```

### Plugin-Specific Fields

Each plugin defines its own configuration schema. See individual plugin examples for details.

## Development Workflow

### 1. Design Phase
- Define plugin purpose and data sources
- Design configuration schema
- Plan error handling and health monitoring
- Consider performance requirements

### 2. Implementation Phase
- Use plugin template as starting point
- Implement core plugin interface
- Add configuration validation
- Implement data collection logic
- Add health monitoring

### 3. Testing Phase
- Write unit tests for plugin logic
- Test configuration validation
- Test error handling scenarios
- Perform integration testing
- Load test with realistic data volumes

### 4. Integration Phase
- Register plugin in main.go
- Create example configuration
- Test with full proxy setup
- Validate metrics collection
- Document configuration options

### 5. Documentation Phase
- Create plugin README
- Document configuration schema
- Provide usage examples
- Add troubleshooting guide

## Debugging Plugins

### Enable Debug Logging

```yaml
logging:
  level: debug
```

### Plugin Health Monitoring

```bash
# Check plugin health via API
curl http://localhost:8088/api/v1/health

# Check plugin configuration
curl http://localhost:8088/api/v1/config | jq '.inputs'
```

### Monitor Plugin Metrics

```bash
# Enable metrics and check plugin-specific metrics
curl http://localhost:9090/metrics | grep myplugin
```

### Common Debugging Steps

1. **Verify plugin registration**:
   - Check that init() function is called
   - Verify plugin appears in registry
   - Ensure import statement in main.go

2. **Check configuration**:
   - Validate config syntax
   - Test config validation logic
   - Verify required fields are provided

3. **Monitor health status**:
   - Check health transitions
   - Look for error messages in health status
   - Verify health updates during operation

4. **Test data flow**:
   - Verify output channel receives messages
   - Check message format and metadata
   - Validate tenant/dataset routing

## Advanced Topics

### Multi-tenant Support

Plugins can support per-instance tenant/dataset overrides:

```go
// Use plugin-specific tenant/dataset if provided, fall back to global
tenantID := p.config.TenantID
if tenantID == "" {
    tenantID = globalConfig.TenantID
}
```

### Metrics Integration

Add custom metrics for your plugin:

```go
import "github.com/prometheus/client_golang/prometheus"

var (
    messagesReceived = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "myplugin_messages_received_total",
            Help: "Total messages received by myplugin",
        },
        []string{"dataset_id", "source"},
    )
)

func init() {
    prometheus.MustRegister(messagesReceived)
}

// In plugin logic
messagesReceived.WithLabelValues(p.config.DatasetID, source).Inc()
```

### Connection Pooling

For plugins that connect to external services:

```go
type ConnectionPool struct {
    connections chan Connection
    factory     func() (Connection, error)
}

func (p *ConnectionPool) Get() (Connection, error) {
    select {
    case conn := <-p.connections:
        return conn, nil
    default:
        return p.factory()
    }
}

func (p *ConnectionPool) Put(conn Connection) {
    select {
    case p.connections <- conn:
    default:
        conn.Close()
    }
}
```

### Graceful Shutdown

Implement proper shutdown handling:

```go
func (p *Plugin) Start(ctx context.Context, output chan<- *plugins.DataMessage) error {
    // Start workers
    var wg sync.WaitGroup
    
    wg.Add(1)
    go func() {
        defer wg.Done()
        // Worker logic with context cancellation
    }()
    
    // Wait for context cancellation
    <-ctx.Done()
    
    // Wait for workers to finish
    wg.Wait()
    
    return nil
}
```

## Resources

- [ZMQ Plugin Example](zmq/) - Complete ZeroMQ plugin implementation
- [Plugin Template](template/) - Boilerplate for new plugins
- [Testing Guide](testing/) - Plugin testing framework and examples
- [Deployment Guide](deployment/) - Building and deploying custom plugins

## Contributing

1. Follow the plugin development guidelines
2. Add comprehensive tests
3. Update documentation
4. Submit pull request with example usage

For questions or support, create an issue in the main repository with the `plugin-development` label.
package plugins

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/n0needt0/go-goodies/log"
)

// Manager manages multiple input plugins and routes their data
type Manager struct {
	plugins    map[string]InputPlugin
	configs    []PluginConfig
	output     chan<- *DataMessage
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	mu         sync.RWMutex
	registry   *Registry
	healthTick *time.Ticker
}

// PluginConfig represents configuration for a single plugin instance
type PluginConfig struct {
	Type   string                 `mapstructure:"type"`
	Name   string                 `mapstructure:"name"`
	Config map[string]interface{} `mapstructure:"config"`
}

// NewManager creates a new plugin manager
func NewManager(configs []PluginConfig, output chan<- *DataMessage, registry *Registry) *Manager {
	ctx, cancel := context.WithCancel(context.Background())

	if registry == nil {
		registry = GlobalRegistry
	}

	return &Manager{
		plugins:    make(map[string]InputPlugin),
		configs:    configs,
		output:     output,
		ctx:        ctx,
		cancel:     cancel,
		registry:   registry,
		healthTick: time.NewTicker(30 * time.Second), // Health check every 30 seconds
	}
}

// NewManagerWithGlobals creates a new plugin manager with global configuration support
func NewManagerWithGlobals(configs []PluginConfig, output chan<- *DataMessage, registry *Registry, globalTenantID, globalBearerToken string) *Manager {
	// Enrich plugin configs with global values when missing
	enrichedConfigs := make([]PluginConfig, len(configs))
	for i, config := range configs {
		enrichedConfigs[i] = config

		// Copy the config map to avoid modifying the original
		enrichedConfigs[i].Config = make(map[string]interface{})
		for k, v := range config.Config {
			enrichedConfigs[i].Config[k] = v
		}

		// Add global tenant_id if not present
		if _, exists := enrichedConfigs[i].Config["tenant_id"]; !exists && globalTenantID != "" {
			enrichedConfigs[i].Config["tenant_id"] = globalTenantID
		}

		// Add global bearer_token if not present
		if _, exists := enrichedConfigs[i].Config["bearer_token"]; !exists && globalBearerToken != "" {
			enrichedConfigs[i].Config["bearer_token"] = globalBearerToken
		}
	}

	return NewManager(enrichedConfigs, output, registry)
}

// Start initializes and starts all configured plugins
func (m *Manager) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	log.Infof("Starting plugin manager with %d configs", len(m.configs))

	// Initialize and start each plugin
	for _, config := range m.configs {
		if err := m.startPlugin(config); err != nil {
			log.Errorf("Failed to start plugin %s (%s): %v", config.Name, config.Type, err)
			// Continue starting other plugins even if one fails
			continue
		}
	}

	// Start health monitoring
	m.wg.Add(1)
	go m.healthMonitor()

	log.Infof("Plugin manager started successfully with %d active plugins", len(m.plugins))
	return nil
}

// startPlugin initializes and starts a single plugin
func (m *Manager) startPlugin(config PluginConfig) error {
	// Create plugin instance
	plugin, err := m.registry.Create(config.Type)
	if err != nil {
		return fmt.Errorf("failed to create plugin: %w", err)
	}

	// Configure plugin
	if err := plugin.Configure(config.Config); err != nil {
		return fmt.Errorf("failed to configure plugin: %w", err)
	}

	// Start plugin
	if err := plugin.Start(m.ctx, m.output); err != nil {
		return fmt.Errorf("failed to start plugin: %w", err)
	}

	m.plugins[config.Name] = plugin
	log.Infof("Started plugin %s (%s)", config.Name, config.Type)
	return nil
}

// Stop gracefully shuts down all plugins
func (m *Manager) Stop() error {
	log.Info("Stopping plugin manager")

	// Cancel context to signal all plugins to stop
	m.cancel()

	// Stop health monitoring
	m.healthTick.Stop()

	m.mu.Lock()
	defer m.mu.Unlock()

	// Stop all plugins
	var stopErrors []error
	for name, plugin := range m.plugins {
		if err := plugin.Stop(); err != nil {
			log.Errorf("Error stopping plugin %s: %v", name, err)
			stopErrors = append(stopErrors, fmt.Errorf("plugin %s: %w", name, err))
		} else {
			log.Infof("Stopped plugin %s", name)
		}
	}

	// Wait for all goroutines to finish
	m.wg.Wait()

	if len(stopErrors) > 0 {
		return fmt.Errorf("errors stopping plugins: %v", stopErrors)
	}

	log.Info("Plugin manager stopped successfully")
	return nil
}

// GetPluginHealth returns health status for all plugins
func (m *Manager) GetPluginHealth() map[string]PluginHealth {
	m.mu.RLock()
	defer m.mu.RUnlock()

	health := make(map[string]PluginHealth)
	for name, plugin := range m.plugins {
		health[name] = plugin.Health()
	}
	return health
}

// GetPlugin returns a specific plugin by name
func (m *Manager) GetPlugin(name string) (InputPlugin, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	plugin, exists := m.plugins[name]
	return plugin, exists
}

// ListPlugins returns names of all active plugins
func (m *Manager) ListPlugins() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.plugins))
	for name := range m.plugins {
		names = append(names, name)
	}
	return names
}

// healthMonitor periodically checks plugin health and logs issues
func (m *Manager) healthMonitor() {
	defer m.wg.Done()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-m.healthTick.C:
			m.checkPluginHealth()
		}
	}
}

// checkPluginHealth checks health of all plugins
func (m *Manager) checkPluginHealth() {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for name, plugin := range m.plugins {
		health := plugin.Health()
		if health.Status == HealthStatusUnhealthy {
			log.Warnf("Plugin %s is unhealthy: %s", name, health.Message)
			if health.LastError != "" {
				log.Warnf("Plugin %s last error: %s", name, health.LastError)
			}
		}
	}
}

// GetPluginCount returns the number of active plugins
func (m *Manager) GetPluginCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.plugins)
}

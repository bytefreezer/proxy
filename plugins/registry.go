package plugins

import (
	"fmt"
	"sync"

	"github.com/n0needt0/go-goodies/log"
)

// Registry manages available input plugins
type Registry struct {
	plugins map[string]InputPluginFactory
	mu      sync.RWMutex
}

// NewRegistry creates a new plugin registry
func NewRegistry() *Registry {
	return &Registry{
		plugins: make(map[string]InputPluginFactory),
	}
}

// Register allows plugins to register themselves
func (r *Registry) Register(name string, factory InputPluginFactory) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.plugins[name]; exists {
		return fmt.Errorf("plugin %s is already registered", name)
	}

	r.plugins[name] = factory
	log.Infof("Registered input plugin: %s", name)
	return nil
}

// Create creates a new instance of the specified plugin
func (r *Registry) Create(name string) (InputPlugin, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	factory, exists := r.plugins[name]
	if !exists {
		return nil, fmt.Errorf("unknown plugin type: %s", name)
	}

	return factory(), nil
}

// List returns all registered plugin names
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.plugins))
	for name := range r.plugins {
		names = append(names, name)
	}
	return names
}

// Global registry instance
var GlobalRegistry = NewRegistry()

// GetRegistry returns the global plugin registry instance
func GetRegistry() *Registry {
	return GlobalRegistry
}

// Register is a convenience function for registering plugins with the global registry
func Register(name string, factory InputPluginFactory) error {
	return GlobalRegistry.Register(name, factory)
}

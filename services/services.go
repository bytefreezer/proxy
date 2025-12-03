// Licensed under Elastic License 2.0
// See LICENSE.txt for details

package services

import (
	"github.com/bytefreezer/proxy/config"
	"github.com/bytefreezer/proxy/domain"
	"github.com/bytefreezer/proxy/plugins"
)

// Services holds all service instances and shared state
type Services struct {
	Config               *config.Config
	ProxyStats           *domain.ProxyStats
	SpoolingService      *SpoolingService
	MetricsService       *MetricsService
	PluginRegistry       *plugins.Registry
	PluginService        *PluginService        // Plugin service for dynamic reload
	ConfigPollingService *ConfigPollingService // Config polling service

	// Service instances will be added here
	// UDPListener  *udp.Listener
	// Forwarder    *forwarder.Service
}

// NewServices creates a new services instance
func NewServices(cfg *config.Config) *Services {
	var metricsService *MetricsService

	// Initialize metrics service if OTel is enabled
	if cfg.Otel.Enabled {
		if ms, err := NewMetricsService(); err != nil {
			// Log error but don't fail startup
			// log.Errorf("Failed to initialize metrics service: %v", err)
		} else {
			metricsService = ms
		}
	}

	return &Services{
		Config:          cfg,
		ProxyStats:      &domain.ProxyStats{},
		SpoolingService: NewSpoolingService(cfg),
		MetricsService:  metricsService,
		PluginRegistry:  plugins.GetRegistry(),
	}
}

// IsHealthy checks if all critical services are healthy
func (s *Services) IsHealthy() bool {
	// Add health checks for services
	return true
}

// GetStats returns current proxy statistics
func (s *Services) GetStats() *domain.ProxyStats {
	return s.ProxyStats
}

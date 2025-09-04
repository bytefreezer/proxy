package services

import (
	"github.com/n0needt0/bytefreezer-proxy/config"
	"github.com/n0needt0/bytefreezer-proxy/domain"
)

// Services holds all service instances and shared state
type Services struct {
	Config         *config.Config
	ProxyStats     *domain.ProxyStats
	SpoolingService *SpoolingService

	// Service instances will be added here
	// UDPListener  *udp.Listener
	// Forwarder    *forwarder.Service
}

// NewServices creates a new services instance
func NewServices(cfg *config.Config) *Services {
	return &Services{
		Config:          cfg,
		ProxyStats:      &domain.ProxyStats{},
		SpoolingService: NewSpoolingService(cfg),
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

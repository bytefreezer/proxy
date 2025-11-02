package services

import (
	"github.com/bytedance/sonic"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/n0needt0/bytefreezer-proxy/config"
	"github.com/n0needt0/go-goodies/log"
)

// TenantValidator handles tenant validation with caching
type TenantValidator struct {
	config     *config.TenantValidationConfig
	controlURL string
	httpClient *http.Client
	cache      map[string]*tenantCacheEntry
	cacheMutex sync.RWMutex
	enabled    bool
}

type tenantCacheEntry struct {
	isActive  bool
	expiresAt time.Time
}

// TenantResponse represents a tenant from the control API
type TenantResponse struct {
	ID     string `json:"id"`
	Active bool   `json:"active"`
}

// NewTenantValidator creates a new tenant validator
func NewTenantValidator(cfg *config.TenantValidationConfig, controlURL string) *TenantValidator {
	if cfg == nil || !cfg.Enabled || controlURL == "" {
		return &TenantValidator{
			enabled: false,
		}
	}

	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	if timeout == 0 {
		timeout = 5 * time.Second
	}

	return &TenantValidator{
		config:     cfg,
		controlURL: controlURL,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		cache:   make(map[string]*tenantCacheEntry),
		enabled: true,
	}
}

// IsEnabled returns whether tenant validation is enabled
func (tv *TenantValidator) IsEnabled() bool {
	return tv.enabled
}

// IsActiveTenant checks if a tenant is active (with caching)
func (tv *TenantValidator) IsActiveTenant(tenantID string) bool {
	if !tv.enabled {
		return true // If validation is disabled, assume all tenants are active
	}

	// Check cache first
	tv.cacheMutex.RLock()
	entry, exists := tv.cache[tenantID]
	tv.cacheMutex.RUnlock()

	if exists && time.Now().Before(entry.expiresAt) {
		log.Debugf("Tenant %s cache hit: active=%v", tenantID, entry.isActive)
		return entry.isActive
	}

	// Cache miss or expired - fetch from control API
	isActive := tv.fetchTenantStatus(tenantID)

	// Use different TTL based on tenant status
	var ttl time.Duration
	if isActive {
		// Active tenants: shorter TTL (default 5 minutes)
		ttl = time.Duration(tv.config.CacheTTLSec) * time.Second
		if ttl == 0 {
			ttl = 5 * time.Minute
		}
	} else {
		// Inactive tenants: longer TTL to reduce API load (default 1 hour)
		ttl = time.Duration(tv.config.InactiveCacheTTLSec) * time.Second
		if ttl == 0 {
			ttl = 1 * time.Hour // Default 1 hour for inactive tenants
		}
	}

	tv.cacheMutex.Lock()
	tv.cache[tenantID] = &tenantCacheEntry{
		isActive:  isActive,
		expiresAt: time.Now().Add(ttl),
	}
	tv.cacheMutex.Unlock()

	log.Infof("Tenant %s validation result: active=%v (cached for %v)", tenantID, isActive, ttl)
	return isActive
}

// fetchTenantStatus fetches tenant status from control API
func (tv *TenantValidator) fetchTenantStatus(tenantID string) bool {
	// Construct URL to control API tenant endpoint
	url := fmt.Sprintf("%s/api/v1/tenants/%s", tv.controlURL, tenantID)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Warnf("Failed to create tenant validation request for %s: %v", tenantID, err)
		return true // Fail open - assume active on error
	}

	resp, err := tv.httpClient.Do(req)
	if err != nil {
		log.Warnf("Failed to validate tenant %s with control API: %v", tenantID, err)
		return true // Fail open - assume active on error
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
		log.Infof("Tenant %s not found or inactive (HTTP %d)", tenantID, resp.StatusCode)
		return false
	}

	if resp.StatusCode != http.StatusOK {
		log.Warnf("Unexpected status code %d when validating tenant %s", resp.StatusCode, tenantID)
		return true // Fail open - assume active on unexpected error
	}

	var tenant TenantResponse
	if err := sonic.ConfigDefault.NewDecoder(resp.Body).Decode(&tenant); err != nil {
		log.Warnf("Failed to decode tenant response for %s: %v", tenantID, err)
		return true // Fail open - assume active on decode error
	}

	return tenant.Active
}

// InvalidateCache invalidates the cache for a specific tenant
func (tv *TenantValidator) InvalidateCache(tenantID string) {
	if !tv.enabled {
		return
	}

	tv.cacheMutex.Lock()
	delete(tv.cache, tenantID)
	tv.cacheMutex.Unlock()

	log.Debugf("Invalidated cache for tenant %s", tenantID)
}

// ClearCache clears the entire tenant cache
func (tv *TenantValidator) ClearCache() {
	if !tv.enabled {
		return
	}

	tv.cacheMutex.Lock()
	tv.cache = make(map[string]*tenantCacheEntry)
	tv.cacheMutex.Unlock()

	log.Info("Cleared tenant validation cache")
}

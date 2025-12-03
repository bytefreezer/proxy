// Licensed under Elastic License 2.0
// See LICENSE.txt for details

package config

import (
	"testing"

	"github.com/bytefreezer/proxy/plugins"
)

func TestValidateIdentifiers(t *testing.T) {
	tests := []struct {
		name      string
		tenantID  string
		expectErr bool
	}{
		{
			name:      "valid tenant ID",
			tenantID:  "customer-1",
			expectErr: false,
		},
		{
			name:      "invalid tenant ID with space",
			tenantID:  "customer 1",
			expectErr: true,
		},
		{
			name:      "empty tenant ID",
			tenantID:  "",
			expectErr: true,
		},
		{
			name:      "valid tenant ID with underscores",
			tenantID:  "customer_1_prod",
			expectErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &Config{
				TenantID: tt.tenantID,
			}

			err := validateIdentifiers(config)

			if tt.expectErr && err == nil {
				t.Errorf("Expected error but got none")
			}
			if !tt.expectErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

func TestLoadConfigValidation(t *testing.T) {
	// Test basic config validation
	config := &Config{
		TenantID:    "test-tenant",
		BearerToken: "test-token",
	}

	// Test identifier validation
	err := validateIdentifiers(config)
	if err != nil {
		t.Errorf("Unexpected error validating identifiers: %v", err)
	}

	// Test empty tenant ID
	config.TenantID = ""
	err = validateIdentifiers(config)
	if err == nil {
		t.Error("Expected error for empty tenant ID")
	}
}

func TestValidatePluginInputs(t *testing.T) {
	t.Run("valid plugin inputs", func(t *testing.T) {
		cfg := &Config{
			TenantID: "test-tenant",
			Inputs: []plugins.PluginConfig{
				{
					Type: "udp",
					Name: "udp-listener",
					Config: map[string]interface{}{
						"dataset_id": "dataset-1",
						"port":       2056,
					},
				},
				{
					Type: "http",
					Name: "http-webhook",
					Config: map[string]interface{}{
						"dataset_id": "dataset-2",
						"port":       8080,
					},
				},
			},
		}

		err := validatePluginInputs(cfg)
		if err != nil {
			t.Errorf("Unexpected error with valid inputs: %v", err)
		}
	})

	t.Run("duplicate dataset same tenant", func(t *testing.T) {
		cfg := &Config{
			TenantID: "test-tenant",
			Inputs: []plugins.PluginConfig{
				{
					Type: "udp",
					Name: "udp-listener-1",
					Config: map[string]interface{}{
						"dataset_id": "duplicate-dataset",
						"port":       2056,
					},
				},
				{
					Type: "http",
					Name: "http-webhook",
					Config: map[string]interface{}{
						"dataset_id": "duplicate-dataset", // Same dataset ID
						"port":       8080,
					},
				},
			},
		}

		err := validatePluginInputs(cfg)
		if err == nil {
			t.Error("Expected error for duplicate dataset ID in same tenant")
		}
		if err != nil && !stringContains(err.Error(), "already configured") {
			t.Errorf("Error should mention duplicate configuration, got: %v", err)
		}
	})

	t.Run("same dataset different tenants allowed", func(t *testing.T) {
		cfg := &Config{
			TenantID: "default-tenant",
			Inputs: []plugins.PluginConfig{
				{
					Type: "udp",
					Name: "udp-listener-1",
					Config: map[string]interface{}{
						"tenant_id":  "tenant-a",
						"dataset_id": "same-dataset",
						"port":       2056,
					},
				},
				{
					Type: "http",
					Name: "http-webhook",
					Config: map[string]interface{}{
						"tenant_id":  "tenant-b",
						"dataset_id": "same-dataset", // Same dataset but different tenant
						"port":       8080,
					},
				},
			},
		}

		err := validatePluginInputs(cfg)
		if err != nil {
			t.Errorf("Unexpected error with same dataset in different tenants: %v", err)
		}
	})

	t.Run("missing dataset_id", func(t *testing.T) {
		cfg := &Config{
			TenantID: "test-tenant",
			Inputs: []plugins.PluginConfig{
				{
					Type: "udp",
					Name: "udp-listener",
					Config: map[string]interface{}{
						"port": 2056,
						// Missing dataset_id
					},
				},
			},
		}

		err := validatePluginInputs(cfg)
		if err == nil {
			t.Error("Expected error for missing dataset_id")
		}
		if err != nil && !stringContains(err.Error(), "dataset_id is required") {
			t.Errorf("Error should mention missing dataset_id, got: %v", err)
		}
	})

	t.Run("port conflicts between different datasets", func(t *testing.T) {
		cfg := &Config{
			TenantID: "test-tenant",
			Inputs: []plugins.PluginConfig{
				{
					Type: "udp",
					Name: "udp-listener-1",
					Config: map[string]interface{}{
						"dataset_id": "dataset-1",
						"port":       8080,
					},
				},
				{
					Type: "http",
					Name: "http-webhook",
					Config: map[string]interface{}{
						"dataset_id": "dataset-2",
						"port":       8080, // Same port as UDP listener
					},
				},
			},
		}

		err := validatePluginInputs(cfg)
		if err == nil {
			t.Error("Expected error for port conflict")
		}
		if err != nil && !stringContains(err.Error(), "port 8080 is already in use") {
			t.Errorf("Error should mention port conflict, got: %v", err)
		}
	})

	t.Run("different ports are allowed", func(t *testing.T) {
		cfg := &Config{
			TenantID: "test-tenant",
			Inputs: []plugins.PluginConfig{
				{
					Type: "udp",
					Name: "udp-listener",
					Config: map[string]interface{}{
						"dataset_id": "dataset-1",
						"port":       8080,
					},
				},
				{
					Type: "http",
					Name: "http-webhook",
					Config: map[string]interface{}{
						"dataset_id": "dataset-2",
						"port":       8081, // Different port
					},
				},
			},
		}

		err := validatePluginInputs(cfg)
		if err != nil {
			t.Errorf("Unexpected error with different ports: %v", err)
		}
	})

	t.Run("invalid port range", func(t *testing.T) {
		cfg := &Config{
			TenantID: "test-tenant",
			Inputs: []plugins.PluginConfig{
				{
					Type: "udp",
					Name: "udp-listener",
					Config: map[string]interface{}{
						"dataset_id": "dataset-1",
						"port":       70000, // Invalid port
					},
				},
			},
		}

		err := validatePluginInputs(cfg)
		if err == nil {
			t.Error("Expected error for invalid port range")
		}
		if err != nil && !stringContains(err.Error(), "port 70000 is invalid") {
			t.Errorf("Error should mention invalid port, got: %v", err)
		}
	})
}

func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

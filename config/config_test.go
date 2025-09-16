package config

import (
	"testing"
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
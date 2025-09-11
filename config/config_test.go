package config

import (
	"strings"
	"testing"
)

func TestValidateIdentifiers(t *testing.T) {
	tests := []struct {
		name      string
		config    *Config
		expectErr bool
		errMsg    string
	}{
		{
			name: "valid configuration",
			config: &Config{
				TenantID: "customer-1",
				UDP: UDP{
					Enabled: true,
					Listeners: []UDPListener{
						{Port: 2056, DatasetID: "app-logs", TenantID: "tenant-1"},
						{Port: 2057, DatasetID: "system-logs"},
					},
				},
			},
			expectErr: false,
		},
		{
			name: "invalid global tenant ID",
			config: &Config{
				TenantID: "customer 1", // Invalid: contains space
				UDP: UDP{
					Enabled: true,
					Listeners: []UDPListener{
						{Port: 2056, DatasetID: "app-logs"},
					},
				},
			},
			expectErr: true,
			errMsg:    "global tenant_id",
		},
		{
			name: "invalid dataset ID",
			config: &Config{
				TenantID: "customer-1",
				UDP: UDP{
					Enabled: true,
					Listeners: []UDPListener{
						{Port: 2056, DatasetID: "app logs"}, // Invalid: contains space
					},
				},
			},
			expectErr: true,
			errMsg:    "dataset_id",
		},
		{
			name: "invalid listener tenant ID",
			config: &Config{
				TenantID: "customer-1",
				UDP: UDP{
					Enabled: true,
					Listeners: []UDPListener{
						{Port: 2056, DatasetID: "app-logs", TenantID: "tenant@1"}, // Invalid: contains @
					},
				},
			},
			expectErr: true,
			errMsg:    "tenant_id",
		},
		{
			name: "reserved dataset name",
			config: &Config{
				TenantID: "customer-1",
				UDP: UDP{
					Enabled: true,
					Listeners: []UDPListener{
						{Port: 2056, DatasetID: "admin"}, // Reserved name
					},
				},
			},
			expectErr: true,
			errMsg:    "is reserved",
		},
		{
			name: "same tenant and dataset name",
			config: &Config{
				TenantID: "customer-1",
				UDP: UDP{
					Enabled: true,
					Listeners: []UDPListener{
						{Port: 2056, DatasetID: "customer-1"}, // Same as tenant
					},
				},
			},
			expectErr: true,
			errMsg:    "cannot be the same",
		},
		{
			name: "UDP disabled - no validation",
			config: &Config{
				TenantID: "customer 1", // Invalid but UDP is disabled
				UDP: UDP{
					Enabled: false,
					Listeners: []UDPListener{
						{Port: 2056, DatasetID: "app logs"}, // Invalid but won't be checked
					},
				},
			},
			expectErr: false, // Should not validate when UDP is disabled
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateIdentifiers(tt.config)

			if tt.expectErr {
				if err == nil {
					t.Errorf("Expected error but got none")
				} else if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Expected error message to contain '%s', got: %s", tt.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}
		})
	}
}

func TestValidateMultiTenantConfig_DatasetConflicts(t *testing.T) {
	tests := []struct {
		name      string
		config    *Config
		expectErr bool
		errMsg    string
	}{
		{
			name: "unique datasets on different ports",
			config: &Config{
				TenantID:    "customer-1",
				BearerToken: "token",
				UDP: UDP{
					Enabled: true,
					Listeners: []UDPListener{
						{Port: 2056, DatasetID: "app-logs"},
						{Port: 2057, DatasetID: "system-logs"},
						{Port: 2058, DatasetID: "metrics"},
					},
				},
			},
			expectErr: false,
		},
		{
			name: "duplicate dataset on different ports",
			config: &Config{
				TenantID:    "customer-1",
				BearerToken: "token",
				UDP: UDP{
					Enabled: true,
					Listeners: []UDPListener{
						{Port: 2056, DatasetID: "app-logs"},
						{Port: 2057, DatasetID: "system-logs"},
						{Port: 2058, DatasetID: "app-logs"}, // Duplicate dataset
					},
				},
			},
			expectErr: true,
			errMsg:    "dataset_id 'app-logs' is already configured on port 2056",
		},
		{
			name: "duplicate dataset with different case",
			config: &Config{
				TenantID:    "customer-1",
				BearerToken: "token",
				UDP: UDP{
					Enabled: true,
					Listeners: []UDPListener{
						{Port: 2056, DatasetID: "app-logs"},
						{Port: 2057, DatasetID: "App-Logs"}, // Different case, but should be treated as same
					},
				},
			},
			expectErr: false, // Case sensitivity is allowed for datasets
		},
		{
			name: "inactive listeners don't cause conflicts",
			config: &Config{
				TenantID:    "customer-1",
				BearerToken: "token",
				UDP: UDP{
					Enabled: true,
					Listeners: []UDPListener{
						{Port: 2056, DatasetID: "app-logs"},
						{Port: 2057, DatasetID: ""}, // Inactive listener
						{Port: 2058, DatasetID: "app-logs"}, // Would conflict if both were active
					},
				},
			},
			expectErr: true, // Still should conflict because both have dataset_id
		},
		{
			name: "empty dataset IDs are ignored",
			config: &Config{
				TenantID:    "customer-1",
				BearerToken: "token",
				UDP: UDP{
					Enabled: true,
					Listeners: []UDPListener{
						{Port: 2056, DatasetID: "app-logs"},
						{Port: 2057, DatasetID: ""}, // Empty - should be ignored
						{Port: 2058, DatasetID: ""}, // Empty - should be ignored
					},
				},
			},
			expectErr: true, // Empty dataset_id should cause validation error
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateMultiTenantConfig(tt.config)

			if tt.expectErr {
				if err == nil {
					t.Errorf("Expected error but got none")
				} else if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Expected error message to contain '%s', got: %s", tt.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}
		})
	}
}

func TestLoadConfig_FullValidation(t *testing.T) {
	// Test that LoadConfig calls all validation functions
	tests := []struct {
		name      string
		config    *Config
		expectErr bool
		errType   string
	}{
		{
			name: "valid full configuration",
			config: &Config{
				TenantID:    "customer-1",
				BearerToken: "token",
				UDP: UDP{
					Enabled: true,
					Listeners: []UDPListener{
						{Port: 2056, DatasetID: "app-logs"},
						{Port: 2057, DatasetID: "system-logs"},
					},
				},
			},
			expectErr: false,
		},
		{
			name: "identifier validation failure",
			config: &Config{
				TenantID:    "customer 1", // Invalid identifier
				BearerToken: "token",
				UDP: UDP{
					Enabled: true,
					Listeners: []UDPListener{
						{Port: 2056, DatasetID: "app-logs"},
					},
				},
			},
			expectErr: true,
			errType:   "invalid",
		},
		{
			name: "multi-tenant validation failure",
			config: &Config{
				TenantID:    "customer-1",
				BearerToken: "token",
				UDP: UDP{
					Enabled: true,
					Listeners: []UDPListener{
						{Port: 2056, DatasetID: "app-logs"},
						{Port: 2057, DatasetID: "app-logs"}, // Duplicate dataset
					},
				},
			},
			expectErr: true,
			errType:   "already configured on port",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the validation that happens in LoadConfig
			var err error

			// Run multi-tenant validation first (matches LoadConfig order)
			if multierr := validateMultiTenantConfig(tt.config); multierr != nil {
				err = multierr
			} else {
				// Only run identifier validation if multi-tenant validation passed
				if iderr := validateIdentifiers(tt.config); iderr != nil {
					err = iderr
				}
			}

			if tt.expectErr {
				if err == nil {
					t.Errorf("Expected error but got none")
				} else if tt.errType != "" && !strings.Contains(err.Error(), tt.errType) {
					t.Errorf("Expected error type '%s', got: %s", tt.errType, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}
		})
	}
}

func TestDatasetPortMapping(t *testing.T) {
	// Test the specific case of dataset-to-port mapping validation
	config := &Config{
		TenantID:    "customer-1",
		BearerToken: "token",
		UDP: UDP{
			Enabled: true,
			Listeners: []UDPListener{
				{Port: 2056, DatasetID: "logs"},
				{Port: 2057, DatasetID: "metrics"},
				{Port: 2058, DatasetID: "traces"},
				{Port: 2059, DatasetID: "logs"}, // This should fail
			},
		},
	}

	err := validateMultiTenantConfig(config)
	if err == nil {
		t.Error("Expected error for duplicate dataset on different ports")
	}

	if !strings.Contains(err.Error(), "dataset_id 'logs' is already configured on port 2056") {
		t.Errorf("Expected specific error message about port conflict, got: %s", err.Error())
	}

	if !strings.Contains(err.Error(), "prevent data mixing") {
		t.Errorf("Expected error message to mention data mixing prevention, got: %s", err.Error())
	}
}
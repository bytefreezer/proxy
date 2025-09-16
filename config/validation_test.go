package config

import (
	"strings"
	"testing"
)

func TestValidateTenantID(t *testing.T) {
	tests := []struct {
		name      string
		tenantID  string
		expectErr bool
		errMsg    string
	}{
		// Valid cases
		{
			name:      "valid alphanumeric",
			tenantID:  "customer1",
			expectErr: false,
		},
		{
			name:      "valid with hyphens",
			tenantID:  "customer-1",
			expectErr: false,
		},
		{
			name:      "valid with underscores",
			tenantID:  "customer_1",
			expectErr: false,
		},
		{
			name:      "valid mixed",
			tenantID:  "customer-1_test",
			expectErr: false,
		},
		{
			name:      "valid single character",
			tenantID:  "a",
			expectErr: false,
		},
		{
			name:      "valid numbers",
			tenantID:  "123",
			expectErr: false,
		},
		{
			name:      "valid long name",
			tenantID:  "customer1234567890abcdefghijklmnopqrstuvwxyz1234567890abcdefgh",
			expectErr: false,
		},

		// Invalid cases - empty
		{
			name:      "empty string",
			tenantID:  "",
			expectErr: true,
			errMsg:    "cannot be empty",
		},

		// Invalid cases - length
		{
			name:      "too long",
			tenantID:  strings.Repeat("a", 65),
			expectErr: true,
			errMsg:    "cannot exceed 64 characters",
		},

		// Invalid cases - special characters
		{
			name:      "with spaces",
			tenantID:  "customer 1",
			expectErr: true,
			errMsg:    "must contain only alphanumeric",
		},
		{
			name:      "with dots",
			tenantID:  "customer.1",
			expectErr: true,
			errMsg:    "must contain only alphanumeric",
		},
		{
			name:      "with slashes",
			tenantID:  "customer/1",
			expectErr: true,
			errMsg:    "must contain only alphanumeric",
		},
		{
			name:      "with special chars",
			tenantID:  "customer@1",
			expectErr: true,
			errMsg:    "must contain only alphanumeric",
		},

		// Invalid cases - start/end with hyphen/underscore
		{
			name:      "starts with hyphen",
			tenantID:  "-customer",
			expectErr: true,
			errMsg:    "cannot start or end with hyphen/underscore",
		},
		{
			name:      "ends with hyphen",
			tenantID:  "customer-",
			expectErr: true,
			errMsg:    "cannot start or end with hyphen/underscore",
		},
		{
			name:      "starts with underscore",
			tenantID:  "_customer",
			expectErr: true,
			errMsg:    "cannot start or end with hyphen/underscore",
		},
		{
			name:      "ends with underscore",
			tenantID:  "customer_",
			expectErr: true,
			errMsg:    "cannot start or end with hyphen/underscore",
		},

		// Invalid cases - reserved names
		{
			name:      "reserved name - admin",
			tenantID:  "admin",
			expectErr: true,
			errMsg:    "is reserved",
		},
		{
			name:      "reserved name - system",
			tenantID:  "system",
			expectErr: true,
			errMsg:    "is reserved",
		},
		{
			name:      "reserved name - proxy",
			tenantID:  "proxy",
			expectErr: true,
			errMsg:    "is reserved",
		},
		{
			name:      "reserved name case insensitive",
			tenantID:  "ADMIN",
			expectErr: true,
			errMsg:    "is reserved",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTenantID(tt.tenantID)

			if tt.expectErr {
				if err == nil {
					t.Errorf("Expected error for tenant_id '%s', but got none", tt.tenantID)
				} else if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Expected error message to contain '%s', got: %s", tt.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error for valid tenant_id '%s': %v", tt.tenantID, err)
				}
			}
		})
	}
}

func TestValidateDatasetID(t *testing.T) {
	tests := []struct {
		name      string
		datasetID string
		expectErr bool
		errMsg    string
	}{
		// Valid cases
		{
			name:      "valid alphanumeric",
			datasetID: "data",
			expectErr: false,
		},
		{
			name:      "valid with hyphens",
			datasetID: "app-logs",
			expectErr: false,
		},
		{
			name:      "valid with underscores",
			datasetID: "app_logs",
			expectErr: false,
		},
		{
			name:      "valid mixed",
			datasetID: "app-logs_v2",
			expectErr: false,
		},

		// Invalid cases (same rules as tenant)
		{
			name:      "empty string",
			datasetID: "",
			expectErr: true,
			errMsg:    "cannot be empty",
		},
		{
			name:      "with spaces",
			datasetID: "app logs",
			expectErr: true,
			errMsg:    "must contain only alphanumeric",
		},
		{
			name:      "starts with hyphen",
			datasetID: "-logs",
			expectErr: true,
			errMsg:    "cannot start or end with hyphen/underscore",
		},
		{
			name:      "reserved name",
			datasetID: "admin",
			expectErr: true,
			errMsg:    "is reserved",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDatasetID(tt.datasetID)

			if tt.expectErr {
				if err == nil {
					t.Errorf("Expected error for dataset_id '%s', but got none", tt.datasetID)
				} else if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Expected error message to contain '%s', got: %s", tt.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error for valid dataset_id '%s': %v", tt.datasetID, err)
				}
			}
		})
	}
}

func TestValidateIdentifierPair(t *testing.T) {
	tests := []struct {
		name      string
		tenantID  string
		datasetID string
		expectErr bool
		errMsg    string
	}{
		{
			name:      "valid pair",
			tenantID:  "customer1",
			datasetID: "app-logs",
			expectErr: false,
		},
		{
			name:      "invalid tenant",
			tenantID:  "customer 1",
			datasetID: "app-logs",
			expectErr: true,
			errMsg:    "invalid tenant",
		},
		{
			name:      "invalid dataset",
			tenantID:  "customer1",
			datasetID: "app logs",
			expectErr: true,
			errMsg:    "invalid dataset",
		},
		{
			name:      "same names",
			tenantID:  "test",
			datasetID: "test",
			expectErr: true,
			errMsg:    "cannot be the same",
		},
		{
			name:      "same names case insensitive",
			tenantID:  "Test",
			datasetID: "test",
			expectErr: true,
			errMsg:    "cannot be the same",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateIdentifierPair(tt.tenantID, tt.datasetID)

			if tt.expectErr {
				if err == nil {
					t.Errorf("Expected error for pair (%s, %s), but got none", tt.tenantID, tt.datasetID)
				} else if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Expected error message to contain '%s', got: %s", tt.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error for valid pair (%s, %s): %v", tt.tenantID, tt.datasetID, err)
				}
			}
		})
	}
}

func TestSanitizeIdentifier(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "valid input unchanged",
			input:    "customer1",
			expected: "customer1",
		},
		{
			name:     "spaces replaced",
			input:    "customer 1",
			expected: "customer_1",
		},
		{
			name:     "special chars replaced",
			input:    "customer@1.test",
			expected: "customer_1_test",
		},
		{
			name:     "leading hyphen removed",
			input:    "-customer",
			expected: "customer",
		},
		{
			name:     "trailing underscore fixed",
			input:    "customer_",
			expected: "customer",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "default",
		},
		{
			name:     "only special chars",
			input:    "@#$%",
			expected: "default",
		},
		{
			name:     "too long",
			input:    strings.Repeat("a", 70),
			expected: strings.Repeat("a", 64),
		},
		{
			name:     "reserved name",
			input:    "admin",
			expected: "admin1",
		},
		{
			name:     "complex case",
			input:    "_customer@domain.com_",
			expected: "customer_domain_com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeIdentifier(tt.input)
			if result != tt.expected {
				t.Errorf("SanitizeIdentifier(%s) = %s, expected %s", tt.input, result, tt.expected)
			}

			// Verify the result is valid
			if err := ValidateTenantID(result); err != nil {
				t.Errorf("Sanitized result '%s' is not valid: %v", result, err)
			}
		})
	}
}

func TestReservedNames(t *testing.T) {
	reservedNames := []string{
		"admin", "root", "system", "proxy", "api",
		"tmp", "log", "var", "etc", "bin",
		"udp", "tcp", "http", "syslog", "netflow",
		"con", "prn", "aux", "nul", "com1", "lpt1",
	}

	for _, name := range reservedNames {
		t.Run("reserved_"+name, func(t *testing.T) {
			if !isReservedName(name) {
				t.Errorf("Expected '%s' to be reserved", name)
			}

			// Test case insensitive
			if !isReservedName(strings.ToUpper(name)) {
				t.Errorf("Expected '%s' (uppercase) to be reserved", name)
			}
		})
	}
}

func TestIdentifierValidationPattern(t *testing.T) {
	validCases := []string{
		"a", "1", "a1", "customer1", "app-logs", "data_set", "test-data_v2", "ABC123",
	}

	invalidCases := []string{
		"", "-test", "test-", "_test", "test_", "test ", " test", "test.com", "test@domain",
		"test/path", "test\\path", "test:port", "test?query", "test#anchor", "test%20space",
	}

	for _, valid := range validCases {
		t.Run("valid_pattern_"+valid, func(t *testing.T) {
			if !identifierPattern.MatchString(valid) {
				t.Errorf("Expected '%s' to match identifier pattern", valid)
			}
		})
	}

	for _, invalid := range invalidCases {
		t.Run("invalid_pattern_"+invalid, func(t *testing.T) {
			if identifierPattern.MatchString(invalid) {
				t.Errorf("Expected '%s' to NOT match identifier pattern", invalid)
			}
		})
	}
}

func BenchmarkValidateTenantID(b *testing.B) {
	tenantID := "customer-1_test"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ValidateTenantID(tenantID)
	}
}

func BenchmarkSanitizeIdentifier(b *testing.B) {
	input := "customer@domain.com with spaces"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = SanitizeIdentifier(input)
	}
}

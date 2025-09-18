package config

import (
	"fmt"
	"regexp"
	"strings"
)

// ValidateIdentifier validates tenant and dataset identifiers
// Identifiers must be alphanumeric (a-z, A-Z, 0-9) and may contain hyphens and underscores
// but cannot start or end with hyphens/underscores
var identifierPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*[a-zA-Z0-9]$|^[a-zA-Z0-9]$`)

// ValidateTenantID validates a tenant identifier
func ValidateTenantID(tenantID string) error {
	if tenantID == "" {
		return fmt.Errorf("tenant_id cannot be empty")
	}

	if len(tenantID) > 64 {
		return fmt.Errorf("tenant_id cannot exceed 64 characters, got %d", len(tenantID))
	}

	if !identifierPattern.MatchString(tenantID) {
		return fmt.Errorf("tenant_id '%s' is invalid: must contain only alphanumeric characters, hyphens, and underscores, cannot start or end with hyphen/underscore", tenantID)
	}

	// Additional checks for reserved names
	if isReservedName(tenantID) {
		return fmt.Errorf("tenant_id '%s' is reserved and cannot be used", tenantID)
	}

	return nil
}

// ValidateDatasetID validates a dataset identifier
func ValidateDatasetID(datasetID string) error {
	if datasetID == "" {
		return fmt.Errorf("dataset_id cannot be empty")
	}

	if len(datasetID) > 64 {
		return fmt.Errorf("dataset_id cannot exceed 64 characters, got %d", len(datasetID))
	}

	if !identifierPattern.MatchString(datasetID) {
		return fmt.Errorf("dataset_id '%s' is invalid: must contain only alphanumeric characters, hyphens, and underscores, cannot start or end with hyphen/underscore", datasetID)
	}

	// Additional checks for reserved names
	if isReservedName(datasetID) {
		return fmt.Errorf("dataset_id '%s' is reserved and cannot be used", datasetID)
	}

	return nil
}

// isReservedName checks if the name is reserved by the system
func isReservedName(name string) bool {
	reservedNames := []string{
		// System directories
		"tmp", "temp", "cache", "log", "logs", "var", "etc", "bin", "usr", "opt",
		// Common protocol names that might cause confusion
		"udp", "tcp", "http", "https", "ftp", "ssh", "syslog", "netflow", "sflow",
		// Application-specific reserved names
		"proxy", "receiver", "piper", "api", "admin", "root", "system",
		// Common metadata terms
		"meta", "metadata", "config", "settings", "status", "health",
		// File system reserved names (case-insensitive check)
		"con", "prn", "aux", "nul", "com1", "com2", "com3", "com4", "com5", "com6", "com7", "com8", "com9",
		"lpt1", "lpt2", "lpt3", "lpt4", "lpt5", "lpt6", "lpt7", "lpt8", "lpt9",
	}

	nameLower := strings.ToLower(name)
	for _, reserved := range reservedNames {
		if nameLower == reserved {
			return true
		}
	}

	return false
}


// ValidateIdentifierPair validates both tenant and dataset IDs together
func ValidateIdentifierPair(tenantID, datasetID string) error {
	if err := ValidateTenantID(tenantID); err != nil {
		return fmt.Errorf("invalid tenant: %w", err)
	}

	if err := ValidateDatasetID(datasetID); err != nil {
		return fmt.Errorf("invalid dataset: %w", err)
	}

	// Check for conflicts between tenant and dataset names
	if strings.EqualFold(tenantID, datasetID) {
		return fmt.Errorf("tenant_id and dataset_id cannot be the same (case-insensitive): '%s'", tenantID)
	}

	return nil
}

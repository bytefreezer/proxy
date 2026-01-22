// Licensed under Elastic License 2.0
// See LICENSE.txt for details

package utils

// ToInt converts an interface{} value to int.
// Handles int, float64, and int64 types commonly seen in JSON/YAML parsing.
// Returns 0 and false if the value cannot be converted.
func ToInt(value any) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case float64:
		return int(v), true
	case int64:
		return int(v), true
	default:
		return 0, false
	}
}

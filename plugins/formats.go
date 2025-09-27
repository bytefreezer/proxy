package plugins

import (
	"bytes"
	"fmt"

	"github.com/bytedance/sonic"
)

// DataFormatter interface for different data format processors
type DataFormatter interface {
	// Format processes input data according to the specific format requirements
	// Returns normalized data ready for spooling
	Format(data []byte) ([]byte, error)

	// Name returns the format identifier
	Name() string
}

// JSONFormatter handles JSON data normalization - 1 document per message, converts to single line
type JSONFormatter struct{}

func (f *JSONFormatter) Name() string {
	return "json"
}

func (f *JSONFormatter) Format(data []byte) ([]byte, error) {
	// Trim whitespace
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, fmt.Errorf("empty JSON data")
	}

	// Parse and re-marshal to ensure compact format using Sonic
	// This handles both single-line and pretty-printed JSON (with newlines)
	var jsonObj interface{}
	if err := sonic.Unmarshal(data, &jsonObj); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}

	// Marshal back to compact format (no indentation) using Sonic
	compactJSON, err := sonic.Marshal(jsonObj)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal JSON: %w", err)
	}

	return compactJSON, nil
}

// NDJSONFormatter handles NDJSON data normalization
type NDJSONFormatter struct{}

func (f *NDJSONFormatter) Name() string {
	return "ndjson"
}

func (f *NDJSONFormatter) Format(data []byte) ([]byte, error) {
	// Trim whitespace
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, fmt.Errorf("empty NDJSON data")
	}

	// Split data into lines
	lines := bytes.Split(data, []byte("\n"))
	var normalizedLines [][]byte

	for i, line := range lines {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue // Skip empty lines
		}

		// Parse and normalize each line as JSON using Sonic
		var jsonObj interface{}
		if err := sonic.Unmarshal(line, &jsonObj); err != nil {
			return nil, fmt.Errorf("invalid JSON at line %d: %w", i+1, err)
		}

		// Re-marshal to ensure compact format using Sonic
		compactJSON, err := sonic.Marshal(jsonObj)
		if err != nil {
			return nil, fmt.Errorf("failed to normalize JSON at line %d: %w", i+1, err)
		}

		normalizedLines = append(normalizedLines, compactJSON)
	}

	if len(normalizedLines) == 0 {
		return nil, fmt.Errorf("no valid JSON lines found in NDJSON data")
	}

	// Join lines with newlines to create valid NDJSON
	return bytes.Join(normalizedLines, []byte("\n")), nil
}

// RawFormatter passes data through without modification
type RawFormatter struct{}

func (f *RawFormatter) Name() string {
	return "raw"
}

func (f *RawFormatter) Format(data []byte) ([]byte, error) {
	// Raw format - pass through without any modification
	return data, nil
}

// GenericFormatter handles unknown formats by removing newlines to ensure 1 document per line
type GenericFormatter struct {
	formatName string
}

func (f *GenericFormatter) Name() string {
	return f.formatName
}

func (f *GenericFormatter) Format(data []byte) ([]byte, error) {
	// Trim whitespace
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, fmt.Errorf("empty data")
	}

	// Remove newlines to ensure 1 document per line
	// Replace newlines with spaces to preserve readability
	result := bytes.ReplaceAll(data, []byte("\n"), []byte(" "))
	result = bytes.ReplaceAll(result, []byte("\r"), []byte(" "))

	// Clean up multiple spaces
	for bytes.Contains(result, []byte("  ")) {
		result = bytes.ReplaceAll(result, []byte("  "), []byte(" "))
	}

	return result, nil
}

// GetFormatter returns the appropriate formatter for the given format hint
func GetFormatter(formatHint string) DataFormatter {
	switch formatHint {
	case "json":
		return &JSONFormatter{}
	case "ndjson":
		return &NDJSONFormatter{}
	case "raw", "":
		return &RawFormatter{}
	default:
		// For unknown formats, use generic formatter that removes newlines
		return &GenericFormatter{formatName: formatHint}
	}
}
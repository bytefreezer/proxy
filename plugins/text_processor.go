package plugins

import (
	"fmt"
	"sync"
	"time"

	"github.com/bytedance/sonic"
)

// TextProcessor handles auto-detection and wrapping of text lines
type TextProcessor struct {
	// Cache for detected formats per dataset
	// Key: "tenantID:datasetID"
	// Value: DataFormatNDJSON or DataFormatText
	detectionCache map[string]string
	cacheMutex     sync.RWMutex
}

// NewTextProcessor creates a new text line processor
func NewTextProcessor() *TextProcessor {
	return &TextProcessor{
		detectionCache: make(map[string]string),
	}
}

// ProcessLine processes a single line based on data_format configuration
// Returns the processed line (wrapped if needed) and whether it was wrapped
func (tp *TextProcessor) ProcessLine(line []byte, tenantID, datasetID, dataFormat string) ([]byte, bool, error) {
	// Handle explicit modes first (fast path)
	switch dataFormat {
	case DataFormatNDJSON:
		// Explicit JSON mode - pass through as-is
		return line, false, nil

	case DataFormatText:
		// Explicit text mode - always wrap
		wrapped, err := tp.wrapTextLine(line)
		return wrapped, true, err

	case DataFormatAuto, "":
		// Auto-detect mode (default)
		return tp.processAutoDetect(line, tenantID, datasetID)

	default:
		// Unknown format - default to auto-detect
		return tp.processAutoDetect(line, tenantID, datasetID)
	}
}

// processAutoDetect handles auto-detection logic with caching
func (tp *TextProcessor) processAutoDetect(line []byte, tenantID, datasetID string) ([]byte, bool, error) {
	cacheKey := fmt.Sprintf("%s:%s", tenantID, datasetID)

	// Check cache first (read lock)
	tp.cacheMutex.RLock()
	cachedFormat, exists := tp.detectionCache[cacheKey]
	tp.cacheMutex.RUnlock()

	if exists {
		// Use cached detection result
		if cachedFormat == DataFormatNDJSON {
			return line, false, nil
		}
		wrapped, err := tp.wrapTextLine(line)
		return wrapped, true, err
	}

	// Cache miss - perform detection
	isJSON := tp.isValidJSON(line)

	// Update cache (write lock)
	tp.cacheMutex.Lock()
	if isJSON {
		tp.detectionCache[cacheKey] = DataFormatNDJSON
	} else {
		tp.detectionCache[cacheKey] = DataFormatText
	}
	tp.cacheMutex.Unlock()

	// Process based on detection result
	if isJSON {
		return line, false, nil
	}

	wrapped, err := tp.wrapTextLine(line)
	return wrapped, true, err
}

// isValidJSON checks if a line is valid JSON
func (tp *TextProcessor) isValidJSON(line []byte) bool {
	// Empty lines are not valid JSON
	if len(line) == 0 {
		return false
	}

	// Try to unmarshal - if it succeeds, it's valid JSON
	var temp interface{}
	err := sonic.Unmarshal(line, &temp)
	return err == nil
}

// wrapTextLine wraps a text line in a JSON envelope
func (tp *TextProcessor) wrapTextLine(line []byte) ([]byte, error) {
	envelope := map[string]interface{}{
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		"raw":       string(line),
	}

	wrapped, err := sonic.Marshal(envelope)
	if err != nil {
		return nil, fmt.Errorf("failed to wrap text line: %w", err)
	}

	// Add newline to maintain NDJSON format
	wrapped = append(wrapped, '\n')
	return wrapped, nil
}

// ClearCache clears detection cache for a specific dataset
// Used when dataset configuration changes
func (tp *TextProcessor) ClearCache(tenantID, datasetID string) {
	cacheKey := fmt.Sprintf("%s:%s", tenantID, datasetID)
	tp.cacheMutex.Lock()
	delete(tp.detectionCache, cacheKey)
	tp.cacheMutex.Unlock()
}

// ClearAllCache clears the entire detection cache
func (tp *TextProcessor) ClearAllCache() {
	tp.cacheMutex.Lock()
	tp.detectionCache = make(map[string]string)
	tp.cacheMutex.Unlock()
}

// GetCacheSize returns the number of cached detections
func (tp *TextProcessor) GetCacheSize() int {
	tp.cacheMutex.RLock()
	defer tp.cacheMutex.RUnlock()
	return len(tp.detectionCache)
}

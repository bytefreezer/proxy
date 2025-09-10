package syslog

import (
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// SyslogMessage represents a parsed syslog message
type SyslogMessage struct {
	Priority  int                    `json:"priority"`
	Facility  int                    `json:"facility"`
	Severity  int                    `json:"severity"`
	Timestamp time.Time              `json:"timestamp"`
	Hostname  string                 `json:"hostname"`
	Tag       string                 `json:"tag"`
	ProcessID string                 `json:"process_id,omitempty"`
	MessageID string                 `json:"message_id,omitempty"`
	Message   string                 `json:"message"`
	Version   int                    `json:"version,omitempty"`
	Raw       string                 `json:"raw"`
	Format    string                 `json:"format"` // "rfc3164" or "rfc5424"
	Metadata  map[string]interface{} `json:"metadata"`
}

// SyslogParser handles parsing of RFC3164 and RFC5424 syslog messages
type SyslogParser struct {
	rfc3164Pattern *regexp.Regexp
	rfc5424Pattern *regexp.Regexp
}

// NewSyslogParser creates a new syslog parser
func NewSyslogParser() *SyslogParser {
	return &SyslogParser{
		// RFC3164 pattern: <priority>Mmm dd hh:mm:ss hostname tag: message
		rfc3164Pattern: regexp.MustCompile(`^<(\d{1,3})>(\w{3}\s+\d{1,2}\s+\d{2}:\d{2}:\d{2})\s+(\S+)\s+([^:]+):\s*(.*)$`),

		// RFC5424 pattern: <priority>version timestamp hostname app-name procid msgid [structured-data] message
		rfc5424Pattern: regexp.MustCompile(`^<(\d{1,3})>(\d+)\s+(\S+)\s+(\S+)\s+(\S+)\s+(\S+)\s+(\S+)\s+(.*)$`),
	}
}

// Parse attempts to parse a syslog message, trying RFC5424 first then RFC3164
func (p *SyslogParser) Parse(data []byte) (*SyslogMessage, error) {
	raw := string(data)
	trimmed := strings.TrimSpace(raw)

	if len(trimmed) == 0 {
		return nil, errors.New("empty message")
	}

	// Try RFC5424 first
	if msg, err := p.parseRFC5424(trimmed); err == nil {
		msg.Raw = raw
		return msg, nil
	}

	// Try RFC3164
	if msg, err := p.parseRFC3164(trimmed); err == nil {
		msg.Raw = raw
		return msg, nil
	}

	// If neither works, create a basic message
	return &SyslogMessage{
		Priority:  13, // Default: user.notice
		Facility:  1,  // user
		Severity:  5,  // notice
		Timestamp: time.Now(),
		Hostname:  "unknown",
		Tag:       "unknown",
		Message:   trimmed,
		Raw:       raw,
		Format:    "unknown",
		Metadata:  make(map[string]interface{}),
	}, nil
}

// parseRFC3164 parses RFC3164 format syslog messages
func (p *SyslogParser) parseRFC3164(message string) (*SyslogMessage, error) {
	matches := p.rfc3164Pattern.FindStringSubmatch(message)
	if len(matches) < 6 {
		return nil, errors.New("not RFC3164 format")
	}

	priority, err := strconv.Atoi(matches[1])
	if err != nil {
		return nil, errors.New("invalid priority")
	}

	facility := priority >> 3
	severity := priority & 0x07

	// Parse timestamp (RFC3164 doesn't include year)
	timeStr := matches[2]
	currentYear := time.Now().Year()
	timestamp, err := time.Parse("Jan 2 15:04:05", timeStr)
	if err != nil {
		// Fallback to current time if parsing fails
		timestamp = time.Now()
	} else {
		// Add current year since RFC3164 doesn't include it
		timestamp = timestamp.AddDate(currentYear-1900, 0, 0)
	}

	// Extract tag and possible process ID
	tag := matches[4]
	processID := ""

	// Check for process ID in format: tag[pid]
	if pidMatch := regexp.MustCompile(`^([^[]+)\[(\d+)\]$`).FindStringSubmatch(tag); len(pidMatch) == 3 {
		tag = pidMatch[1]
		processID = pidMatch[2]
	}

	return &SyslogMessage{
		Priority:  priority,
		Facility:  facility,
		Severity:  severity,
		Timestamp: timestamp,
		Hostname:  matches[3],
		Tag:       tag,
		ProcessID: processID,
		Message:   matches[5],
		Format:    "rfc3164",
		Metadata: map[string]interface{}{
			"parsed_at": time.Now().Format(time.RFC3339),
			"parser":    "rfc3164",
		},
	}, nil
}

// parseRFC5424 parses RFC5424 format syslog messages
func (p *SyslogParser) parseRFC5424(message string) (*SyslogMessage, error) {
	matches := p.rfc5424Pattern.FindStringSubmatch(message)
	if len(matches) < 9 {
		return nil, errors.New("not RFC5424 format")
	}

	priority, err := strconv.Atoi(matches[1])
	if err != nil {
		return nil, errors.New("invalid priority")
	}

	version, err := strconv.Atoi(matches[2])
	if err != nil {
		return nil, errors.New("invalid version")
	}

	facility := priority >> 3
	severity := priority & 0x07

	// Parse timestamp
	timestamp, err := time.Parse(time.RFC3339, matches[3])
	if err != nil {
		// Try alternative formats
		if timestamp, err = time.Parse("2006-01-02T15:04:05.000Z", matches[3]); err != nil {
			timestamp = time.Now()
		}
	}

	hostname := matches[4]
	if hostname == "-" {
		hostname = "unknown"
	}

	appName := matches[5]
	if appName == "-" {
		appName = "unknown"
	}

	processID := matches[6]
	if processID == "-" {
		processID = ""
	}

	messageID := matches[7]
	if messageID == "-" {
		messageID = ""
	}

	// The rest is structured data + message
	remaining := matches[8]

	// TODO: Parse structured data properly
	// For now, treat everything as the message
	messageText := remaining

	return &SyslogMessage{
		Priority:  priority,
		Facility:  facility,
		Severity:  severity,
		Version:   version,
		Timestamp: timestamp,
		Hostname:  hostname,
		Tag:       appName,
		ProcessID: processID,
		MessageID: messageID,
		Message:   messageText,
		Format:    "rfc5424",
		Metadata: map[string]interface{}{
			"parsed_at": time.Now().Format(time.RFC3339),
			"parser":    "rfc5424",
		},
	}, nil
}

// ToJSON converts the syslog message to JSON bytes
func (m *SyslogMessage) ToJSON() ([]byte, error) {
	return json.Marshal(m)
}

// GetSeverityName returns the textual representation of severity
func (m *SyslogMessage) GetSeverityName() string {
	severityNames := []string{
		"emergency", "alert", "critical", "error",
		"warning", "notice", "informational", "debug",
	}

	if m.Severity >= 0 && m.Severity < len(severityNames) {
		return severityNames[m.Severity]
	}
	return "unknown"
}

// GetFacilityName returns the textual representation of facility
func (m *SyslogMessage) GetFacilityName() string {
	facilityNames := map[int]string{
		0: "kernel", 1: "user", 2: "mail", 3: "daemon",
		4: "security", 5: "syslogd", 6: "lpd", 7: "news",
		8: "uucp", 9: "cron", 10: "authpriv", 11: "ftp",
		12: "ntp", 13: "log_audit", 14: "log_alert", 15: "clock",
		16: "local0", 17: "local1", 18: "local2", 19: "local3",
		20: "local4", 21: "local5", 22: "local6", 23: "local7",
	}

	if name, ok := facilityNames[m.Facility]; ok {
		return name
	}
	return "unknown"
}

package syslog

import (
	"strings"
	"testing"
	"time"
)

func TestSyslogParser_ParseRFC3164(t *testing.T) {
	parser := NewSyslogParser()

	// Test RFC3164 format
	input := `<134>Oct 15 10:05:30 web01 nginx[1234]: 192.168.1.100 - - [10/Oct/2000:13:55:36 -0700] "GET /apache.gif HTTP/1.1" 200 2326`

	result, err := parser.Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	// Test basic syslog structure parsing
	if result.Format != "rfc3164" {
		t.Errorf("Expected format rfc3164, got %s", result.Format)
	}

	if result.Priority != 134 {
		t.Errorf("Expected priority 134, got %d", result.Priority)
	}

	if result.Facility != 16 { // 134 >> 3 = 16
		t.Errorf("Expected facility 16, got %d", result.Facility)
	}

	if result.Severity != 6 { // 134 & 0x07 = 6
		t.Errorf("Expected severity 6, got %d", result.Severity)
	}

	if result.Hostname != "web01" {
		t.Errorf("Expected hostname web01, got %s", result.Hostname)
	}

	if result.Tag != "nginx" {
		t.Errorf("Expected tag nginx, got %s", result.Tag)
	}

	if result.ProcessID != "1234" {
		t.Errorf("Expected process_id 1234, got %s", result.ProcessID)
	}

	// Verify original message is preserved
	expectedMessage := `192.168.1.100 - - [10/Oct/2000:13:55:36 -0700] "GET /apache.gif HTTP/1.1" 200 2326`
	if result.Message != expectedMessage {
		t.Errorf("Expected message %s, got %s", expectedMessage, result.Message)
	}

	// Verify raw message is preserved
	if result.Raw != input {
		t.Errorf("Expected raw message to be preserved")
	}
}

func TestSyslogParser_ParseRFC5424(t *testing.T) {
	parser := NewSyslogParser()

	// Test RFC5424 format
	input := `<134>1 2023-10-15T10:05:30.123Z web01 apache 5678 ACCESS - 192.168.1.100 - - [10/Oct/2000:13:55:36 -0700] "GET /secure/ HTTP/1.1" 401 2326`

	result, err := parser.Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	// Test basic syslog structure parsing
	if result.Format != "rfc5424" {
		t.Errorf("Expected format rfc5424, got %s", result.Format)
	}

	if result.Version != 1 {
		t.Errorf("Expected version 1, got %d", result.Version)
	}

	if result.Priority != 134 {
		t.Errorf("Expected priority 134, got %d", result.Priority)
	}

	if result.Hostname != "web01" {
		t.Errorf("Expected hostname web01, got %s", result.Hostname)
	}

	if result.Tag != "apache" {
		t.Errorf("Expected tag apache, got %s", result.Tag)
	}

	if result.ProcessID != "5678" {
		t.Errorf("Expected process_id 5678, got %s", result.ProcessID)
	}

	if result.MessageID != "ACCESS" {
		t.Errorf("Expected message_id ACCESS, got %s", result.MessageID)
	}

	// Verify original message content is preserved (after structured data)
	expectedMessage := `- 192.168.1.100 - - [10/Oct/2000:13:55:36 -0700] "GET /secure/ HTTP/1.1" 401 2326`
	if result.Message != expectedMessage {
		t.Errorf("Expected message %s, got %s", expectedMessage, result.Message)
	}

	// Verify raw message is preserved
	if result.Raw != input {
		t.Errorf("Expected raw message to be preserved")
	}
}

func TestSyslogParser_InvalidFormat(t *testing.T) {
	parser := NewSyslogParser()

	// Test invalid syslog format - should create basic message
	input := `This is not a valid syslog message`

	result, err := parser.Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse should not fail for invalid format: %v", err)
	}

	// Should create default message structure
	if result.Format != "unknown" {
		t.Errorf("Expected format unknown, got %s", result.Format)
	}

	if result.Priority != 13 { // Default: user.notice
		t.Errorf("Expected default priority 13, got %d", result.Priority)
	}

	if result.Message != input {
		t.Errorf("Expected message to be original input")
	}
}

func TestSyslogParser_EmptyMessage(t *testing.T) {
	parser := NewSyslogParser()

	_, err := parser.Parse([]byte(""))
	if err == nil {
		t.Error("Expected error for empty message")
	}
}

func TestSyslogParser_OnlySpaces(t *testing.T) {
	parser := NewSyslogParser()

	_, err := parser.Parse([]byte("   \n\t  "))
	if err == nil {
		t.Error("Expected error for whitespace-only message")
	}
}

func TestSyslogMessage_ToJSON(t *testing.T) {
	parser := NewSyslogParser()
	input := `<134>Oct 15 10:05:30 web01 nginx[1234]: Test message`

	result, err := parser.Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	jsonBytes, err := result.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON failed: %v", err)
	}

	if len(jsonBytes) == 0 {
		t.Error("Expected non-empty JSON output")
	}

	// Verify it's valid JSON by checking for key fields
	jsonStr := string(jsonBytes)
	expectedFields := []string{
		"priority", "facility", "severity", "hostname", "tag", 
		"message", "format", "raw", "metadata",
	}

	for _, field := range expectedFields {
		if !strings.Contains(jsonStr, field) {
			t.Errorf("Expected JSON to contain field: %s", field)
		}
	}
}

func TestSyslogMessage_GetSeverityName(t *testing.T) {
	msg := &SyslogMessage{Severity: 3}
	if msg.GetSeverityName() != "error" {
		t.Errorf("Expected severity name 'error', got %s", msg.GetSeverityName())
	}

	msg.Severity = 99 // Invalid
	if msg.GetSeverityName() != "unknown" {
		t.Errorf("Expected severity name 'unknown' for invalid severity")
	}
}

func TestSyslogMessage_GetFacilityName(t *testing.T) {
	msg := &SyslogMessage{Facility: 1}
	if msg.GetFacilityName() != "user" {
		t.Errorf("Expected facility name 'user', got %s", msg.GetFacilityName())
	}

	msg.Facility = 16
	if msg.GetFacilityName() != "local0" {
		t.Errorf("Expected facility name 'local0', got %s", msg.GetFacilityName())
	}

	msg.Facility = 99 // Invalid
	if msg.GetFacilityName() != "unknown" {
		t.Errorf("Expected facility name 'unknown' for invalid facility")
	}
}

func TestSyslogParser_TimestampParsing(t *testing.T) {
	parser := NewSyslogParser()

	// Test RFC3164 timestamp (no year)
	input := `<134>Oct 15 10:05:30 web01 nginx: Test message`

	result, err := parser.Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	// Should have a valid timestamp (year should be added)
	if result.Timestamp.IsZero() {
		t.Error("Expected valid timestamp")
	}

	// Year should be current year since RFC3164 doesn't include year
	currentYear := time.Now().Year()
	if result.Timestamp.Year() != currentYear {
		t.Errorf("Expected year %d, got %d", currentYear, result.Timestamp.Year())
	}
}

func TestSyslogParser_ProcessIDExtraction(t *testing.T) {
	parser := NewSyslogParser()

	tests := []struct {
		input       string
		expectedTag string
		expectedPID string
	}{
		{
			input:       `<134>Oct 15 10:05:30 web01 nginx[1234]: Test message`,
			expectedTag: "nginx",
			expectedPID: "1234",
		},
		{
			input:       `<134>Oct 15 10:05:30 web01 nginx: Test message`,
			expectedTag: "nginx",
			expectedPID: "",
		},
		{
			input:       `<134>Oct 15 10:05:30 web01 systemd[1]: Test message`,
			expectedTag: "systemd",
			expectedPID: "1",
		},
	}

	for _, test := range tests {
		result, err := parser.Parse([]byte(test.input))
		if err != nil {
			t.Fatalf("Parse failed for %s: %v", test.input, err)
		}

		if result.Tag != test.expectedTag {
			t.Errorf("Expected tag %s, got %s", test.expectedTag, result.Tag)
		}

		if result.ProcessID != test.expectedPID {
			t.Errorf("Expected process_id %s, got %s", test.expectedPID, result.ProcessID)
		}
	}
}

func BenchmarkSyslogParser_ParseRFC3164(b *testing.B) {
	parser := NewSyslogParser()
	input := []byte(`<134>Oct 15 10:05:30 web01 nginx[1234]: 192.168.1.100 - - [10/Oct/2000:13:55:36 -0700] "GET /apache.gif HTTP/1.1" 200 2326`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := parser.Parse(input)
		if err != nil {
			b.Fatalf("Parse failed: %v", err)
		}
	}
}

func BenchmarkSyslogParser_ParseRFC5424(b *testing.B) {
	parser := NewSyslogParser()
	input := []byte(`<134>1 2023-10-15T10:05:30.123Z web01 apache 5678 ACCESS - Test message content`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := parser.Parse(input)
		if err != nil {
			b.Fatalf("Parse failed: %v", err)
		}
	}
}
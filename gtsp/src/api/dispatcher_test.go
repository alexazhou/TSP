package api

import (
	"strings"
	"testing"
)

func TestTruncateForLog_ShortLineUnchanged(t *testing.T) {
	line := `{"id":"1","type":"result","result":{"file_path":"a.png"}}`
	got := truncateForLog(line)
	if got != line {
		t.Errorf("short line should be unchanged, got %q", got)
	}
}

func TestTruncateForLog_LongLineTruncated(t *testing.T) {
	// Simulate a read_image response with a large base64 payload.
	longBase64 := strings.Repeat("A", 10000)
	line := `{"id":"1","type":"result","result":{"base64":"` + longBase64 + `"}}`

	got := truncateForLog(line)
	if len(got) >= len(line) {
		t.Errorf("truncated line should be shorter than original (%d >= %d)", len(got), len(line))
	}
	if len(got) > maxLogLineLength+64 {
		t.Errorf("truncated line too long: %d", len(got))
	}
	if !strings.Contains(got, "...[truncated") {
		t.Errorf("expected truncation marker, got %q", got)
	}
	// The payload prefix must be preserved for debugging.
	if !strings.Contains(got[:len(got)-64], `"base64":"AAAA`) {
		t.Errorf("truncated line should keep base64 prefix")
	}
}

func TestTruncateForLog_ExactlyAtLimit(t *testing.T) {
	line := strings.Repeat("x", maxLogLineLength)
	got := truncateForLog(line)
	if got != line {
		t.Errorf("line at limit should be unchanged, got len=%d", len(got))
	}
}

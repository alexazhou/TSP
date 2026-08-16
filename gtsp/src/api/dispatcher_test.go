package api

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateForLog_ShortLineUnchanged(t *testing.T) {
	line := `{"id":"1","type":"result","result":{"file_path":"a.png"}}`
	got := truncateForLog(line)
	if got != line {
		t.Errorf("short line should be unchanged, got %q", got)
	}
}

func TestTruncateForLog_LongNonBase64LineTruncated(t *testing.T) {
	// A long line without a redactable field still gets hard-truncated.
	line := strings.Repeat("x", 10000)
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
}

func TestTruncateForLog_Utf8NotSplit(t *testing.T) {
	// A long line of multi-byte (Chinese) content must never end mid-rune.
	line := strings.Repeat("中文内容啊", 5000) // > 4096 bytes
	got := truncateForLog(line)
	if !utf8.ValidString(got) {
		t.Errorf("truncated line contains invalid UTF-8: %q", got[len(got)-20:])
	}
}

func TestTruncateForLog_ExactlyAtLimit(t *testing.T) {
	line := strings.Repeat("x", maxLogLineLength)
	got := truncateForLog(line)
	if got != line {
		t.Errorf("line at limit should be unchanged, got len=%d", len(got))
	}
}

// ───────────────────── SendResponse log/client split ─────────────────────

// redactableResult is a stand-in for a result type implementing api.LogRedactor.
type redactableResult struct {
	Name   string `json:"name"`
	Base64 string `json:"base64"`
}

func (r redactableResult) RedactForLog() interface{} {
	r.Base64 = r.Base64[:10] + "...[base64:N]"
	return r
}

// fakeSession returns a real SessionLogger so the log line can be inspected.
type fakeSession struct {
	Session
	logger *SessionLogger
}

func (s *fakeSession) GetLogger() *SessionLogger { return s.logger }

// fakeClient captures the payload written to the client.
type fakeClient struct {
	got interface{}
}

func (c *fakeClient) WriteJSON(v interface{}) error {
	c.got = v
	return nil
}

func TestSendResponse_LogsRedactedClientGetsFull(t *testing.T) {
	// Route session logs to a temp dir, and restore the global log target after.
	dir := t.TempDir()
	oldOut, oldFlags := log.Writer(), log.Flags()
	defer func() { log.SetOutput(oldOut); log.SetFlags(oldFlags) }()
	if err := InitLogger(dir); err != nil {
		t.Fatalf("InitLogger: %v", err)
	}
	sl, err := NewSessionLogger("sendresp")
	if err != nil {
		t.Fatalf("NewSessionLogger: %v", err)
	}
	defer sl.Close()

	d := NewDispatcher()
	session := &fakeSession{logger: sl}
	client := &fakeClient{}

	fullBase64 := strings.Repeat("A", 5000)
	d.SendResponse(session, client, "1", redactableResult{Name: "shot.png", Base64: fullBase64})

	// The client must receive the full, unmasked payload.
	resp, ok := client.got.(Response)
	if !ok {
		t.Fatalf("client got %T, want Response", client.got)
	}
	gotResult, ok := resp.Result.(redactableResult)
	if !ok {
		t.Fatalf("client result is %T, want redactableResult", resp.Result)
	}
	if gotResult.Base64 != fullBase64 {
		t.Errorf("client should get full base64 (%d chars), got %d chars", len(fullBase64), len(gotResult.Base64))
	}

	// The session log must contain the masked version, never the full payload.
	logData, err := os.ReadFile(filepath.Join(dir, "gt-agent-sendresp.log"))
	if err != nil {
		t.Fatalf("read session log: %v", err)
	}
	logStr := string(logData)
	if !strings.Contains(logStr, "...[base64") {
		t.Errorf("log should contain the mask marker, got: %.120s", logStr)
	}
	if strings.Contains(logStr, fullBase64) {
		t.Errorf("log leaked the full base64 payload")
	}
}

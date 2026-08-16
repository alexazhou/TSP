package unit_test

import (
	"gTSP/src/api"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

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
	api.Session
	logger *api.SessionLogger
}

func (s *fakeSession) GetLogger() *api.SessionLogger { return s.logger }

// fakeClient captures the payload written to the client.
type fakeClient struct {
	got interface{}
}

func (c *fakeClient) WriteJSON(v interface{}) error {
	c.got = v
	return nil
}

// newLogEnv sets up a session logger writing to a temp dir, and returns the
// dir plus the logger. The global log target is restored on test cleanup.
func newLogEnv(t *testing.T, sessionID string) (string, *api.SessionLogger) {
	t.Helper()
	dir := t.TempDir()
	oldOut, oldFlags := log.Writer(), log.Flags()
	t.Cleanup(func() { log.SetOutput(oldOut); log.SetFlags(oldFlags) })
	if err := api.InitLogger(dir); err != nil {
		t.Fatalf("InitLogger: %v", err)
	}
	sl, err := api.NewSessionLogger(sessionID)
	if err != nil {
		t.Fatalf("NewSessionLogger: %v", err)
	}
	return dir, sl
}

func readLog(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	return string(data)
}

func TestSendResponse_LogsRedactedClientGetsFull(t *testing.T) {
	dir, sl := newLogEnv(t, "sendresp")
	defer sl.Close()

	d := api.NewDispatcher()
	session := &fakeSession{logger: sl}
	client := &fakeClient{}

	fullBase64 := strings.Repeat("A", 5000)
	d.SendResponse(session, client, "1", redactableResult{Name: "shot.png", Base64: fullBase64})

	// The client must receive the full, unmasked payload.
	resp, ok := client.got.(api.Response)
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
	logStr := readLog(t, dir, "gt-agent-sendresp.log")
	if !strings.Contains(logStr, "...[base64") {
		t.Errorf("log should contain the mask marker, got: %.120s", logStr)
	}
	if strings.Contains(logStr, fullBase64) {
		t.Errorf("log leaked the full base64 payload")
	}
}

func TestSendResponse_TruncatesHugeResultLog(t *testing.T) {
	dir, sl := newLogEnv(t, "sendresp-trunc")
	defer sl.Close()

	d := api.NewDispatcher()
	session := &fakeSession{logger: sl}
	client := &fakeClient{}

	huge := strings.Repeat("x", 10000)
	d.SendResponse(session, client, "1", map[string]interface{}{"content": huge})

	// The client still gets the full payload.
	resp, ok := client.got.(api.Response)
	if !ok {
		t.Fatalf("client got %T, want Response", client.got)
	}
	if content := resp.Result.(map[string]interface{})["content"].(string); content != huge {
		t.Errorf("client should get full content, got %d chars", len(content))
	}

	// The log line is truncated by the dispatcher backstop.
	logStr := readLog(t, dir, "gt-agent-sendresp-trunc.log")
	if !strings.Contains(logStr, "...[truncated") {
		t.Errorf("log should contain the truncation marker, got: %.120s", logStr)
	}
	if strings.Contains(logStr, huge) {
		t.Errorf("log contains the full huge payload")
	}
}

func TestSendResponse_TruncationUtf8Safe(t *testing.T) {
	dir, sl := newLogEnv(t, "sendresp-utf8")
	defer sl.Close()

	d := api.NewDispatcher()
	session := &fakeSession{logger: sl}
	client := &fakeClient{}

	huge := strings.Repeat("中文内容啊", 5000) // > 4096 bytes, multi-byte content
	d.SendResponse(session, client, "1", map[string]interface{}{"content": huge})

	logStr := readLog(t, dir, "gt-agent-sendresp-utf8.log")
	if !utf8.ValidString(logStr) {
		t.Errorf("truncated log line contains invalid UTF-8 (split mid-rune)")
	}
	if strings.Contains(logStr, huge) {
		t.Errorf("log contains the full huge payload")
	}
}

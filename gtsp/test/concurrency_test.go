package handlers_test

import (
	"gTSP/src/api"
	"gTSP/src/tools"
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"
)

type mockWriter struct {
	responses chan []byte
}

func (m *mockWriter) Write(p []byte) (n int, err error) {
	// Split by newline if multiple responses are written
	lines := strings.Split(string(p), "\n")
	for _, line := range lines {
		if len(strings.TrimSpace(line)) > 0 {
			m.responses <- []byte(line)
		}
	}
	return len(p), nil
}

// initDispatcher initializes a dispatcher synchronously using a throwaway client
func initDispatcher(d *api.Dispatcher, s api.Session) {
	var buf bytes.Buffer
	client := api.NewStdioClient(nil, &buf)
	d.HandleRequest(s, client, []byte(`{"id":"_init","method":"initialize","input":{"protocolVersion":"0.3"}}`))
}

func TestDispatcher_Concurrency(t *testing.T) {
	reader, writer := io.Pipe()
	mockStdout := &mockWriter{responses: make(chan []byte, 10)}
	dispatcher := api.NewDispatcher()
	session := api.NewSession()
	stdioClient := api.NewStdioClient(reader, mockStdout)

	// Register a slow tool and a fast tool
	dispatcher.Register("slow_tool", func(s api.Session, params json.RawMessage) (interface{}, error) {
		time.Sleep(500 * time.Millisecond)
		return map[string]string{"result": "slow_done"}, nil
	})
	dispatcher.Register("fast_tool", func(s api.Session, params json.RawMessage) (interface{}, error) {
		return map[string]string{"result": "fast_done"}, nil
	})

	// Initialize before starting ServeStdio
	initDispatcher(dispatcher, session)

	// Start dispatcher in a goroutine
	go dispatcher.ServeStdio(session, stdioClient)

	// Send slow request then fast request immediately
	go func() {
		writer.Write([]byte(`{"id": "slow", "method": "tool", "tool": "slow_tool", "input": {}}` + "\n"))
		time.Sleep(50 * time.Millisecond) // Ensure order in stdin
		writer.Write([]byte(`{"id": "fast", "method": "tool", "tool": "fast_tool", "input": {}}` + "\n"))
		time.Sleep(100 * time.Millisecond)
		writer.Close()
	}()

	var firstID string
	var secondID string

	// Collect first response
	select {
	case data := <-mockStdout.responses:
		var resp api.Response
		json.Unmarshal(data, &resp)
		firstID = resp.ID
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for first response")
	}

	// Collect second response
	select {
	case data := <-mockStdout.responses:
		var resp api.Response
		json.Unmarshal(data, &resp)
		secondID = resp.ID
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for second response")
	}

	// In a serial model, "slow" would be first.
	// In a concurrent model, "fast" should be first because "slow" takes 500ms.
	if firstID != "fast" {
		t.Errorf("Expected 'fast' to complete first, but got %q first", firstID)
	}
	if secondID != "slow" {
		t.Errorf("Expected 'slow' to complete second, but got %q second", secondID)
	}
}

func TestExecuteBash_Concurrency(t *testing.T) {
	reader, writer := io.Pipe()
	mockStdout := &mockWriter{responses: make(chan []byte, 10)}
	dispatcher := api.NewDispatcher()
	session := api.NewSession()
	stdioClient := api.NewStdioClient(reader, mockStdout)

	// Register real bash tool
	tools.RegisterAll(dispatcher)

	// Initialize before starting ServeStdio
	initDispatcher(dispatcher, session)

	go dispatcher.ServeStdio(session, stdioClient)

	// Send a long sleep and a quick echo
	go func() {
		writer.Write([]byte(`{"id": "bash_slow", "method": "tool", "tool": "execute_bash", "input": {"command": "sleep 0.5"}}` + "\n"))
		time.Sleep(50 * time.Millisecond)
		writer.Write([]byte(`{"id": "bash_fast", "method": "tool", "tool": "execute_bash", "input": {"command": "echo fast"}}` + "\n"))
		time.Sleep(100 * time.Millisecond)
		writer.Close()
	}()

	// We expect bash_fast to return its result while bash_slow is still sleeping
	var firstID string

	select {
	case data := <-mockStdout.responses:
		var resp api.Response
		json.Unmarshal(data, &resp)
		firstID = resp.ID
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout")
	}

	if firstID != "bash_fast" {
		t.Errorf("Expected bash_fast to finish first, but got %q", firstID)
	}
}

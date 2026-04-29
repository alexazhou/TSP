package handlers_test

import (
	"gTSP/src/api"
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestDispatcher_SendEvent(t *testing.T) {
	stdout := &bytes.Buffer{}
	client := api.NewStdioClient(nil, stdout)

	eventData := map[string]string{"foo": "bar"}
	client.WriteJSON(map[string]interface{}{
		"type":   "event",
		"result": eventData,
	})

	var resp api.Response
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.Type != "event" {
		t.Errorf("expected type 'event', got %q", resp.Type)
	}
}

func TestDispatcher_sendResponse(t *testing.T) {
	stdout := &bytes.Buffer{}
	dispatcher := api.NewDispatcher()
	client := api.NewStdioClient(nil, stdout)
	session := api.NewSession()

	dispatcher.Register("test", func(s api.Session, params json.RawMessage) (interface{}, error) {
		return map[string]string{"status": "ok"}, nil
	})

	// Initialize first (synchronous call)
	dispatcher.HandleRequest(session, client, []byte(`{"id":"0","method":"initialize","input":{"protocolVersion":"0.3"}}`))
	stdout.Reset() // discard initialize response

	// Now invoke the tool
	dispatcher.HandleRequest(session, client, []byte(`{"id":"123","method":"tool","tool":"test","input":{}}`))

	var resp api.Response
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.ID != "123" {
		t.Errorf("expected ID 123, got %q", resp.ID)
	}
	if resp.Type != "result" {
		t.Errorf("expected type 'result', got %q", resp.Type)
	}
}

func TestDispatcher_Schemas(t *testing.T) {
	dispatcher := api.NewDispatcher()
	schema := api.ToolDefinition{
		Name:        "test_tool",
		Description: "test desc",
		InputSchema: map[string]interface{}{"type": "object"},
	}

	dispatcher.RegisterWithSchema("test_tool", func(s api.Session, params json.RawMessage) (interface{}, error) {
		return nil, nil
	}, schema)

	schemas := dispatcher.GetSchemas()
	if len(schemas) != 1 {
		t.Errorf("expected 1 schema, got %d", len(schemas))
	}
	if _, ok := schemas["test_tool"]; !ok {
		t.Error("expected schema for 'test_tool' not found")
	}
}

func TestInitialize(t *testing.T) {
	stdout := &bytes.Buffer{}
	dispatcher := api.NewDispatcher()
	client := api.NewStdioClient(nil, stdout)
	session := api.NewSession()

	dispatcher.HandleRequest(session, client, []byte(`{"id":"1","method":"initialize","input":{"protocolVersion":"0.3","clientInfo":{"name":"test"}}}`))

	var resp api.Response
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if resp.ID != "1" {
		t.Errorf("expected ID '1', got %q", resp.ID)
	}
	if resp.Type != "result" {
		t.Errorf("expected type 'result', got %q", resp.Type)
	}
	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got %T", resp.Result)
	}
	if _, ok := result["protocolVersion"]; !ok {
		t.Error("missing protocolVersion in initialize result")
	}
	if _, ok := result["serverInfo"]; !ok {
		t.Error("missing serverInfo in initialize result")
	}
	if _, ok := result["capabilities"]; !ok {
		t.Error("missing capabilities in initialize result")
	}
	if _, ok := result["workdir"]; !ok {
		t.Error("missing workdir in initialize result")
	}
}

func TestShutdown(t *testing.T) {
	stdout := &bytes.Buffer{}
	dispatcher := api.NewDispatcher()
	client := api.NewStdioClient(nil, stdout)
	session := api.NewSession()

	// Initialize
	dispatcher.HandleRequest(session, client, []byte(`{"id":"1","method":"initialize","input":{"protocolVersion":"0.3"}}`))
	stdout.Reset()

	// Shutdown
	dispatcher.HandleRequest(session, client, []byte(`{"id":"2","method":"shutdown","input":{}}`))

	var shutdownResp api.Response
	if err := json.Unmarshal(stdout.Bytes(), &shutdownResp); err != nil {
		t.Fatalf("failed to unmarshal shutdown response: %v", err)
	}
	if shutdownResp.Type != "result" {
		t.Errorf("expected type 'result', got %q", shutdownResp.Type)
	}
	stdout.Reset()

	// Subsequent tool request should return shutting_down error
	dispatcher.Register("test", func(s api.Session, params json.RawMessage) (interface{}, error) {
		return map[string]string{"ok": "yes"}, nil
	})
	dispatcher.HandleRequest(session, client, []byte(`{"id":"3","method":"tool","tool":"test","input":{}}`))

	var errResp api.ErrorResponse
	if err := json.Unmarshal(stdout.Bytes(), &errResp); err != nil {
		t.Fatalf("failed to unmarshal error response: %v", err)
	}
	if errResp.Code != api.ErrShuttingDown {
		t.Errorf("expected %q, got %q", api.ErrShuttingDown, errResp.Code)
	}
}

func TestNotInitialized(t *testing.T) {
	stdout := &bytes.Buffer{}
	dispatcher := api.NewDispatcher()
	client := api.NewStdioClient(nil, stdout)
	session := api.NewSession()

	dispatcher.Register("test", func(s api.Session, params json.RawMessage) (interface{}, error) {
		return map[string]string{"ok": "yes"}, nil
	})

	dispatcher.HandleRequest(session, client, []byte(`{"id":"1","method":"tool","tool":"test","input":{}}`))

	var errResp api.ErrorResponse
	if err := json.Unmarshal(stdout.Bytes(), &errResp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if errResp.Code != api.ErrNotInitialized {
		t.Errorf("expected %q, got %q", api.ErrNotInitialized, errResp.Code)
	}
}

func TestParseError(t *testing.T) {
	stdout := &bytes.Buffer{}
	dispatcher := api.NewDispatcher()
	client := api.NewStdioClient(nil, stdout)
	session := api.NewSession()

	dispatcher.HandleRequest(session, client, []byte(`not valid json`))

	var errResp api.ErrorResponse
	if err := json.Unmarshal(stdout.Bytes(), &errResp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if errResp.ID != nil {
		t.Errorf("expected null id for parse error, got %v", *errResp.ID)
	}
	if errResp.Code != api.ErrParseError {
		t.Errorf("expected %q, got %q", api.ErrParseError, errResp.Code)
	}
}

func TestToolNotFound(t *testing.T) {
	stdout := &bytes.Buffer{}
	dispatcher := api.NewDispatcher()
	client := api.NewStdioClient(nil, stdout)
	session := api.NewSession()

	// Initialize first
	dispatcher.HandleRequest(session, client, []byte(`{"id":"0","method":"initialize","input":{"protocolVersion":"0.3"}}`))
	stdout.Reset()

	dispatcher.HandleRequest(session, client, []byte(`{"id":"1","method":"tool","tool":"nonexistent","input":{}}`))

	var errResp api.ErrorResponse
	if err := json.Unmarshal(stdout.Bytes(), &errResp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if errResp.Code != api.ErrToolNotFound {
		t.Errorf("expected %q, got %q", api.ErrToolNotFound, errResp.Code)
	}
	errMsg, _ := errResp.Error.(string)
	if !strings.Contains(errMsg, "nonexistent") {
		t.Errorf("expected error message to contain tool name, got %q", errMsg)
	}
}

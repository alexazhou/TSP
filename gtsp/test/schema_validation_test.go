package handlers_test

import (
	"gTSP/src/api"
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// makeDispatcherWithSchema creates a dispatcher with a test tool that has
// a schema including required fields and additionalProperties: false.
func makeDispatcherWithSchema() *api.Dispatcher {
	d := api.NewDispatcher()
	schema := api.ToolDefinition{
		Name:        "test",
		Description: "test tool",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{
					"type":        "string",
					"description": "A name.",
				},
				"count": map[string]interface{}{
					"type":        "integer",
					"description": "A count.",
				},
			},
			"required":             []string{"name"},
			"additionalProperties": false,
		},
	}
	d.RegisterWithSchema("test", func(s api.Session, params json.RawMessage) (interface{}, error) {
		return map[string]string{"ok": "yes"}, nil
	}, schema)
	return d
}

// initializeAndDiscard sends an initialize and resets output.
func initializeAndDiscard(t *testing.T, d *api.Dispatcher, client *api.StdioClient, stdout *bytes.Buffer) api.Session {
	t.Helper()
	session := api.NewSession()
	d.HandleRequest(session, client, []byte(`{"id":"0","method":"initialize","input":{"protocolVersion":"0.3"}}`))
	stdout.Reset()
	return session
}

func TestSchemaValidation_ValidParams(t *testing.T) {
	stdout := &bytes.Buffer{}
	d := makeDispatcherWithSchema()
	client := api.NewStdioClient(nil, stdout)
	session := initializeAndDiscard(t, d, client, stdout)

	d.HandleRequest(session, client, []byte(`{"id":"1","method":"tool","tool":"test","input":{"name":"hello"}}`))

	var resp api.Response
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if resp.Type != "result" {
		t.Errorf("expected 'result', got %q", resp.Type)
	}
}

func TestSchemaValidation_ExtraParams(t *testing.T) {
	stdout := &bytes.Buffer{}
	d := makeDispatcherWithSchema()
	client := api.NewStdioClient(nil, stdout)
	session := initializeAndDiscard(t, d, client, stdout)

	d.HandleRequest(session, client, []byte(`{"id":"1","method":"tool","tool":"test","input":{"name":"hello","extra":"oops"}}`))

	var errResp api.ErrorResponse
	if err := json.Unmarshal(stdout.Bytes(), &errResp); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if errResp.Code != api.ErrInvalidParams {
		t.Errorf("expected %q, got %q", api.ErrInvalidParams, errResp.Code)
	}
	errMsg, _ := errResp.Error.(string)
	if !strings.Contains(errMsg, "extra") {
		t.Errorf("expected error message to mention 'extra', got %q", errMsg)
	}
}

func TestSchemaValidation_MissingRequired(t *testing.T) {
	stdout := &bytes.Buffer{}
	d := makeDispatcherWithSchema()
	client := api.NewStdioClient(nil, stdout)
	session := initializeAndDiscard(t, d, client, stdout)

	d.HandleRequest(session, client, []byte(`{"id":"1","method":"tool","tool":"test","input":{"count":5}}`))

	var errResp api.ErrorResponse
	if err := json.Unmarshal(stdout.Bytes(), &errResp); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if errResp.Code != api.ErrInvalidParams {
		t.Errorf("expected %q, got %q", api.ErrInvalidParams, errResp.Code)
	}
	errMsg, _ := errResp.Error.(string)
	if !strings.Contains(errMsg, "name") {
		t.Errorf("expected error message to mention 'name', got %q", errMsg)
	}
}

func TestSchemaValidation_EmptyParams_WhenRequired(t *testing.T) {
	stdout := &bytes.Buffer{}
	d := makeDispatcherWithSchema()
	client := api.NewStdioClient(nil, stdout)
	session := initializeAndDiscard(t, d, client, stdout)

	d.HandleRequest(session, client, []byte(`{"id":"1","method":"tool","tool":"test","input":{}}`))

	var errResp api.ErrorResponse
	if err := json.Unmarshal(stdout.Bytes(), &errResp); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if errResp.Code != api.ErrInvalidParams {
		t.Errorf("expected %q, got %q", api.ErrInvalidParams, errResp.Code)
	}
}

func TestSchemaValidation_NoSchema(t *testing.T) {
	// Tools registered without a schema should not fail validation.
	stdout := &bytes.Buffer{}
	d := api.NewDispatcher()
	d.Register("no_schema_tool", func(s api.Session, params json.RawMessage) (interface{}, error) {
		return map[string]string{"ok": "yes"}, nil
	})
	client := api.NewStdioClient(nil, stdout)
	session := initializeAndDiscard(t, d, client, stdout)

	d.HandleRequest(session, client, []byte(`{"id":"1","method":"tool","tool":"no_schema_tool","input":{"anything":"goes"}}`))

	var resp api.Response
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if resp.Type != "result" {
		t.Errorf("expected 'result', got %q", resp.Type)
	}
}

func TestSchemaValidation_BadJSON(t *testing.T) {
	stdout := &bytes.Buffer{}
	d := makeDispatcherWithSchema()
	client := api.NewStdioClient(nil, stdout)
	session := initializeAndDiscard(t, d, client, stdout)

	d.HandleRequest(session, client, []byte(`{"id":"1","method":"tool","tool":"test","input":not-valid-json}`))

	var errResp api.ErrorResponse
	if err := json.Unmarshal(stdout.Bytes(), &errResp); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if errResp.Code != api.ErrParseError {
		t.Errorf("expected %q, got %q", api.ErrParseError, errResp.Code)
	}
}

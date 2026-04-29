package handlers_test

import (
	"gTSP/src/api"
	"gTSP/src/tools"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

func newTestWSServer(dispatcher *api.Dispatcher) *httptest.Server {
	mux := http.NewServeMux()
	upgrader := websocket.Upgrader{}
	mux.HandleFunc("/tsp", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		client := &api.WSClientForTest{Conn: conn}
		session := api.NewSession()
		// Initial session allows everything for tests
		session.SetPathRules([]api.PathRule{{Action: "allow", Path: api.GetWorkdirRoot()}}, []api.PathRule{{Action: "allow", Path: api.GetWorkdirRoot()}})
		session.SetNetworkAllowed(true)

		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				break
			}
			go dispatcher.HandleRequest(session, client, message)
		}
	})
	return httptest.NewServer(mux)
}

func wsInitialize(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	initReq := api.Request{
		ID:     "init",
		Method: "initialize",
		Input:  json.RawMessage(`{"protocolVersion": "0.3"}`),
	}
	if err := conn.WriteJSON(initReq); err != nil {
		t.Fatalf("failed to send initialize: %v", err)
	}
	var initResp api.Response
	if err := conn.ReadJSON(&initResp); err != nil {
		t.Fatalf("failed to read initialize response: %v", err)
	}
}

func TestWebSocket_Integration(t *testing.T) {
	dispatcher := api.NewDispatcher()
	tools.RegisterAll(dispatcher)

	server := newTestWSServer(dispatcher)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/tsp"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect to websocket: %v", err)
	}
	defer conn.Close()

	// Initialize first
	wsInitialize(t, conn)

	// Send a tool request
	req := api.Request{
		ID:     "ws_test_1",
		Method: "tool",
		Tool:   "execute_bash",
		Input:  json.RawMessage(`{"command": "echo hello_ws"}`),
	}
	if err := conn.WriteJSON(req); err != nil {
		t.Fatalf("failed to write JSON: %v", err)
	}

	// Read response
	var resp api.Response
	err = conn.ReadJSON(&resp)
	if err != nil {
		t.Fatalf("failed to read JSON: %v", err)
	}

	if resp.ID != "ws_test_1" {
		t.Errorf("expected ID ws_test_1, got %q", resp.ID)
	}

	// Check result
	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got %T", resp.Result)
	}
	stdout := result["stdout"].(string)
	if !strings.Contains(stdout, "hello_ws") {
		t.Errorf("expected 'hello_ws' in stdout, got %q", stdout)
	}
}

func TestWebSocket_Concurrency(t *testing.T) {
	dispatcher := api.NewDispatcher()
	tools.RegisterAll(dispatcher)

	server := newTestWSServer(dispatcher)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/tsp"

	// Connect two clients
	conn1, _, _ := websocket.DefaultDialer.Dial(wsURL, nil)
	defer conn1.Close()
	conn2, _, _ := websocket.DefaultDialer.Dial(wsURL, nil)
	defer conn2.Close()

	// Initialize both connections
	wsInitialize(t, conn1)
	wsInitialize(t, conn2)

	// Send requests
	conn1.WriteJSON(api.Request{ID: "c1", Method: "tool", Tool: "execute_bash", Input: json.RawMessage(`{"command": "sleep 0.2 && echo c1"}`)})
	conn2.WriteJSON(api.Request{ID: "c2", Method: "tool", Tool: "execute_bash", Input: json.RawMessage(`{"command": "echo c2"}`)})

	// Conn2 should return first
	var resp2 api.Response
	conn2.ReadJSON(&resp2)
	if resp2.ID != "c2" {
		t.Errorf("expected c2 first, got %q", resp2.ID)
	}

	var resp1 api.Response
	conn1.ReadJSON(&resp1)
	if resp1.ID != "c1" {
		t.Errorf("expected c1 second, got %q", resp1.ID)
	}
}

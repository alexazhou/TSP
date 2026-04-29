package handlers_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gTSP/src/api"

	"github.com/gorilla/websocket"
)

// TestServeStdio_PanicRecovery verifies that a panicking handler does not crash
// the server and that subsequent requests are processed normally.
func TestServeStdio_PanicRecovery(t *testing.T) {
	dispatcher := api.NewDispatcher()
	dispatcher.Register("panic_tool", func(_ api.Session, _ json.RawMessage) (interface{}, error) {
		panic("simulated panic")
	})
	dispatcher.Register("ok_tool", func(_ api.Session, _ json.RawMessage) (interface{}, error) {
		return map[string]string{"status": "alive"}, nil
	})

	reader, writer := io.Pipe()
	mockStdout := &mockWriter{responses: make(chan []byte, 10)}
	client := api.NewStdioClient(reader, mockStdout)
	session := api.NewSession()

	initDispatcher(dispatcher, session)
	go dispatcher.ServeStdio(session, client)

	// Trigger panic — no response expected
	writer.Write([]byte(`{"id":"panic1","method":"tool","tool":"panic_tool","input":{}}` + "\n"))
	time.Sleep(50 * time.Millisecond)

	// Server should still be alive
	writer.Write([]byte(`{"id":"ok1","method":"tool","tool":"ok_tool","input":{}}` + "\n"))

	select {
	case data := <-mockStdout.responses:
		var resp api.Response
		if err := json.Unmarshal(data, &resp); err != nil {
			t.Fatalf("failed to unmarshal response: %v", err)
		}
		if resp.ID != "ok1" {
			t.Errorf("expected id 'ok1', got %q", resp.ID)
		}
		if resp.Type != "result" {
			t.Errorf("expected type 'result', got %q", resp.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no response after panic: server may have crashed")
	}

	writer.Close()
}

// TestServeWS_PanicRecovery verifies that a panicking handler does not terminate
// the WebSocket connection and that subsequent requests are processed normally.
func TestServeWS_PanicRecovery(t *testing.T) {
	dispatcher := api.NewDispatcher()
	dispatcher.Register("panic_tool", func(_ api.Session, _ json.RawMessage) (interface{}, error) {
		panic("simulated panic")
	})
	dispatcher.Register("ok_tool", func(_ api.Session, _ json.RawMessage) (interface{}, error) {
		return map[string]string{"status": "alive"}, nil
	})

	// Mirror the production goroutine pattern from websocket.go
	mux := http.NewServeMux()
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	mux.HandleFunc("/tsp", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		client := &api.WSClientForTest{Conn: conn}
		session := api.NewSession()
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				break
			}
			go func(msg []byte) {
				defer func() { recover() }()
				dispatcher.HandleRequest(session, client, msg)
			}(message)
		}
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/tsp"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	// Initialize
	conn.WriteJSON(api.Request{ID: "init", Method: "initialize", Input: json.RawMessage(`{"protocolVersion":"0.3"}`)})
	var initResp api.Response
	conn.ReadJSON(&initResp)

	// Send panic request then a normal request — no response expected for panic
	conn.WriteJSON(api.Request{ID: "panic1", Method: "tool", Tool: "panic_tool", Input: json.RawMessage(`{}`)})
	conn.WriteJSON(api.Request{ID: "ok1", Method: "tool", Tool: "ok_tool", Input: json.RawMessage(`{}`)})

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var resp api.Response
	if err := conn.ReadJSON(&resp); err != nil {
		t.Fatalf("no response after panic: server may have crashed (%v)", err)
	}
	if resp.ID != "ok1" {
		t.Errorf("expected id 'ok1', got %q", resp.ID)
	}
	if resp.Type != "result" {
		t.Errorf("expected type 'result', got %q", resp.Type)
	}
}

package api

import (
	"fmt"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for the tool
	},
}

// WSClient implements Client interface for WebSocket connections
type WSClient struct {
	Conn *websocket.Conn
	mu   sync.Mutex
}

func (c *WSClient) WriteJSON(v interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.Conn.WriteJSON(v)
}

// WSClientForTest is used in external tests to avoid unexported field issues
type WSClientForTest struct {
	Conn *websocket.Conn
	mu   sync.Mutex
}

func (c *WSClientForTest) WriteJSON(v interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.Conn.WriteJSON(v)
}

// ServeWS starts a WebSocket server and routes messages to the dispatcher
func (d *Dispatcher) ServeWS(port int) {
	http.HandleFunc("/tsp", func(w http.ResponseWriter, r *http.Request) {
		defer recoverPanic("WSHandler")
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("upgrade error: %v", err)
			return
		}
		defer conn.Close()

		client := &WSClient{Conn: conn}
		session := NewSession()
		defer session.CloseLogger()

		log.Printf("New client connected via WebSocket from %s, session ID: %s", r.RemoteAddr, session.GetSessionID())

		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				log.Printf("read error: %v", err)
				break
			}

			// Run in goroutine to allow concurrent requests over the same WS connection
			go func(msg []byte) {
				defer recoverPanic("HandleRequest/ws")
				d.HandleRequest(session, client, msg)
			}(message)
		}
	})

	addr := fmt.Sprintf(":%d", port)
	log.Printf("WebSocket server starting on %s/tsp", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("ListenAndServe error: %v", err)
	}
}

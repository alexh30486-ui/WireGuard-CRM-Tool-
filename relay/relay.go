package main

// Fixed pairing logic: the first peer now waits for its partner
// instead of returning (and closing the WebSocket).

import (
	"context"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type SessionPair struct {
	mu        sync.Mutex
	client    *websocket.Conn
	connector *websocket.Conn
	timer     *time.Timer
	ready     chan struct{} // closed when both peers are present
}

var (
	sessions   = make(map[string]*SessionPair)
	sessionsMu sync.Mutex
)

func main() {
	http.HandleFunc("/ws/relay", HandleRelay)
	log.Println("Relay server listening on :8080...")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func HandleRelay(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session")
	token := r.URL.Query().Get("token")
	role := r.URL.Query().Get("role") // "client" or "connector"

	if !verifyTokenWithControlPlane(sessionID, token) {
		http.Error(w, "Unauthorized session token", http.StatusUnauthorized)
		return
	}

	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("WebSocket upgrade failed:", err)
		return
	}
	defer ws.Close()

	sessionsMu.Lock()
	pair, exists := sessions[sessionID]
	if !exists {
		pair = &SessionPair{
			ready: make(chan struct{}),
		}
		pair.timer = time.AfterFunc(30*time.Second, func() {
			sessionsMu.Lock()
			defer sessionsMu.Unlock()
			if p, ok := sessions[sessionID]; ok {
				p.mu.Lock()
				if p.client != nil {
					p.client.Close()
				}
				if p.connector != nil {
					p.connector.Close()
				}
				p.mu.Unlock()
				delete(sessions, sessionID)
				log.Printf("Session %s timed out waiting for partner.", sessionID)
			}
		})
		sessions[sessionID] = pair
	}
	sessionsMu.Unlock()

	pair.mu.Lock()
	if role == "client" {
		if pair.client != nil {
			pair.mu.Unlock()
			log.Printf("Session %s already has a client, rejecting", sessionID)
			return
		}
		pair.client = ws
	} else {
		if pair.connector != nil {
			pair.mu.Unlock()
			log.Printf("Session %s already has a connector, rejecting", sessionID)
			return
		}
		pair.connector = ws
	}

	bothReady := pair.client != nil && pair.connector != nil
	pair.mu.Unlock()

	if bothReady {
		// Second peer arrived — wake the first peer and start proxying
		pair.mu.Lock()
		if pair.timer != nil {
			pair.timer.Stop()
		}
		close(pair.ready)
		pair.mu.Unlock()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		go proxyStream(ctx, pair.client, pair.connector, cancel)
		go proxyStream(ctx, pair.connector, pair.client, cancel)

		<-ctx.Done()

		sessionsMu.Lock()
		delete(sessions, sessionID)
		sessionsMu.Unlock()
		log.Printf("Session %s completed and torn down.", sessionID)
		return
	}

	// First peer: wait until the partner arrives or timeout
	select {
	case <-pair.ready:
		// Partner arrived — the second peer already started the proxies.
		// Just keep this connection alive until the session ends.
		<-make(chan struct{}) // block forever (connection will be closed by proxy)
	case <-time.After(35 * time.Second):
		log.Printf("Session %s: first peer timed out waiting", sessionID)
	}
}

func proxyStream(ctx context.Context, src, dst *websocket.Conn, cancel context.CancelFunc) {
	defer cancel()
	for {
		_, msg, err := src.ReadMessage()
		if err != nil {
			return
		}
		if err := dst.WriteMessage(websocket.BinaryMessage, msg); err != nil {
			return
		}
	}
}

// STUB — accepts everything. Do not run this reachable from an untrusted network.
func verifyTokenWithControlPlane(sessionID, token string) bool {
	return true
}

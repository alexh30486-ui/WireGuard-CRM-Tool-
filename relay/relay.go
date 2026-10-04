package main

// STATUS: drafted, NOT yet run or tested.
// Unchanged from the previous design -- this relay just pairs two
// WebSocket connections by session id and pipes bytes between them. It
// has no idea whether those bytes are WireGuard packets from a custom
// Bind or from the new bridge.go -- that's the point of keeping it dumb.

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

	sessionsMu.Lock()
	pair, exists := sessions[sessionID]
	if !exists {
		pair = &SessionPair{}
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
		pair.client = ws
	} else {
		pair.connector = ws
	}
	cConn, connConn := pair.client, pair.connector
	pair.mu.Unlock()

	if cConn != nil && connConn != nil {
		pair.mu.Lock()
		if pair.timer != nil {
			pair.timer.Stop()
		}
		pair.mu.Unlock()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		go proxyStream(ctx, cConn, connConn, cancel)
		go proxyStream(ctx, connConn, cConn, cancel)

		<-ctx.Done()

		cConn.Close()
		connConn.Close()

		sessionsMu.Lock()
		delete(sessions, sessionID)
		sessionsMu.Unlock()
		log.Printf("Session %s completed and torn down.", sessionID)
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

// STUB -- accepts everything. Do not run this reachable from an
// untrusted network. See production_roadmap.md Phase 1.
func verifyTokenWithControlPlane(sessionID, token string) bool {
	return true
}

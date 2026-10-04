package main

// STATUS: drafted, NOT yet run or tested.
//
// This replaces the old connector.go / client.go entirely. Those files
// re-implemented WireGuard's own socket layer as a custom conn.Bind
// against wireguard-go's internal library API -- an API that changes
// between versions, which is what caused the repeated rebuilds. Bugs in
// that layer were also invisible to normal tools: you can't tcpdump a
// custom in-process Bind.
//
// This program does ONE thing: forward raw UDP datagrams between a local
// port and the relay over a WebSocket. It has no idea what WireGuard is,
// no crypto, no key handling. Real WireGuard (the actual `wg`/`wg-quick`
// tool, or the official wireguard-go binary, both unmodified) runs on
// each side and is configured to send its already-encrypted packets to
// this bridge's local port instead of a real network interface. Because
// this only moves bytes, you can verify it in isolation with `tcpdump -i
// lo udp port <port>` -- independent of whether WireGuard itself is
// configured correctly.
//
// One process of this runs on the connector side, one on the client
// side. Same binary, different flags.

import (
	"flag"
	"log"
	"net"
	"net/url"
	"time"

	"github.com/gorilla/websocket"
)

func main() {
	relayURL := flag.String("relay", "ws://localhost:8080/ws/relay", "relay base URL")
	session := flag.String("session", "test1", "session id (must match on both sides)")
	token := flag.String("token", "mvp_test_token_123", "relay auth token")
	role := flag.String("role", "", "client or connector")
	localPort := flag.Int("local-port", 0, "local UDP port your WireGuard peer 'Endpoint' points at")
	flag.Parse()

	if *role != "client" && *role != "connector" {
		log.Fatal("must set -role=client or -role=connector")
	}
	if *localPort == 0 {
		log.Fatal("must set -local-port (the port your wg-config Endpoint= points at)")
	}

	// Local UDP socket: WireGuard's own process sends its encrypted
	// packets here (configure the peer Endpoint as 127.0.0.1:<local-port>
	// in your wg-config file) and expects replies from the same address.
	udpAddr := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: *localPort}
	udpConn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		log.Fatalf("failed to bind local UDP socket on 127.0.0.1:%d: %v", *localPort, err)
	}
	defer udpConn.Close()
	log.Printf("bridge (%s) listening for local WireGuard traffic on 127.0.0.1:%d", *role, *localPort)

	// Relay WebSocket connection -- identical relay.go from before works
	// unchanged here; it just pipes bytes between paired sessions and
	// doesn't care what's inside them.
	u, err := url.Parse(*relayURL)
	if err != nil {
		log.Fatalf("invalid relay URL: %v", err)
	}
	q := u.Query()
	q.Set("session", *session)
	q.Set("token", *token)
	q.Set("role", *role)
	u.RawQuery = q.Encode()

	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	ws, _, err := dialer.Dial(u.String(), nil)
	if err != nil {
		log.Fatalf("relay dial failed: %v", err)
	}
	defer ws.Close()
	log.Printf("connected to relay as %s (session=%s)", *role, *session)

	// WireGuard's own process is the only thing that ever sends to this
	// local UDP socket, so we learn its address from the first packet we
	// see and reply to that address from then on.
	var peerAddr *net.UDPAddr

	// Local UDP -> relay.
	go func() {
		buf := make([]byte, 65535)
		for {
			n, addr, err := udpConn.ReadFromUDP(buf)
			if err != nil {
				log.Println("local UDP read error:", err)
				return
			}
			peerAddr = addr
			if err := ws.WriteMessage(websocket.BinaryMessage, buf[:n]); err != nil {
				log.Println("relay write error:", err)
				return
			}
		}
	}()

	// Relay -> local UDP.
	for {
		_, msg, err := ws.ReadMessage()
		if err != nil {
			log.Println("relay read error:", err)
			return
		}
		if peerAddr == nil {
			// Nothing local has sent us a packet yet, so there's nowhere
			// to deliver this. Expected only before your local WireGuard
			// process has sent its first handshake-initiation packet.
			log.Println("dropping relay packet: local WireGuard peer not seen yet")
			continue
		}
		if _, err := udpConn.WriteToUDP(msg, peerAddr); err != nil {
			log.Println("local UDP write error:", err)
			return
		}
	}
}

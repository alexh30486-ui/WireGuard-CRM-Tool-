# ZTNA Product — v2: Real WireGuard + Dumb Bridge

## Quick Start

---

### Quick Start (single machine)

```bash
# 1. Generate keys and fill configs
make wg-keys

# 2. Start the three services (three terminals)
make relay
make bridge-connector
make bridge-client

# 3. Bring up real WireGuard interfaces (needs sudo)
make wg-up-connector
make wg-up-client

# 4. Verify handshake
sudo wg show

The previous version (`connector.go` / `client.go`) reimplemented
WireGuard's own socket layer as a custom `conn.Bind` directly against the
`wireguard-go` library's internal API. That API changes between
`wireguard-go` versions — which is exactly what kept breaking the build —
and bugs inside a custom in-process Bind are invisible to normal tools:
you can't `tcpdump` something that never touches a real socket.

**v2 drops all of that.** Real WireGuard — the actual `wg` / `wg-quick`
command-line tool, unmodified, the same thing millions of people run —
does 100% of the crypto and tunnel logic on each side. All this project
adds is one small program, `bridge.go`, that forwards raw UDP bytes
between WireGuard's local socket and the relay. It has no WireGuard
dependency at all — only `gorilla/websocket`. You can verify it in total
isolation with `tcpdump -i lo udp port <port>`, independent of whether
WireGuard itself is even configured right.

```
Real WireGuard          bridge.go            Relay            bridge.go          Real WireGuard
(wg-quick, unmodified)  (UDP<->WS,          (unchanged,      (UDP<->WS,          (wg-quick, unmodified)
 connector side)         no wg deps)         dumb pipe)        no wg deps)         client side
      │                      │                   │                 │                    │
      │  UDP (encrypted)     │   WebSocket        │   WebSocket     │   UDP (encrypted)  │
      └─────────────────────►│───────────────────►│────────────────►│───────────────────►│
                              │                   │                 │
```

**None of this has been run yet.** Treat it as a strong, testable draft.

## What's in this folder

```
ztna-v2/
├── README.md
├── Makefile
├── control-plane/
│   ├── main.py             unchanged mock control plane
│   └── requirements.txt
├── relay/
│   ├── relay.go            unchanged DERP-style pairing/relay
│   └── go.mod
├── bridge/
│   ├── bridge.go           NEW — the only piece that replaces the old
│   │                       connector.go/client.go custom Bind code
│   └── go.mod
└── wg-config/
    ├── connector.conf      wg-quick config template, connector side
    └── client.conf         wg-quick config template, client side
```

## Prerequisites

- `wireguard-tools` installed (gives you the `wg` and `wg-quick`
  commands). On macOS: `brew install wireguard-tools`. This project was
  built against your Intel x86_64 Mac dev environment.
- Go 1.22+, Python 3 + the packages in `control-plane/requirements.txt`.
- Root/sudo for `wg-quick up` (creating a network interface always needs
  elevated privileges — this hasn't changed from before).

## Exact test sequence (single machine, both "sides" running locally)

This is the Phase 0 validation loop from `production_roadmap.md`,
updated for the new architecture.

1. **Generate keys:**
   ```
   make wg-keys
   ```
   Paste the printed keys into `wg-config/connector.conf` and
   `wg-config/client.conf` (each file's `PrivateKey`, and the *other*
   side's public key under `[Peer] PublicKey`).

2. **Start the relay** (terminal 1):
   ```
   make relay
   ```

3. **Start both bridges** (terminals 2 and 3):
   ```
   make bridge-connector
   make bridge-client
   ```
   Confirm both print "connected to relay as ...".

4. **Bring up both WireGuard interfaces** (terminal 4, needs sudo):
   ```
   make wg-up-connector
   make wg-up-client
   ```

5. **Check the handshake:**
   ```
   sudo wg show
   ```
   Look for a nonzero `latest handshake` on each interface. This is your
   pass/fail signal — if it stays blank, the tunnel isn't establishing
   and you should check the bridge logs first (are packets actually
   arriving at the local UDP ports?).

6. **Prove connectivity:**
   ```
   ping 100.64.0.1    # from a shell that has the client interface up
   ping 100.64.0.2    # from a shell that has the connector interface up
   ```

If `wg show` shows a handshake and the pings work, you have proven the
core loop: **real, unmodified WireGuard, tunneling entirely over an
outbound-only relay, with zero inbound ports opened anywhere.** That's
the actual hard problem this whole project exists to solve, and this is
the first version of the code positioned to actually prove it.

## Known issues / still stubbed

Carried over from before, unaffected by the redesign — see
`production_roadmap.md` for the full phased list:

- `verifyTokenWithControlPlane` in `relay.go` accepts everything. Don't
  run this anywhere reachable from an untrusted network.
- `session_id` and keys are still manually copy-pasted between files —
  no automatic delivery from the control plane yet.
- No reconnect/backoff if the relay drops.
- Single-process relay only, no multi-instance signaling.
- This is a single-machine loopback test. Running connector and client
  on two actual separate machines is the next real milestone after this
  one passes, and will surface NAT/firewall behavior this test can't.
## What and How does this skill work on a day to day basis 

### 2. Better Makefile (auto key injection + nicer targets)

```bash
cat > Makefile << 'EOF'
.PHONY: help control-plane relay bridge-connector bridge-client wg-keys wg-up-connector wg-up-client wg-down tidy clean test-relay-bytes

help:
	@echo ""
	@echo "ZTNA v2 — Real WireGuard + Dumb Bridge"
	@echo "======================================"
	@echo "  make wg-keys            Generate keys and auto-fill wg-config/*.conf"
	@echo "  make relay              Start the pairing relay"
	@echo "  make bridge-connector   Start connector-side bridge"
	@echo "  make bridge-client      Start client-side bridge"
	@echo "  make wg-up-connector    Bring up connector WireGuard interface (sudo)"
	@echo "  make wg-up-client       Bring up client WireGuard interface (sudo)"
	@echo "  make wg-down            Tear down both interfaces"
	@echo "  make tidy               go mod tidy"
	@echo "  make clean              Remove binaries + keys + interfaces"
	@echo "  make test-relay-bytes   Instructions for pure WebSocket test"
	@echo ""

control-plane:
	cd control-plane && python3 -m uvicorn main:app --host 127.0.0.1 --port 8000

relay:
	cd relay && go run .

wg-keys:
	@echo "Generating WireGuard keypairs..."
	@wg genkey | tee connector_private.key | wg pubkey > connector_public.key
	@wg genkey | tee client_private.key    | wg pubkey > client_public.key
	@echo ""
	@echo "Injecting keys into config files..."
	@sed -i.bak "s|REPLACE_WITH_CONNECTOR_PRIVATE_KEY|$$(cat connector_private.key)|" wg-config/connector.conf
	@sed -i.bak "s|REPLACE_WITH_CLIENT_PUBLIC_KEY|$$(cat client_public.key)|" wg-config/connector.conf
	@sed -i.bak "s|REPLACE_WITH_CLIENT_PRIVATE_KEY|$$(cat client_private.key)|" wg-config/client.conf
	@sed -i.bak "s|REPLACE_WITH_CONNECTOR_PUBLIC_KEY|$$(cat connector_public.key)|" wg-config/client.conf
	@rm -f wg-config/*.bak
	@echo "Done. Keys are now in wg-config/*.conf"
	@echo "  connector public : $$(cat connector_public.key)"
	@echo "  client public    : $$(cat client_public.key)"

wg-up-connector:
	sudo wg-quick up ./wg-config/connector.conf

wg-up-client:
	sudo wg-quick up ./wg-config/client.conf

wg-down:
	-sudo wg-quick down ./wg-config/connector.conf
	-sudo wg-quick down ./wg-config/client.conf

bridge-connector:
	cd bridge && go build -o bridge . && ./bridge -role connector -local-port 51821 -session test1

bridge-client:
	cd bridge && go build -o bridge . && ./bridge -role client -local-port 51831 -session test1

tidy:
	cd relay && go mod tidy
	cd bridge && go mod tidy

test-relay-bytes:
	@echo ""
	@echo "1. Start relay:   make relay"
	@echo "2. Terminal A:    websocat 'ws://localhost:8080/ws/relay?session=test1&role=client&token=mvp_test_token_123'"
	@echo "3. Terminal B:    websocat 'ws://localhost:8080/ws/relay?session=test1&role=connector&token=mvp_test_token_123'"
	@echo "4. Type in one → should appear in the other"
	@echo ""

clean:
	rm -f bridge/bridge connector_private.key connector_public.key client_private.key client_public.key
	-sudo wg-quick down ./wg-config/connector.conf 2>/dev/null || true
	-sudo wg-quick down ./wg-config/client.conf 2>/dev/null || true
EOF
## Where to pick up

1. Run the 6-step sequence above. This is the thing to actually get
   working before anything else.
2. If `wg show` shows no handshake after a few seconds, check in this
   order: (a) are both bridge processes still running and connected to
   the relay (no "relay read error" in their logs)? (b) does `wg-config`
   have matching keys — connector's conf has client's *public* key and
   vice versa, never a private key in the peer slot? (c) run `tcpdump -i
   lo udp port 51821` (or `51831`) while bringing the interfaces up to
   confirm packets are actually hitting the bridge's local socket at all.
3. Once step 1 passes, move to two-machine testing before touching
   anything else in `production_roadmap.md`.

   ## RoadMap 
   
###  Better Makefile (auto key injection + nicer targets)

```bash
cat > Makefile << 'EOF'
.PHONY: help control-plane relay bridge-connector bridge-client wg-keys wg-up-connector wg-up-client wg-down tidy clean test-relay-bytes

help:
	@echo ""
	@echo "ZTNA v2 — Real WireGuard + Dumb Bridge"
	@echo "======================================"
	@echo "  make wg-keys            Generate keys and auto-fill wg-config/*.conf"
	@echo "  make relay              Start the pairing relay"
	@echo "  make bridge-connector   Start connector-side bridge"
	@echo "  make bridge-client      Start client-side bridge"
	@echo "  make wg-up-connector    Bring up connector WireGuard interface (sudo)"
	@echo "  make wg-up-client       Bring up client WireGuard interface (sudo)"
	@echo "  make wg-down            Tear down both interfaces"
	@echo "  make tidy               go mod tidy"
	@echo "  make clean              Remove binaries + keys + interfaces"
	@echo "  make test-relay-bytes   Instructions for pure WebSocket test"
	@echo ""

control-plane:
	cd control-plane && python3 -m uvicorn main:app --host 127.0.0.1 --port 8000

relay:
	cd relay && go run .

wg-keys:
	@echo "Generating WireGuard keypairs..."
	@wg genkey | tee connector_private.key | wg pubkey > connector_public.key
	@wg genkey | tee client_private.key    | wg pubkey > client_public.key
	@echo ""
	@echo "Injecting keys into config files..."
	@sed -i.bak "s|REPLACE_WITH_CONNECTOR_PRIVATE_KEY|$$(cat connector_private.key)|" wg-config/connector.conf
	@sed -i.bak "s|REPLACE_WITH_CLIENT_PUBLIC_KEY|$$(cat client_public.key)|" wg-config/connector.conf
	@sed -i.bak "s|REPLACE_WITH_CLIENT_PRIVATE_KEY|$$(cat client_private.key)|" wg-config/client.conf
	@sed -i.bak "s|REPLACE_WITH_CONNECTOR_PUBLIC_KEY|$$(cat connector_public.key)|" wg-config/client.conf
	@rm -f wg-config/*.bak
	@echo "Done. Keys are now in wg-config/*.conf"
	@echo "  connector public : $$(cat connector_public.key)"
	@echo "  client public    : $$(cat client_public.key)"

wg-up-connector:
	sudo wg-quick up ./wg-config/connector.conf

wg-up-client:
	sudo wg-quick up ./wg-config/client.conf

wg-down:
	-sudo wg-quick down ./wg-config/connector.conf
	-sudo wg-quick down ./wg-config/client.conf

bridge-connector:
	cd bridge && go build -o bridge . && ./bridge -role connector -local-port 51821 -session test1

bridge-client:
	cd bridge && go build -o bridge . && ./bridge -role client -local-port 51831 -session test1

tidy:
	cd relay && go mod tidy
	cd bridge && go mod tidy

test-relay-bytes:
	@echo ""
	@echo "1. Start relay:   make relay"
	@echo "2. Terminal A:    websocat 'ws://localhost:8080/ws/relay?session=test1&role=client&token=mvp_test_token_123'"
	@echo "3. Terminal B:    websocat 'ws://localhost:8080/ws/relay?session=test1&role=connector&token=mvp_test_token_123'"
	@echo "4. Type in one → should appear in the other"
	@echo ""

clean:
	rm -f bridge/bridge connector_private.key connector_public.key client_private.key client_public.key
	-sudo wg-quick down ./wg-config/connector.conf 2>/dev/null || true
	-sudo wg-quick down ./wg-config/client.conf 2>/dev/null || true
EOF

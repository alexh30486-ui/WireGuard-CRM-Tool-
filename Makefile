.PHONY: help control-plane relay wg-keys wg-up-connector wg-up-client wg-down bridge-connector bridge-client tidy test-relay-bytes clean

help:
	@echo "ZTNA MVP (v2 - real WireGuard + dumb bridge) targets:"
	@echo "  make control-plane    - run mock FastAPI control plane on :8000"
	@echo "  make relay            - run DERP-style relay on :8080"
	@echo "  make wg-keys          - generate connector + client WireGuard keypairs"
	@echo "  make wg-up-connector  - bring up real WireGuard interface (connector side, needs root)"
	@echo "  make wg-up-client     - bring up real WireGuard interface (client side, needs root)"
	@echo "  make wg-down          - tear down both WireGuard interfaces"
	@echo "  make bridge-connector - build & run the UDP<->relay bridge, connector side"
	@echo "  make bridge-client    - build & run the UDP<->relay bridge, client side"
	@echo "  make tidy             - go mod tidy in relay/ and bridge/"
	@echo "  make test-relay-bytes - reminder for the byte-level relay validation"
	@echo "  make clean            - remove built binaries and bring interfaces down"

control-plane:
	cd control-plane && python3 -m uvicorn main:app --host 127.0.0.1 --port 8000

relay:
	cd relay && go run .

wg-keys:
	@echo "Requires wireguard-tools ('wg' command) installed."
	wg genkey | tee connector_private.key | wg pubkey > connector_public.key
	wg genkey | tee client_private.key    | wg pubkey > client_public.key
	@echo "Paste these into wg-config/connector.conf and wg-config/client.conf:"
	@echo "  connector PrivateKey: $$(cat connector_private.key)"
	@echo "  connector PublicKey (goes in client.conf's Peer):    $$(cat connector_public.key)"
	@echo "  client PrivateKey:    $$(cat client_private.key)"
	@echo "  client PublicKey (goes in connector.conf's Peer):    $$(cat client_public.key)"

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
	@echo "1. Start relay: make relay"
	@echo "2. Open two websocat (or similar) clients:"
	@echo "   websocat 'ws://localhost:8080/ws/relay?session=test1&role=client&token=mvp_test_token_123'"
	@echo "   websocat 'ws://localhost:8080/ws/relay?session=test1&role=connector&token=mvp_test_token_123'"
	@echo "3. Type a message in one; confirm it appears in the other."
	@echo "4. Close one side; confirm the other closes promptly."

clean:
	rm -f bridge/bridge connector_private.key connector_public.key client_private.key client_public.key
	-sudo wg-quick down ./wg-config/connector.conf
	-sudo wg-quick down ./wg-config/client.conf

# Production Roadmap (v2)

Updated after replacing the custom wireguard-go conn.Bind approach with
real WireGuard + a dumb UDP<->relay bridge. Ordered from current draft
toward a shippable multi-tenant ZTNA product.

## Phase 0 — Core tunnel proof (current focus)

- [ ] Generate real keypairs and fill in wg-config/*.conf
- [ ] Byte-level relay validation (two plain WebSocket clients, no WireGuard) —
      still valid, unchanged from before
- [ ] Bring up both bridges + both wg-quick interfaces on one machine
- [ ] Confirm handshake via `wg show` and ping across the overlay
      (100.64.0.1 <-> 100.64.0.2)
- [ ] Repeat the same test across two actual separate machines

## Phase 1 — Session automation & security

- [ ] Control plane generates and distributes wg-config (or the relevant
      fields) instead of manual copy-paste
- [ ] Automatic session_id delivery to both bridges
- [ ] Replace `verifyTokenWithControlPlane` stub with signed short-TTL tokens
- [ ] Bridge reconnect + exponential backoff on relay drop
- [ ] Decide: does the bridge also manage wg-quick lifecycle (up/down),
      or stay pure UDP<->WS forwarding and let something else own that?

## Phase 2 — Control plane hardening

- [ ] Postgres + RLS multi-tenancy
- [ ] Identity providers (OIDC / SAML)
- [ ] Resource catalog + policy engine
- [ ] Admin API + basic dashboard

## Phase 3 — Operational readiness

- [ ] Redis-backed multi-instance relay/signaling
- [ ] Metrics, structured logging, tracing
- [ ] Horizontal scaling story for relay fleet
- [ ] Packaging (systemd units, container images, Helm) — note: packaging
      now needs to install/configure real wireguard-tools as a dependency,
      not just ship a Go binary

## Phase 4 — Product surface

- [ ] Client apps (macOS / Windows / Linux / mobile) — mobile in
      particular will need a different approach, since wg-quick isn't
      available there; likely back to a library-based WireGuard
      implementation on mobile specifically, once the desktop/server
      path is proven
- [ ] Billing / seat tiers
- [ ] Audit log export
- [ ] Compliance documentation (SOC2 path)

Nothing beyond Phase 0 has been implemented. Treat all later phases as
intent only.

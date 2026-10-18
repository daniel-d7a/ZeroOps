# ZeroOps — CV-Value Feature Roadmap (2026-10-18)

Companion to `FIXLIST.md` (fixes land first — then build). This file ranks
new features by CV value per unit of effort. Framing question for every
item: *what does this prove I understand?*

---

## Tier 1 — high CV value, moderate effort (~1–2 weeks combined)

### F1. Git deploy + PR previews
GitHub webhook → build the branch → ephemeral environment per PR. Replaces
the zip upload as the primary source flow.
- **Proves:** webhooks, lifecycle orchestration, ephemeral env cleanup.
- **Scope:** webhook receiver on the API, branch checkout → existing build
  pipeline, preview URL per PR, env teardown on PR close/merge.

### F2. Streaming everything (SSE) — promoted to transport backbone
One SSE channel carries **all** event traffic, not just logs: build/runtime
log lines, deployment status transitions, action completions, notifications,
admin/system events.
- **Proves:** real-time systems, `docker logs --follow` fanout,
  backpressure/fanout design, clean API/worker separation over a broker.
- **Status:** core transport work moved into FIXLIST **Phase 2 (§2.1–2.2)** —
  worker pushes to the `events` fanout exchange, API terminates SSE, UI
  EventSource with polling as fallback. What remains here is the
  *consumer polish*: curated build-events view vs raw debug tail, multi-tab
  fanout, reconnect/resume UX.
- **Bonus:** structurally retires the "too many logs" problem — a streamed
  capped window beats a polled unbounded buffer.
- **Scope:** log-follow in the worker (build tail + container logs) →
  RabbitMQ fanout or direct SSE → live log viewer in the UI.

### F3. CI for the platform itself
GitHub Actions: go test + web tests + e2e suite on every PR; publish worker
images.
- **Proves:** treating the platform as a product; the e2e suite is already
  built for this.
- **Effort:** almost no platform code — pipeline config only.

---

## Tier 2 — hard, exceptional CV value

### F4. Multi-node workers
Prove the existing `node_id` scaffolding: real distributed claims, a node
registry, scheduling onto a node with free capacity, more than one
`run.<node>.q` actually existing.
- **Proves:** distributed control plane, claim semantics, scheduling. The
  single biggest CV-value-per-effort item — turns "compose project" into
  "distributed system" on a resume.
- **Depends on:** FIXLIST Phase 3 heartbeat/claim-steal work (mutually
  reinforcing).

### F5. Idle sleep / scale-to-zero
Detect idleness (traffic/idle timers), stop the container, transparent
wake-on-request (Traefik intercept or a tiny wake-proxy), restart on first
request.
- **Proves:** state machines, cost engineering — Railway's actual hardest
  problem.
- **Risk:** most design-heavy item. Start with a manual sleep button +
  wake-on-URL behind the proxy; automate detection later.

### F6. Project teardown + disk/network GC
Full delete: containers, images, route files, `net-*` networks, DB rows,
student databases.
- **Proves:** lifecycle/resource ownership — exactly what senior platform
  interviews probe. Also retires the real `net-*` leak from the audit.

---

## Tier 3 — only if time remains

### F7. Stripe billing
Webhooks are interview gold, but glue without a product. Skip unless
actually charging.
- **Proves:** payment webhook handling, metered usage. Lower signal per
  hour.

### F8. Browser shell (`docker exec` terminal)
`docker exec` → WebSocket bridge → xterm.js in the browser.
- **Proves:** hardening under a hostile UI surface; flashy demo.
- **Caution:** punches through the sandbox threat model — needs origin/auth
  rigor as strong as the CSRF story; only after FIXLIST Phase 3 lands.

### F9. CLI (`zero`)
Deploy/upload/stream logs from the terminal.
- **Proves:** API client design, DX thinking. Cheap if the API is clean.
  Do last.

### F10. Custom domains + wildcard TLS
Let's Encrypt DNS-01, wildcard certs, real DNS.
- **Proves:** real-domain ops. Modest signal until actually deployed on a
  VPS with traffic.

---

## Decided order (once FIXLIST is done)

**Tier 1** (F1 → F2 → F3) → **F4 (multi-node)** → **F5 (idle sleep)**

Lands the resume line: *"Real-time distributed PaaS with preview
environments and CI."* F6 slots in whenever you (or a user) need to actually
delete a project; F7–F10 purely as time allows.

## Still deliberately out of scope (plan.md §1)

No new users of these gate anything above: Redis add-on, DB backups UI,
metrics graphs, image registry, Laravel support, email, build-time env
vars, app port changes. Anything not on this file or FIXLIST.md stays out
unless a real user asks for it.

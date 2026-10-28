# ZeroOps — Post-Audit Fix & Feature Plan (2026-10-18)

Companion to ASSESSMENT.md (strategic view). This file is the tactical list:
concrete bugs, file:line pointers, and the order to fix them. All findings
come from the Oct 18 audit (API, worker, web, infra agents). Core invariants
verified intact — nothing here violates them; gaps are in resilience, UX
truthfulness, sandboxing, and log growth.

Priority rule: fix what lies to the user first (stop flow, logs), then add
what's missing (runtime logs), then harden.

> **Status 2026-10-26.** Phase 1 is nearly done. 1.1a `9a3b5ce`, 1.1b `ef6ba82`,
> 1.1c `4c59a8d`; 1.2 tail clamp/source suppression/UI window/terminal drain
> `11de4ee`. Still open in Phase 1: the build-events key-line view (§1.2).
> Phases 2–3 and the backlog are untouched.

---

## Phase 1 — Make it not lie to you (~1–2 days)

The stop-flow triad + log growth. These jointly explain every line of notes.md.

### 1.1 Stop flow broken — 3 compounding causes

a) **UI stops polling after mutations** (`ProjectDetailView.vue:26-27`):
`['project']` polls only while status is `new`/`deploying`. After a
stop/start/restart POST the worker flips DB status asynchronously, the UI
refetches once (still the old state), polling stays off → badge says
"running" forever until page reload.
**Fix:** keep polling for a grace window after any run-action mutation
(e.g. poll while `desiredState !== status`, or N seconds post-mutation).

b) **Wedged action rows can permanently lock stop** (`run/run.go:412-415`,
`store/store.go:433-450`): `ClaimAction` has no worker/heartbeat column, so a
worker that dies mid-action leaves the row `running` forever; redelivery
skips-and-acks, and the partial unique index makes the API 409 every future
same-type action ("you can never stop again"). Plan §14 sweeper never built.
**Fix:** let redelivery steal `running` actions (like deployment claims do),
or a titled rescue in the worker loop.

c. **Student containers resurrect on daemon restart / survive compose down**
(`docker` restart policy `on-failure:5` + persistent `traefik-dynamic` route
volume): observed live — stack down since Oct 13, daemon reboot Oct 18
brought a student container back Up with zero platform knowledge.
**Fix:** teardown/cleanup of student containers when the platform goes down
(compose `down` hook or label-driven cleanup; disable restart policy for
student containers if it isn't load-bearing).

### 1.2 "Too many logs" — curation model, provider-style (2026-10-18 revision)

The complaint is not "cap the viewer", it's "Railway/Cloudflare only show
what matters". They never persist the raw flood: live-tail while building,
persist only a capped tail + structured markers afterwards; suppress the
noise at the source (npm `--no-progress`, `--loglevel=error`, CI=true).
Rebuilt plan along those lines:

- **DB** (`deploy_logs`): persist only a **capped tail** — last ~2000 lines,
  every line hard-clipped at 2KB (worker-side at insert). Older lines are
  dropped, not archived. Replaces the "retention prune everything" idea.
- **Suppress at source** (worker build flow): verify what railpack already
  passes (CI=true, non-TTY detection); set `npm_config_loglevel=error` /
  `--no-progress` env in the build container. Goal: fewer lines *born*, not
  fewer lines stored.
- **Build events view** (API): extract key lines (error/warn patterns,
  top-level command headers, final verdict) from the tail as the primary
  "deploy log" view; the raw tail becomes the secondary "debug" toggle (only
  meaningful once runtime logs land in Phase 2).
- **UI** (`DeploymentsTab.vue:42`): `lines` ref grows for the whole session,
  full `v-for` re-render + forced autoscroll per batch → 10k+ DOM nodes on a
  chatty build. **Fix:** viewer defaults to last ~1k lines (drop from front).
- **Final-pull truncation** (`DeploymentsTab.vue:81-99`): only one pull after
  a terminal status flip → any backlog >500 lines is silently lost.
  **Fix:** keep pulling until the batch is short, for a few ticks after
  terminal status.

---

## Phase 2 — SSE transport for everything + missing capability (~3–4 days)

### 2.1 SSE transport, everything (promoted 2026-10-18 — was "logs streaming")

One fanout channel carries **all** event traffic, not just logs:

- **Log lines** (build/runtime) — live raw stream; server-side persistence is
  only the capped tail from §1.2, so the live view and the stored tail are
  the same source of truth.
- **Deployment status transitions** (also fixes the polling blind spot in
  §1.1a structurally).
- **Action completions** (stop/start/restart/deploy done, error, busy).
- **System events** (admin panel: worker up/down, capacity).

Design (decided 2026-10-18):
- **Worker pushes to RabbitMQ; API terminates SSE.** Worker → fanout exchange
  (new `events` exchange in definitions.json) → API consumes → SSE to the
  browser. Matches the invariant "messages carry ids, worker owns state" and
  scales to multi-node for free.
- **Polling stays as fallback** — SSE is primary; on EventSource error/close,
  UI falls back to the existing poll loops (already built, covers buffered
  proxies and reconnect gaps). Poll intervals can stay generous since SSE
  handles the real-time case.

### 2.2 Runtime logs (via the §2.1 channel)

`ContainerLogs` is called **nowhere** in the worker; container stdout goes to
Docker's json-file driver (10MB × 3 in `docker.go:152-155`) which nothing
reads and which is wiped on rebuild. The log viewer only ever shows
build/system output.
**Fix chain:** worker `docker logs --follow` → events exchange → API SSE →
log-viewer "runtime" toggle. Historical tail (post-reconnect window) comes
from a plain `GET /deployments/:id/logs?type=runtime`, reusing the §1.2
capped-tail semantics.

### 2.3 Silent unroutable publishing

`publisher.service.ts:40-49` + `actions.service.ts:72-84`: with
`mandatory: true` an unroutable message comes back via the channel `'return'`
event (only logged) while `waitForConfirms()` **resolves** → publish "succeeds"
for an unbound queue → action/deployment sits `queued` forever; the existing
compensating-write path can never fire.
**Fix:** check the `'return'` event → throw, so the compensation runs.

### 2.4 Platform hygiene

- **Zero memory limits on any compose service** (platform-db, student-db,
  rabbitmq, traefik, api, web, buildkit, worker) — the platform is less
  guarded than its 1GiB-capped student containers. Add
  `deploy.resources.limits` everywhere.
- **No log rotation for platform containers**: default json-file, no
  max-size; `daemon.json.example` lacks `log-opts`. Add
  `max-size`/`max-file` defaults.

---

## Phase 3 — Sandbox & safety (~2 days)

1. **Student containers run as root with default caps** (`docker.go:134-158`):
   `no-new-privileges` + limits are good, but no `User:`/`CapDrop`/
   `ReadOnlyRootfs`/non-default seccomp. Add non-root + `CapDrop: ALL` +
   read-only rootfs + default seccomp; document the escape hatch for apps
   that truly need more (e.g. bind/caps for compiled binaries).
2. **Build steal race**: `heartbeat_at` set once at claim (`store.go:87-93`),
   never refreshed — any build >2 min gets shafted on redelivery → two
   railpacks racing the same workdir. Add heartbeat refresh ticker.
3. **Banned users' apps keep running** (`admin/admin.service.ts:79-92`):
   user-disable wipes sessions but never enqueues `stop` actions for their
   projects (plan §7 requires it). Enqueue stops in the disable transaction.
4. **Concurrent upload → two active deployments** (`deployments.service.ts:114`):
   "one in flight per project" is check-then-act; two uploads both pass.
   Add a DB arbiter (partial unique index like actions have).

---

## Backlog (sweep when bored, don't start here)

- **Retry ladder off-by-one**: 2 retries, not 3 (`queue.go:162-192`; matches
  plan §6, off from AGENTS "3×") — decide and align AGENTS.md or code+e2e 17.
- **NODE_ID/topology seam**: `deploy.replicas: 2` with fixed `NODE_ID=node-1`;
  new nodes get no `run.<node>.q` without hand-editing definitions.json.
- **BASE_DOMAIN drift**: Traefik hardcodes `Host(console.localhost)`
  (`docker-compose.yml:105,119`) while workers build student routes from
  `${BASE_DOMAIN}` — change BASE_DOMAIN and only half the system follows.
- **Quota races**: 3-project/50-var checks are check-then-act, no DB arbiter
  (`projects.service.ts:20-25`, `env.service.ts:59-63,114`).
- **Dead columns**: `deployments.container_name`/`attempt` written by nobody
  (always 0), still exposed in responses (`db/schema.ts:122,124`) — drop.
- **Upload orphan**: failure after `rename(tmp→final)` unlinks the wrong path
  → orphan zips forever (`deployments.service.ts:126`).
- **e2e cleanup danger**: `cleanup.sh:17-35` force-removes all `app-*`
  containers and all `net-*` networks unconditionally — label-filter the rm
  before this ever touches a shared host.
- **Admin route convergence**: `GET /admin/overview` + `/admin/workers`
  collapsed into `GET /admin/system` — matches the web client, drifts from
  plan contract; pick one and document.
- **Restart is destructive** (old container removed before new passes probe;
  failed probe = project with nothing) — accepted by plan §8.3.4, revisit only
  if users scream.
- **Route file hygiene** (worker): `.tmp` files inside Traefik's watched dir
  (parse flapping) `route/route.go:52-56`; worker never re-validates slug
  before YAML interpolation (`route.go:33-46`) — defense-in-depth only.
- **Zombie `running` probes / UI nits**: missing badge classes for
  `superseded` etc. (`ProjectsView.vue:75-77`); `truncate` clips deploy error
  with no readable full text (`DeploymentsTab.vue:150`); env `changed` flag
  never reconciled with restart (`EnvironmentTab.vue:27`); login `redirect`
  param needs a `/`-prefix check (`LoginView.vue:35-36`).
- **Dead deps/small stuff**: Pinia installed+registered, zero stores
  (`main.ts:2,9`); pass-through `createError` computed (`ProjectsView.vue:36`);
  inline `logout` reimplementation (`ProjectsView.vue:27`); inline the
  single-caller `beforeInsert` callback (`deployments.service.ts:72-87`);
  drop multer dupe check (`deployments.service.ts:116`); `Object.hasOwn` in
  `env.service.ts:104`; share the four ownership helpers; consolidate the
  duplicated pool-probe logic (`cleanup.sh:40-52` vs `network-status.sh:40-49`).

---

## Feature roadmap (what to build after the fix phases)

Features start only once the above lands — the platform must first stop lying
about state and show runtime logs. Then, most bang for effort:

1. **Project teardown (deletion)** — the stretch goal that stops the rot:
   no more leaked `net-*` networks / images / route files per project;
   addresses the "address pool fills, disk grows" production risks in
   ASSESSMENT.md §2.
2. **Dogfooding prep** — if deploying to a VPS: rotate default secrets,
   Postgres backups, TLS. (See ASSESSMENT.md §2/§4 — already scheduled there.)
3. **Teacher-facing features** — the B2B wedge per ASSESSMENT.md §5
   (class dashboards, starter templates, per-student quotas). Only worth it
   after 1–2; highest money value, highest effort.

Still deliberately out of scope (plan.md §1): GitHub deploys, custom domains,
idle sleep, metrics, rollbacks UI, email, billing.

---

## Verification

- Unit: `cd services/api && npm test`, `cd services/worker && go test ./...`,
  `cd services/web && npm test` after each phase.
- Full gate: `./scripts/e2e.sh` (18 scenarios, ~10 min) at phase boundaries.
- Key e2e touchpoints: `13-lifecycle.sh` (stop flow), `14-*`/retention,
  `17-retry.sh` (retry ladder), `18-security.sh` (sandbox changes).

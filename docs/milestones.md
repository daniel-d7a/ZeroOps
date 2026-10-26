# ZeroOps milestones and steps

Working checklist of every phase (M0–M7) and its sub-steps. **`plan.md` is the
authoritative spec** — this file is the progress tracker. On conflict, plan.md
wins for design, this file for "what is done".

Status: `[x]` committed · `[~]` started · `[ ]` planned.
Step numbers continue across milestones (M0.1…M1.10…M2.16…); the minor number
resets per milestone since M3 (M3.1, M4.1). Future step numbers are proposals —
re-number freely as they land.

---

## M0 — Scaffold ✅ (gate passed in `10577ea`)

Compose + infra healthy, migrations run.

- [x] **M0.1** `fd00c9d` — AGENTS.md operating manual, README skeleton, gitignore, plan v2
- [x] **M0.2** `17439d1` — repo layout, `.env.example`, `daemon.json.example`
- [x] **M0.3** `2ec6dc0` — compose: platform-db + student-db, healthchecks, platform/db networks
- [x] **M0.4** `d13b20f` — RabbitMQ topology (`definitions.json`), boot wrapper, compose service
- [x] **M0.5** `ca3fc5c` — Traefik static config, compose service on host port 8080, edge network
- [x] **M0.6** `78605ab` — NestJS api skeleton, `/api/health`, console.localhost routing, dev overlay
- [x] **M0.7** `7957a1f` — web service on Caddy (SPA fallback, security headers), console-web routing
- [x] **M0.8** `e929fd7` — Go worker skeleton (config, JSON logs, SIGTERM), buildkit service, Railpack builder
- [x] **M0.9** `10577ea` — Drizzle schema, users migration, migrate one-shot service **[M0 gate]**

## M1 — Auth, users, projects ✅ (gate passed in `fc3ccce`)

- [x] **M1.10** `f28bbe7` — `POST /api/auth/register`: argon2id, validation, rate limiting
- [x] **M1.11** `0302c32` — sessions: express-session + pg store, login/logout/me
- [x] **M1.12** `c9bec09` — CSRF guard: `X-CSRF-Token` on state-changing requests
- [x] **M1.13** `08211df` — admin seed, roles guard, admin users endpoints
- [x] **M1.14** `58983ac` — projects: table, slugify/reserved slugs, create/list/get, quota, ownership (404-not-403)
- [x] **M1.15** `fc3ccce` — Swagger at `/api/docs`, `GET /admin/projects`, **[M1 gate]**

## M2 — Sources, upload, queue plumbing ✅ (gate passed in `db32f99`)

- [x] **M2.16** `0d42368` — schema: sources, deployments, project_actions + open-action unique index
- [x] **M2.17** `38e0bdd` — zip upload + AMQP publisher (confirms, mandatory), file-type sniff, transactional writes
- [x] **M2.18** `710f928` — deployment history + sources endpoints (list, single)
- [x] **M2.18b** `b7613fa` — retention rule specified in plan (newest 5 + live + in-flight, inline prune), manual delete dropped
- [x] **M2.19+20** `152487a` — worker build/run roles (fake work), claims + CAS, project advisory lock, heartbeat, graceful shutdown, 2 replicas
- [x] *(docs)* `1612063` — deployment state machine + API/worker role split (mermaid)
- [x] **M2.21** `a1c4509` — retry/dead-letter classification (attempt ×3, release-claim, MarkFailed), malformed → dead queue, `FORCE_TRANSIENT` hook
- [x] **M2.22** `f7bd9de` — source retention: prune inline on upload (unlink + soft-delete, never fails the request)
- [x] **M2.23** `db32f99` — **[M2 gate]** status chains, transient ladder, duplicate delivery, replicas share load, kill-worker recovery

## M3 — Real build ✅ (gate passed in `79e6db8`)

- [x] **M3.1** `4e107d5` — `deploy_logs` schema + migration 0005 + `GET /deployments/:id/logs` cursor endpoint
- [x] **M3.2** `b0750f9` — safe unzip + sha256 verification + system log line; permanent-failure paths (zip-slip/abs/symlink/nopkg/too-many/sha/missing)
- [x] **M3.3** `fd9929a` — Railpack build with streamed batched logs, `image_ref`, skip-if-exists repair, BuildKit preflight, `DOCKER_API_VERSION` pin
- [x] **M3.4** `79e6db8` — **[M3 gate]** hello built+live with logs, broken-build clean fail, hostile zips, skip-if-image-exists, duplicate job harmless

## M4 — Real run ✅ (gate passed)

*Done when: `hello-node.zip` produces a working `http://<slug>.localhost` URL, a
second upload replaces it with no manual cleanup, and stop and start work as
described in plan §11.*

- [x] **M4.1** `0771894` — real deploy path: `internal/docker` (Engine API, per-project network,
  container limits, TCP probe, container switch, capacity count), `internal/route`
  (Traefik file provider, temp+rename), run.go deploy sequence (lock → capacity → claim →
  network → container → probe → route → switch → MarkLive), config envs
  (`BASE_DOMAIN`, `TRAEFIK_CONTAINER`, `DYNAMIC_CONFIG_DIR`, `MAX_RUNNING_PER_NODE`).
  Container name `app-<deploymentId>` (63-char DNS label limit; plan §8.3/§8.4 synced).
  Go 1.26 + docker v27.5.1 + go-connections v0.6.0 pins.
  *Battery: live URL via Traefik, clean switch on 2nd upload, crash-app fails permanently
  while old version keeps serving.*
- [x] **M4.2** `f864d47` — action endpoints: `POST /projects/:id/stop|start|restart`
  (ActionsService, partial unique index one-open-action-per-type → 409, publish
  `{type:'action'}` to `run.x`), `GET /projects/:id/actions`, `POST /admin/projects/:id/stop`,
  `PublisherService.publishAction`.
- [x] **M4.3** `c263401` — worker action handler (plan §8.6): `Handle` dispatches
  deploy/action; shared `replaceContainer`+`probe`+`permanentError`; **stop** (route
  delete → stop 10s grace, keep containers → `stopped/stopped`) · **start** (reuse
  existing + probe + route, or rebuild if gone, or permanent "image missing, use
  Redeploy") · **restart** (replace-container); claim CAS, transient releases +
  ladder, give-up fails the row.
  *Battery: stop→URL dead, start→same container no rebuild, restart→new ID live,
  nothing-to-start + image-missing permanent, 409 duplicate, replay skipped,
  lock-busy retry.*
- [x] **M4.4** `f9eaab7` — redeploy endpoint: `POST /projects/:id/redeploy {sourceId?}`
  (default newest non-pruned source; pruned/foreign → 404, bad uuid → 400, active →
  409), `kind=redeploy`; `queueDeployment` extracted (tx + publish + compensate,
  upload keeps source insert in-tx via `beforeInsert`).
- [x] **M4.5** `0de14fc` — image prune after MarkLive: `MarkLive` returns the
  superseded image_ref; `RemoveProjectImages` lists `proj-<id>:*` and removes
  everything but live + one before; log-only, never fails a live deploy.
- [x] **M4.6** *(this commit)* — **[M4 gate]** plan §11: hello URL works; 2nd upload
  replaces (hi→hi2) with no manual cleanup; stop → URL dead; start → restored with
  same container and no new deployment; restart → new container, still live; image
  removed → start fails "image missing, use Redeploy", redeploy succeeds. M4 ticked
  in AGENTS.md + README status updated.

## M5 — Database and env vars ✅ (gate passed)

*Done when: `node-with-db.zip` and `env-echo.zip` work, redeploying keeps the same
database, and one project's role cannot connect to another's database.*

- [x] **M5.1** (`fc6c0d8`) — schema: env-var storage (project-scoped, ≤50/project) +
  `project_databases` table; migration.
- [x] **M5.2** (`accf2b7`) — env endpoints (plan §5): `GET /projects/:id/env`,
  `PUT /projects/:id/env/:key`, `DELETE /projects/:id/env/:key` — key regex
  `^[A-Z_][A-Z0-9_]*`, reserved `PORT`/`DATABASE_URL` rejected, values encrypted
  AES-256-GCM (`ENCRYPTION_KEY`), returned only to owner, never logged.
- [x] **M5.3** (`d7b2f5d`) — DB provisioning in deploy path (plan §8.5): insert
  `project_databases` (encrypted generated password) → validate identifier
  `^p_[0-9a-f]{32}$` → `CREATE ROLE`/`CREATE ROLE`-if-missing + `REVOKE CONNECT` →
  status `ready`; student-db joins the project network.
- [x] **M5.4** (`8958741`) — worker injects env at container create: user vars + reserved
  `PORT=3000` + `DATABASE_URL` (from `project_databases` when `wants_db`);
  restart action applies changes (banner is frontend, M6). Fixed two bugs found
  by the battery: invalid `ReleaseClaim("starting")` target (latent since M4.1)
  and Go/TS GCM wire-layout mismatch (`iv‖ct‖tag` vs `iv‖tag‖ct`), pinned by
  an API-generated vector in `crypt_test.go`.
- [x] **M5.5** — **[M5 gate]** fixtures `node-with-db.zip`, `env-echo.zip` committed
  in `fixtures/` (sources alongside); env-echo: set var → still old → restart →
  visible; node-with-db: `/db` round trip works, redeploy keeps the same database
  (old rows survive); cross-project role isolation: own role connects, both
  cross-tempts denied `CONNECT privilege`. Tick M5 + README.

## M6 — Vue frontend ✅ (gate passed in `389222c`)

*Done when: a user can sign up, create project, upload, watch logs, manage env vars
and control the project from the UI; an admin can manage users and projects.*

- [x] **M6.1** — scaffold: Vite + Vue 3 + TS + Pinia + Vue Router + shadcn-vue;
  API client (`credentials: 'include'`, CSRF header), auth guard via `GET /auth/me`.
- [x] **M6.2** — login/register, projects list (status badge, URL, last deploy),
  new project form (name, type Node, needs-Postgres checkbox).
- [x] **M6.3** — project detail → Overview tab: status, URL, Stop/Start/Restart/
  Redeploy buttons (disabled when N/A, confirmation on Stop/Redeploy), upload dialog;
  poll status every 2–3s while in progress.
- [x] **M6.4** — Deployments tab: history + log viewer (poll `after` cursor).
- [x] **M6.5** — Environment tab (add/edit/delete + "restart required" banner) and
  Files tab (saved sources + redeploy-from-zip); Database tab (provision status).
- [x] **M6.6** — admin pages: users (list/status), projects (all + stop),
  workers/overview (heartbeats, deployment counts).
- [x] **M6.7** — serve production build from `web` container (Caddy), Vitest login
  test; **[M6 gate]** full UI walkthrough vs plan §9. Tick M6 + README.

## M7 — Hardening and tests [~] (built; gate blocked on M7.4)

*Done when: `scripts/e2e.sh` passes all fixtures and scenarios and the security
checklist is satisfied.*

Status: M7.1–M7.3 + M7.5 shipped (`347ae46`, e2e + fixtures + README).
M7.4 (startup reconcile) is the only gate item still open.

- [x] **M7.1** — fixtures: the 8 zips from plan §11 (`hello-node`, `node-with-db`,
  `env-echo`, `broken-build`, `no-listen`, `memory-hog`, `zip-slip`, `net-probe`).
- [x] **M7.2** — `scripts/e2e.sh`: upload→live, broken build, health-check fail keeps
  old, stop/start, redeploy after image removal, quotas, retention (files actually
  deleted), retry ladder, duplicate jobs.
- [x] **M7.3** — security checklist (plan §10): socket mounts, network isolation
  (one project cannot reach another), cookie flags/CSRF/session lifecycle, 404-not-403,
  env encryption at rest, secrets not committed, no stray host ports.
- [ ] **M7.4** — startup reconcile (plan §8.7: `desired_state=running` + container
  missing → enqueue `start` after reboot) — deferred from M4.
- [x] **M7.5** — README complete (setup, commands, known limitations from plan §10);
  **[M7 gate]**. Tick M7.

---

**Stretch (only after M7):** project teardown (stop + remove container, network,
route file, image, database, role), SSE log streaming, Laravel support.

**Out of scope (plan §1):** GitHub deploys, custom domains, Redis, DB backups,
rollbacks UI, metrics, idle sleep, multi-machine, image registry, build-time env
vars, changing the app port, email, automatic crash handling.

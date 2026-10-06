# ZeroOps — AGENTS.md

Railway-like hosting platform for student projects. Users sign up, create a project,
upload a Node zip, optionally get a Postgres DB, set env vars, get a live URL under
`<slug>.localhost`.

Authoritative spec: **plan.md** (v2 handoff document). This file is the operating
manual for agents working in this repo. If the two ever disagree, plan.md wins for
design, this file wins for day-to-day conventions and commands.

## Stack

| Part | Choice |
|---|---|
| Control plane | NestJS (TS, Express) — REST under `/api`, Swagger at `/api/docs` |
| DB layer | Drizzle ORM + drizzle-kit; `migrate` one-shot service owns all migrations |
| Frontend | Vue 3 + Vite + TS + Pinia + Vue Router + shadcn-vue, served by Caddy |
| Worker | Go — roles `build` and `run`, `WORKER_ROLES` env, scalable replicas |
| Broker | RabbitMQ 3.x (definitions.json is the source of truth) |
| Platform DB | Postgres 16 `platform-db` (users, projects, deployments, ...) |
| Student DB | Postgres 16 `student-db` (only student databases) |
| Proxy | Traefik v3 — Docker provider (platform, `exposedbydefault=false`), file provider (`/dynamic`, student apps) |
| Builder | Nixpacks CLI inside the worker image, through the local Docker daemon |
| Docker access | Go SDK, worker only. Traefik gets the socket read-only |

## Commands

```sh
# Infra + everything (prod-ish)
docker compose up -d --build

# Development (bind mounts, nest --watch, vite dev server)
docker compose -f docker-compose.yml -f docker-compose.dev.yml up -d --build

# Scale workers
docker compose up -d --scale worker=3

# Apply migrations (also runs automatically before api/worker start)
docker compose run --rm migrate

# Generate migration SQL after editing services/api/src/db/schema.ts
docker compose run --rm api npx drizzle-kit generate

# Tests
cd services/worker && go test ./...     # Go unit tests
cd services/api && npm test             # NestJS unit tests
cd services/web && npm test             # Vitest
scripts/e2e.sh                          # full scenario suite (M7)

# Logs
docker compose logs -f api worker
```

App URLs: console at `http://console.localhost:8080`, student apps at
`http://<slug>.localhost:8080`. Traefik is the only published port — **8080**
(host port 80 belongs to the host's Apache; Traefik listens on 80 *inside* its
container). If port 80 ever becomes free, change the mapping to `80:80` in
`docker-compose.yml` and drop the `:8080` suffixes.

Scripts cannot resolve `console.localhost` / `<slug>.localhost` (the system
resolver has no wildcard for `.localhost`; browsers do). In curl use:

```sh
curl --resolve console.localhost:8080:127.0.0.1 http://console.localhost:8080/api/health
# or plain: curl -H 'Host: console.localhost' http://127.0.0.1:8080/api/health
```

## Invariants (never break these)

- **State ownership:** the worker writes deployment/project/action status transitions
  after a job starts. The API writes only on request handling (create, upload sets
  `deploying`, action rows). Never have both writing the same transition.
- **`deployments.image_ref` is the only place an image reference lives.** Never
  hardcode tags (`proj-<id>:<deploymentId>` is constructed in the build role and
  stored there, then read from there).
- **Migrations are owned by the API** (drizzle). The Go worker has `MIN_SCHEMA_MIGRATION`
  (minimum row count in `drizzle_migrations`) and refuses to run if the schema is older.
  All worker SQL lives in `services/worker/internal/store` only.
- **404, not 403,** when a user asks for another user's resource. Admin routes use the
  roles guard and can read anything.
- **No Traefik labels on student containers.** Student routing = file provider only,
  written by the worker after the new container passes its health probe.
- **Env var values are encrypted (AES-256-GCM), never logged, never returned to
  non-owners.** Reserved keys `PORT`, `DATABASE_URL` cannot be set by users.
- **Only `worker` and `traefik` (read-only) mount the Docker socket.**
- **Student containers only ever join `net-<projectId>`** — never `platform`, `edge`, `db`.
- Messages carry **ids only**; the worker always reloads state from the DB.
- Manual acks: ack only after the final DB write. Transient → retry exchange (3×,
  30 s TTL) → dead queue. Permanent → fail now, no retry.
- Same-origin only: no CORS. Console routing: `console.<BASE_DOMAIN>` +
  `PathPrefix(/api)` → api, everything else → web.
- Never commit `.env`.

## Out of scope (do not build — from plan §1)

GitHub deploys, custom domains, Redis add-on, DB backups, rollbacks UI, metrics/graphs,
idle sleep, multiple machines, image registry, Laravel support, email, build-time env
vars, changing the app port, project deletion/teardown (stretch only), automatic crash
handling.

## Milestone checklist

- [ ] **M0** Scaffold — compose, infra healthy, migrate runs
- [ ] **M1** Auth, users, projects — sessions, CSRF, roles, quotas, Swagger
- [ ] **M2** Sources, upload, queue plumbing — fake build/run, retries, replicas
- [ ] **M3** Real build — safe unzip, nixpacks, log streaming
- [ ] **M4** Real run — networks, container limits, health check, Traefik routing,
      stop/start/restart/redeploy
- [ ] **M5** Database and env vars
- [ ] **M6** Vue frontend (user + admin)
- [ ] **M7** Hardening and tests (plan §10 + §11), README complete

Update the checkbox when a milestone gate passes.

## Reserved

Project slugs: `console`, `api`, `www`, `admin`, `traefik`, `rabbitmq`.

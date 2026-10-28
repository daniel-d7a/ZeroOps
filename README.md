# ZeroOps

A small Railway-like hosting platform: sign up, create a project, upload a Node zip,
optionally get a Postgres database, set environment variables, get a live URL at
`http://<slug>.localhost`.

Spec: `plan.md`. Agent operating manual: `AGENTS.md`.

## Status

Milestones **M0–M7 complete** (`scripts/e2e.sh`, 18 scenarios), including
startup reconcile after a node restart (plan §8.7). The console covers
everything in plan §9: auth, projects list (status, URL, last deploy),
project detail with tabs (overview actions + drag-and-drop upload with
progress, deployment history with source filenames + log viewer, `.env`-style
environment editor with atomic bulk apply, saved files, database status) and the role-gated admin pages (users, projects, system). Phase 2
(runtime logs + SSE) and the remaining sandbox work are tracked in
`docs/FIXLIST.md`; progress lives in `docs/milestones.md`.

## Requirements

- Docker Engine with Compose v5 (`docker compose version`)
- Editable Docker daemon config (for the address pool, see below)

## Quick start

```sh
cp .env.example .env      # then set the secrets
docker compose up -d --build
```

To stop the platform **including student apps**, use `scripts/down.sh`
(plainer `docker compose down` never touches student containers — they are
not part of the compose project; see the script header for why that matters).

- Console: http://console.localhost:8080
- Student apps: http://<slug>.localhost:8080
- API: http://console.localhost:8080/api (Swagger: `/api/docs`) 

> Port **8080**, not 80: the host runs Apache on port 80 (pulled in by
> `libapache2-mod-php8.5`). Traefik still listens on 80 inside its container.
> To move to port 80 later: `sudo systemctl disable --now apache2`, then change
> the port mapping in `docker-compose.yml` to `80:80`.

Development with hot reload:

```sh
docker compose -f docker-compose.yml -f docker-compose.dev.yml up -d --build
```

## Known issue: Docker address pool fills up

**Every project creates one Docker network (`net-<projectId>`), and the MVP has
no project teardown — networks accumulate forever.** Docker's default pool only
allows ~30 user-defined networks, so past ~25-30 projects every new deploy
fails with:

```
all predefined address pools have been fully subnetted
```

Existing projects keep running; only *new* network creation (new deploys) is
blocked. The failure appears in the deployment's error/logs after ~60s
(3 retries).

Check the pool any time (safe on production hosts):

```sh
bash scripts/network-status.sh   # counts, capacity, and a live allocation probe
```

Fix once, ideally **before the first deploy** (gives ~4000 projects):

```sh
sudo cp daemon.json.example /etc/docker/daemon.json
sudo systemctl restart docker && docker compose up -d
```

Do **not** free space by deleting `net-*` networks: a running project's network
is in use (deleting it takes the project down), and a stopped project needs its
network for the next `start`. Only enlarge the pool — or implement project
teardown (plan §14 backlog). The e2e runner's `scripts/e2e/cleanup.sh` deletes
networks and is for test hosts only, never production.

## Known limitations

- **Docker networks grow without bound** — one per project, no teardown yet
  (see the address-pool section above).
- Documented in plan §10 — full list for the final release at M7.

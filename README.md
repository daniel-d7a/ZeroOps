# ZeroOps

A small Railway-like hosting platform: sign up, create a project, upload a Node zip,
optionally get a Postgres database, set environment variables, get a live URL at
`http://<slug>.localhost`.

Spec: `plan.md`. Agent operating manual: `AGENTS.md`.

## Status

Milestone **M0 (scaffold)** in progress. See the checklist in `AGENTS.md`.

## Requirements

- Docker Engine with Compose v5 (`docker compose version`)
- Editable Docker daemon config (for the address pool, see below)

## Quick start

```sh
cp .env.example .env      # then set the secrets
docker compose up -d --build
```

- Console: http://console.localhost
- Student apps: http://<slug>.localhost
- API: http://console.localhost/api (Swagger: `/api/docs`)

Development with hot reload:

```sh
docker compose -f docker-compose.yml -f docker-compose.dev.yml up -d --build
```

## Docker address pool (required before many projects)

Docker only creates ~30 user-defined networks by default; every project gets one.
Add to `/etc/docker/daemon.json` (see `daemon.json.example`) and restart Docker:

```sh
sudo systemctl restart docker
```

## Known limitations

Documented in plan §10 — filled in for the final release at M7.

# Hosting Platform MVP: Build Plan (v2)

Handoff document for the implementing agent. Read it fully before writing code. Build in the milestone order in section 13. Do not add features outside the scope in section 1.


## 0. What changed from v1


- Added user-defined environment variables.
- Added a Vue web frontend: users manage their projects, an admin sees all users and projects.
- Added stop, start, restart and redeploy actions.
- Uploaded source zips are now kept (with retention rules) so any project can be rebuilt or redeployed.
- Auth now uses server-side sessions (cookies), not JWT.
- Workers are replicable from day one, and work is split into two job types: `build` and `run`. For now both run in the same worker process on the same machine as the Docker daemon.
- Every project gets its own Docker network.
- Database migrations are owned by the NestJS app.
- Routing moved from Docker labels to Traefik's file provider, so traffic is switched only after a new container is healthy (section 8.4).


## 1. Goal and scope


A small Railway-like platform for student projects. A user signs up, creates a project, uploads a zip of a Node app, optionally gets a Postgres database, sets environment variables, and receives a live URL. Users can stop, start, restart and redeploy their projects. An admin can see everyone.


**In scope (MVP)**
- Accounts with session auth, roles `user` and `admin`
- Projects (type `node` only; keep `type` as a column so Laravel can be added later)
- Zip upload that creates a deployment; uploaded zips are stored and reusable
- Build with Railpack (successor to Nixpacks, runs through a BuildKit container), run as a resource-limited container, route by subdomain with Traefik
- Optional Postgres database per project (created on first deploy)
- User-defined environment variables per project
- Stop, start, restart (apply new env vars) and redeploy (rebuild from a stored zip)
- Deployment status and build logs via the API (polling)
- Vue frontend for users and admins
- Several worker replicas from the start; separate `build` and `run` jobs
- One Docker network per project
- Everything runs through Docker Compose


**Out of scope (do not build)**
GitHub deploys, custom domains, Redis add-on, database backups, rollbacks UI, metrics and graphs, idle sleep, multiple machines, image registry, Laravel support, email, build-time env vars, changing the app port, project deletion/teardown (stretch only), automatic crash handling (the product owner wants manual redeploy after crashes).


**Defaults chosen (change if wrong)**
- Single host for development and first deployment, base domain `localhost` (apps at `<slug>.localhost`).
- Registration is open (`REGISTRATION_ENABLED=true`). The first admin is created from env vars at startup.
- Frontend UI library is the implementer's choice (Tailwind, PrimeVue or Naive UI are all fine).


## 2. Stack and decisions


| Part | Choice | Notes |
|---|---|---|
| Control plane | NestJS (TypeScript, Express platform) | REST API under `/api`, session auth, publishes jobs |
| Frontend | Vue 3, Vite, TypeScript, Pinia, Vue Router | Built to static files, served by a Caddy container |
| Worker | Go | Consumes `build` and `run` jobs |
| Message broker | RabbitMQ 3.x with management plugin | Retries and dead-lettering |
| Platform DB | Postgres 16 (`platform-db`) | Users, sessions, projects, deployments, logs, env vars |
| Student DB server | Postgres 16 (`student-db`) | Only holds student project databases |
| Reverse proxy | Traefik v3 | Docker provider for platform services, file provider for student apps |
| Builder | Railpack CLI inside the worker image | Builds through the `buildkit` service (BuildKit); `BUILDKIT_HOST` env |
| Docker access | Official Go SDK (`github.com/docker/docker/client`) | Worker only (Traefik gets a read-only socket) |


**Decision: migrations live in NestJS.** Use TypeORM or Prisma migrations, with auto-sync disabled (TypeORM `synchronize: false`). A one-shot `migrate` service uses the API image to apply migrations before `api` and `worker` start. The Go worker uses `pgx` with plain SQL against these tables. Safety rules:
- Make changes backward compatible: add columns and tables first, remove old ones in a later release.
- The worker checks the latest applied migration at startup and refuses to run if it is older than the minimum it needs (`MIN_SCHEMA_MIGRATION` constant).
- Keep all worker SQL in one package (`store`) and add an integration test that runs it against a freshly migrated database.


**Decision: same machine now, split jobs anyway.** Build and run are separate job types on separate queues. A worker process enables roles through `WORKER_ROLES` (default `build,run`). Later, a builder-only and a runner-only worker can run on different machines without redesign (section 12).


**Decision: file handoff.** The API writes uploads to a Docker volume `uploads` (read-write in `api`, read-only in `worker`). No object storage yet.


**Decision: no registry yet.** Builds and runs share one Docker daemon, so images are already visible to the runner. Always read and write images through the `deployments.image_ref` column (never hardcode tags elsewhere) so a registry can be added later.


## 3. Repository layout


```
platform/
  docker-compose.yml
  .env.example
  daemon.json.example          # Docker address pool settings (section 4)
  rabbitmq/
    rabbitmq.conf
    definitions.json           # exchanges, queues, bindings (source of truth)
  traefik/
    traefik.yml                # static config
  services/
    api/                       # NestJS, includes migrations and seed command
    web/                       # Vue app, Dockerfile builds static files into the Caddy image
    worker/                    # Go, Dockerfile includes docker CLI and railpack
  fixtures/                    # sample zips for tests (section 11)
  scripts/e2e.sh
  README.md
```


## 4. Containers and networks


**Services in `docker-compose.yml`**
- `traefik` (`container_name: traefik`): the only service publishing a host port (80). Docker provider enabled with `exposedbydefault=false` for platform services. File provider watching `/dynamic` for student apps. Docker socket mounted read-only. Dashboard disabled.
- `web`: Caddy serving the built Vue app.
- `api`: NestJS. No Docker socket.
- `worker`: Go. Run with `deploy: replicas: 2` (no `container_name`, so it can scale). Has the Docker socket, `uploads` (read-only), `traefik-dynamic` (read-write) and a `work` volume for build directories.
- `platform-db`, `student-db` (`container_name: student-db`), `rabbitmq`, `buildkit` (privileged, no host ports — Railpack's BuildKit daemon), and a one-shot `migrate`.


**Fixed networks (give each an explicit `name:`)**
- `platform`: api, worker, platform-db, rabbitmq.
- `edge`: traefik, api, web.
- `db`: worker, student-db (the worker uses it to administer student databases).


**Per-project networks (created by the worker at runtime)**
- Name: `net-<projectId>`, bridge driver, labeled `platform.project=<projectId>`.
- Members: that project's app container, `traefik`, and `student-db` (only if the project has a database). Student containers are never on `platform`, `edge` or `db`.
- Two projects therefore cannot see each other. Traefik and `student-db` are the only shared members.
- The worker joins a project network temporarily to health check a new container, then leaves it.


**Docker address pools (required setup).** By default Docker can only create a few dozen user-defined networks. Configure a large pool in the daemon settings before creating many projects, for example `{"default-address-pools":[{"base":"10.192.0.0/12","size":24}]}` (gives about 4000 networks). On Docker Desktop, put it in the Docker Engine settings. Document this in the README and provide `daemon.json.example`.


**Rules**
- Healthchecks on stateful services (`pg_isready`, `rabbitmq-diagnostics -q ping`). Use `depends_on: condition: service_healthy`. `api` and `worker` wait for `migrate` to complete successfully.
- Named volumes: `platform-db-data`, `student-db-data`, `rabbitmq-data`, `uploads`, `traefik-dynamic`, `work`.
- RabbitMQ gets a dedicated user from env. The management UI binds to `127.0.0.1:15672` only.
- Routing for the console: `console.${BASE_DOMAIN}` with `PathPrefix(/api)` going to `api` and everything else to `web`. Same origin, so no CORS and cookies work simply.
- Reserved project slugs: `console`, `api`, `www`, `admin`, `traefik`, `rabbitmq`.
- Secrets come from `.env` (provide `.env.example`, never commit `.env`).


## 5. Data model (platform DB)


Use UUID primary keys and `timestamptz` columns. NestJS migrations create all of this.


**users**: `id`, `email` (unique), `password_hash` (argon2id), `role` (`user` or `admin`), `status` (`active` or `disabled`), `created_at`


**sessions**: session id, `user_id`, serialized data, `expires_at`, `created_at`. The store must support deleting all sessions of one user.


**projects**: `id`, `user_id`, `name`, `slug` (unique), `type` (`node`), `wants_db`, `node_id` (default `node-1`), `desired_state` (`running` or `stopped`), `status` (`new`, `deploying`, `running`, `stopped`, `failed`), `current_deployment_id` (nullable), `created_at`


**sources** (saved uploads): `id`, `project_id`, `path`, `size_bytes`, `sha256`, `original_filename`, `created_at`, `deleted_at` (nullable)


**deployments**: `id`, `project_id`, `source_id`, `kind` (`upload` or `redeploy`), `status`, `image_ref` (nullable), `container_name` (nullable), `error` (nullable), `attempt`, `worker_id`, `heartbeat_at`, `triggered_by`, `created_at`, `started_at`, `finished_at`
- Status values: `queued` → `building` → `built` → `starting` → `live`, or `failed`. A replaced live deployment becomes `superseded`.


**project_actions** (stop, start, restart requests): `id`, `project_id`, `type` (`stop`, `start`, `restart`), `status` (`queued`, `running`, `done`, `failed`), `error`, `requested_by`, `created_at`, `finished_at`. Add a unique partial index so a project has at most one open (`queued` or `running`) action per type.


**env_vars**: `id`, `project_id`, `key`, `value_encrypted`, `created_at`, `updated_at`. Unique on `(project_id, key)`.


**project_databases**: `id`, `project_id` (unique), `db_name`, `db_user`, `password_encrypted`, `status` (`provisioning` or `ready`), `created_at`


**deploy_logs**: `id` (bigserial), `deployment_id`, `stream` (`system`, `build`), `line`, `created_at`. Index on `(deployment_id, id)`.


**workers**: `id` (hostname), `roles`, `node_id`, `started_at`, `last_seen_at` (updated every 15 seconds; shown in the admin UI).


## 6. Message contract (RabbitMQ)


Topology lives in `rabbitmq/definitions.json`. Services do not redeclare it; they assume it exists and fail fast on startup if not. Publishers use publisher confirms and the `mandatory` flag so unroutable messages are detected.


| Object | Name | Settings |
|---|---|---|
| Exchange, queue | `build.x`, `build.q` | direct, durable, key `build` |
| Retry | `build.retry.x`, `build.retry.q` | `x-message-ttl: 30000`, dead-letters to `build.x` with key `build` |
| Dead | `build.dead.x`, `build.dead.q` | inspection only |
| Exchange | `run.x` | direct, durable. Routing key is the node id. |
| Queue per node | `run.node-1.q` | bound to `run.x` with key `node-1` |
| Retry per node | `run.retry.x`, `run.node-1.retry.q` | `x-message-ttl: 30000`, dead-letters to `run.x` with key `node-1` |
| Dead | `run.dead.x`, `run.dead.q` | inspection only |


Adding a node later means adding its queues to `definitions.json` (document this in the README).


**Messages (JSON, persistent)**
- Build: `{"type":"build","deployment_id":"<uuid>"}`
- Run: `{"type":"deploy","deployment_id":"<uuid>"}` or `{"type":"stop"|"start"|"restart","action_id":"<uuid>"}`


Messages carry ids only. The worker loads current state from the DB.


**Consumer rules**
- Manual acks only. Ack after the final DB state is written.
- Prefetch is set per role: `BUILD_PREFETCH=1` (builds are heavy), `RUN_PREFETCH=4`.
- Transient error: publish to the retry exchange with an incremented `attempt` header, then ack. At attempt 3, mark `failed`, publish to the dead exchange, ack.
- Permanent error: mark `failed` (or the action `failed`), ack, no retry.
- On SIGTERM: stop consuming, finish or nack-requeue in-flight jobs, then exit.


**Error classes**
- Permanent: missing `package.json`, unsafe zip, source file missing or checksum mismatch, build failure caused by the user's code, health check failed, node at capacity, image missing for a `start`.
- Transient: Docker daemon errors or timeouts, DB or broker connection errors, project lock not acquired, name conflicts from a half-finished previous attempt.


**Publisher (API):** insert the DB row, then publish. If the publish fails, mark the row `failed` and return 503.


## 7. API (NestJS)


Everything is under `/api`. Provide Swagger at `/api/docs`.


**Sessions**
- Use `express-session` with a Postgres-backed store (table `sessions`). Cookie: `httpOnly`, `SameSite=Lax`, `Secure` when not on localhost, rolling idle timeout (e.g. 7 days) and an absolute maximum (e.g. 30 days).
- Regenerate the session id on login. Logout destroys the session.
- CSRF: because cookies authenticate requests, require a CSRF token header (`X-CSRF-Token`) on every state-changing request. The token is returned by `GET /auth/me` and login.
- Passwords use argon2id. Rate-limit auth routes strictly (`@nestjs/throttler`), and all other routes moderately.
- The first admin is created by a seed command from `ADMIN_EMAIL` and `ADMIN_PASSWORD` on startup if no admin exists.
- Disabled users cannot log in. Disabling a user deletes their sessions and enqueues `stop` actions for all their projects.


**Endpoints**


| Method and path | Purpose |
|---|---|
| `POST /auth/register`, `POST /auth/login`, `POST /auth/logout`, `GET /auth/me` | Session auth |
| `POST /projects`, `GET /projects`, `GET /projects/:id` | Projects. Responses include status and the live URL. |
| `POST /projects/:id/deployments` | Multipart zip upload. Saves a source, creates a deployment, publishes a `build` job. |
| `GET /projects/:id/deployments`, `GET /deployments/:id` | History and status |
| `GET /deployments/:id/logs?after=<id>` | Log lines after a cursor id (clients poll) |
| `POST /projects/:id/redeploy` | Body `{sourceId?}` (default: latest source). New deployment, rebuilds from the stored zip. |
| `POST /projects/:id/stop`, `/start`, `/restart` | Create a `project_actions` row and publish a `run` job. Return the action id. |
| `GET /projects/:id/actions` | Recent actions and results |
| `GET /projects/:id/env`, `PUT /projects/:id/env/:key`, `DELETE /projects/:id/env/:key` | Environment variables |
| `GET /projects/:id/sources`, `DELETE /projects/:id/sources/:sourceId` | Saved uploads (cannot delete the source of the live deployment) |
| `GET /admin/users`, `PATCH /admin/users/:id` | List users; change `status` (`active` or `disabled`) |
| `GET /admin/projects`, `POST /admin/projects/:id/stop` | All projects with owner and status; stop any project |
| `GET /admin/workers`, `GET /admin/overview` | Worker heartbeats; counts of deployments by status |


Admin routes use a roles guard. Pagination on all list endpoints.


**Rules**
- Every query is scoped by the authenticated user. Return 404 (not 403) for other users' resources. Admins can read any project.
- Upload: max 50 MB, check zip magic bytes, compute `sha256`, save to `/data/uploads/<projectId>/<sourceId>.zip`. Reject (409) if the project already has a deployment in `queued`, `building`, `built` or `starting`.
- Quotas (configurable): max 3 projects per user, max 500 MB of stored sources per user, max 50 env vars per project.
- Retention: keep the newest 5 sources per project plus the source of the live deployment. A nightly cron (`@nestjs/schedule`) deletes older files and sets `deleted_at`.
- Env var rules: key matches `^[A-Z_][A-Z0-9_]*$`, max 128 characters; value max 4 KB; reserved keys `PORT` and `DATABASE_URL` cannot be set. Encrypt values with AES-256-GCM using `ENCRYPTION_KEY`. Return values only to the owner (and admin), and never log them. Changing env vars does not affect a running container; the UI shows "restart required".


## 8. Worker (Go)


**Packages:** `config`, `queue`, `store`, `docker` (SDK wrapper behind an interface), `build`, `run`, `provision`, `network`, `routing`, `crypto`, `maintenance`.


**Configuration (env):** `NODE_ID=node-1`, `WORKER_ROLES=build,run`, `BUILD_PREFETCH`, `RUN_PREFETCH`, `MAX_RUNNING_PER_NODE` (default 50), `BASE_DOMAIN`, `TRAEFIK_CONTAINER=traefik`, `STUDENT_DB_CONTAINER=student-db`, `DYNAMIC_CONFIG_DIR=/dynamic`, `UPLOADS_DIR`, `ENCRYPTION_KEY`, `PLATFORM_DB_URL`, `STUDENT_DB_ADMIN_URL`, `AMQP_URL`, `BUILDKIT_HOST=docker-container://buildkit`.


**Replicas:** the worker is stateless. Its id is its hostname. Replicas share one queue, so RabbitMQ distributes jobs automatically. Scale with `docker compose up --scale worker=N`. It must be correct with any number of replicas.


**Per-project lock:** `run` jobs take a session-level Postgres advisory lock keyed by the project id (`pg_try_advisory_lock`). If the lock is not acquired, treat it as transient and retry later. This prevents, for example, a stop and a deploy running at the same time.


### 8.1 Build role (consumes `build.q`)


1. **Claim.** `UPDATE deployments SET status='building', worker_id=$2, heartbeat_at=now(), started_at=COALESCE(started_at, now()) WHERE id=$1 AND (status='queued' OR (status='building' AND heartbeat_at < now() - interval '2 minutes'))`. If zero rows: if the status is `built`, republish the `deploy` run job (the earlier publish may have failed) and ack; otherwise ack and skip. Update `heartbeat_at` every 15 seconds while working.
2. **Load source.** Read the zip path from `sources`. If the file is missing or its `sha256` does not match, fail permanently.
3. **Extract** into `/work/<deploymentId>` (delete first if it exists). Safe-unzip rules: reject path traversal and absolute paths, reject symlinks, max 5000 files, max 200 MB uncompressed. If there is a single top-level directory, use it as the root. Require `package.json` for type `node`.
4. **Build.** Run `railpack build --name proj-<projectId>:<deploymentId> --progress plain <dir>` with a 10-minute timeout. Stream stdout and stderr line by line into `deploy_logs` (stream `build`), batched about once per second. Skip the build if that image already exists.
5. **Finish.** Set `status='built'` and `image_ref`. Publish `{"type":"deploy","deployment_id":...}` to `run.x` with the project's `node_id` as the routing key. Delete the work directory. Ack.


### 8.2 Run role: deploy (consumes `run.<node>.q`)


1. Take the project lock. Check capacity: if running containers on this node are at `MAX_RUNNING_PER_NODE`, fail permanently with a clear message.
2. **Claim:** `built` → `starting` (same claim pattern with a heartbeat).
3. Load env vars (decrypt), then add `PORT=3000`.
4. **Database (only if `wants_db`):** see 8.5. Add `DATABASE_URL`.
5. Run the **replace-container routine** (8.3).
6. On success: set the deployment `live`, mark the previous live deployment `superseded`, set `projects.current_deployment_id`, `status='running'`, `desired_state='running'`. Prune old images for this project, keeping the live one and the one before it. Ack.


### 8.3 Replace-container routine (used by deploy, restart, and start when the container is missing)


1. Ensure the project network exists (create if missing, with the project label). Connect `traefik` to it (ignore "already connected"). Connect `student-db` too if the project has a database.
2. Remove any leftover container with the new container's name. Create `app-<projectId>-<nonce>` (nonce is the deployment id or action id) from `image_ref`:
   - Env: user vars plus `PORT=3000` and `DATABASE_URL` if present.
   - Limits: CPU 0.5 (`NanoCPUs: 500_000_000`), memory 1 GiB, `MemorySwap` equal to memory, `PidsLimit: 256`, `no-new-privileges`, not privileged, no Docker socket.
   - Restart policy `on-failure` with max 5 retries. Log driver `json-file` with `max-size=10m`, `max-file=3`.
   - Labels `platform.project=<id>` and `platform.deployment=<id>`. No Traefik labels.
   - Attached only to `net-<projectId>`.
3. Start it. Temporarily connect this worker container to the project network (use the container's own id from its hostname), probe TCP `<container-name>:3000` for up to 60 seconds, then disconnect.
4. If the probe fails, remove the new container and fail permanently. The old container and the existing route are untouched.
5. If it passes, write the routing file (8.4), then stop and remove the other containers of this project (found by label).


### 8.4 Routing with Traefik's file provider


Traefik watches `/dynamic`. For each project the worker writes `/dynamic/proj-<projectId>.yml` with a router (`Host(\`<slug>.<BASE_DOMAIN>\`)`, entrypoint `web`) and a service pointing at `http://app-<projectId>-<nonce>:3000`. Write to a temp file and rename it so Traefik never reads a half-written file. The route moves only after the new container is healthy, which gives a clean switch and leaves the old version live when a deploy fails. Stopping a project deletes its file.


### 8.5 Database provisioning (inside deploy, once per project)


- Insert `project_databases` with status `provisioning` and an encrypted generated password before touching `student-db`.
- Names: `p_<project uuid without dashes>` for both role and database. Validate against `^p_[0-9a-f]{32}$` before using in SQL (identifiers cannot be parameterized).
- If the role exists, run `ALTER ROLE ... PASSWORD` with the stored password. Otherwise `CREATE ROLE ... LOGIN CONNECTION LIMIT 10`. Create the database owned by that role if missing. `REVOKE CONNECT ON DATABASE ... FROM PUBLIC`. Set the status to `ready`.
- Connection string: `postgres://<user>:<pass>@student-db:5432/<db>`. It works because `student-db` joins the project network.


### 8.6 Run role: stop, start, restart (messages carry `action_id`)


- Take the project lock and claim the action (`queued` → `running`). If it is already `done` or `failed`, ack and skip.
- **stop:** delete the routing file, stop the project's containers (10-second grace) without removing them, set `desired_state='stopped'` and `status='stopped'`. A manual stop is not undone by Docker's restart policy.
- **start:** if the stopped container exists, start it, probe it, rewrite the routing file. If it is missing, run the replace-container routine from the current deployment's `image_ref`. If the image is gone, fail with "image missing, use Redeploy". Set `desired_state='running'`, `status='running'`.
- **restart:** run the replace-container routine from the current image so new env vars are applied.
- Record the result in `project_actions` (`done` or `failed` with an error), then ack.


### 8.7 Other worker duties


- **Heartbeat:** upsert the `workers` row every 15 seconds.
- **Startup reconcile (runner role):** for projects on this node with `desired_state='running'` whose container is not running, enqueue a `start` action (skipped if an open one exists). This brings projects back after a machine reboot.
- **Crash policy:** never auto-redeploy a crashed project. Docker's `on-failure:5` restarts the same image only. Crash state tracking is post-MVP.
- Pass `context.Context` with timeouts to every external call. Use structured JSON logs with `deployment_id`, `project_id`, `action_id` and `worker_id` on every line. Keep all Docker calls behind an interface so they can be faked in tests.


## 9. Frontend (Vue)


Vue 3 with Vite, TypeScript, Pinia and Vue Router. The API client uses `credentials: 'include'` and sends the CSRF header. Route guards use `GET /auth/me`. Poll status every 2 to 3 seconds while a deployment or action is in progress, and poll logs with the `after` cursor.


**User pages**
- Login and register.
- **Projects list:** name, status badge, live URL, last deploy time.
- **New project:** name, type (Node), "needs Postgres" checkbox.
- **Project detail** with tabs:
  - *Overview:* status, URL, buttons for Stop, Start, Restart and Redeploy (disabled when not applicable, with confirmation for Stop and Redeploy), and an upload dialog (drag and drop zip with progress).
  - *Deployments:* history with status, source and time. Opening one shows the log viewer.
  - *Environment:* add, edit and delete variables. Show a "restart required" banner after changes, with a Restart button.
  - *Database:* shows whether one is provisioned (name and status). The connection string is injected automatically as `DATABASE_URL`.
  - *Files:* saved sources with size and date, and a Redeploy-from-this-zip action.
- Clear, friendly error messages for the common failures (build failed, health check failed, image missing, quota reached).


**Admin pages (role-gated)**
- **Users:** email, role, status, project count, created date; disable or enable.
- **All projects:** owner, status, node, URL; stop action.
- **System:** worker list with last heartbeat and roles; counts of deployments by status.


Serve the production build from the `web` container (Caddy with security headers and SPA fallback). Add a Vitest test for the login flow. A Playwright smoke test (login, create project, upload) is optional.


## 10. Security checklist (must pass before calling the MVP done)


- Only `worker` and `traefik` (read-only) mount the Docker socket. `api`, `web` and student containers never do.
- Student containers are only on their own `net-<projectId>`. Verified by test: one project cannot reach another.
- Student DB admin credentials exist only in the worker's environment.
- Uploads are validated (size, magic bytes, checksum, safe unzip) before extraction.
- Session cookie flags set; CSRF header enforced; session id regenerated on login; sessions destroyed on logout and when a user is disabled.
- Admin endpoints reject non-admins. Every user endpoint is scoped to the owner.
- Env var values are encrypted at rest, never logged, and never returned to other users.
- Secrets (`SESSION_SECRET`, `ENCRYPTION_KEY`, DB and broker passwords) come from env and are not committed.
- No host ports are published except Traefik's port 80 (and the RabbitMQ UI on `127.0.0.1` in development).


**Known limitations (document in the README)**
- The worker holds the Docker socket, which is effectively root on the host. Keep its code small and reviewed. A socket proxy is a later hardening step.
- The `buildkit` container runs privileged (required by BuildKit's build isolation). It gets no Docker socket and publishes no host ports.
- `student-db` joins every project network that has a database; revisit when the number of projects reaches the hundreds.
- No registry and a single machine: images and sources live on one host.
- Build-time env vars are not supported.


## 11. Test fixtures and verification


Create these in `fixtures/` and cover them in `scripts/e2e.sh`:
1. `hello-node.zip`: minimal Express app on `$PORT`. Expect `live` and a working URL.
2. `node-with-db.zip`: connects with `DATABASE_URL` and exposes `GET /db`. Expect a working round trip.
3. `env-echo.zip`: exposes `GET /env?key=GREETING`. Set the variable, restart, and verify the value appears.
4. `broken-build.zip`: fails during build. Expect `failed` with no retries and visible build logs.
5. `no-listen.zip`: never listens. Expect `failed` from the health check, and the previous live version (if any) still serving.
6. `memory-hog.zip`: allocates beyond 1 GiB. Expect the container killed by Docker without affecting other services.
7. `zip-slip.zip`: contains `../` paths. Expect rejection.
8. `net-probe.zip`: tries to reach another project's container. Expect failure (network isolation).


Scenario tests:
- **Stop and start:** stop makes the URL stop responding; start restores it without a rebuild.
- **Redeploy from a stored zip** after removing the image: `start` fails with a clear message, `redeploy` succeeds.
- **Replicas:** run 3 workers and upload 10 projects. Each deployment is processed exactly once.
- **Idempotency:** deliver the same message twice, and kill a worker mid-build and mid-deploy. Expect exactly one container and a correct final state.
- **Retry:** force a transient error. Expect 3 attempts, then a message in the dead queue.
- **Sessions:** CSRF rejection, logout invalidation, disabled user loses access immediately.
- **Quotas and retention:** project limit, storage limit, old sources pruned.


Add unit tests for slug generation, safe-unzip, the claim queries, the retry decision, env var validation and encryption.


## 12. Designed for later expansion (keep these seams)


- **Split roles across machines:** run builder-only workers (`WORKER_ROLES=build`) and runner-only workers (`WORKER_ROLES=run`) with different `NODE_ID`s.
- **Add a registry:** the builder pushes `image_ref`, the runner pulls it. Only `build` and `run` code changes.
- **Add runtime nodes:** add `run.<node>` queues to `definitions.json`, assign `projects.node_id`, and run Traefik (or a shared routing layer) per node. The student DB server then needs a private network or VPN with TLS, and sources need shared storage (MinIO or S3).
- **Keep Docker calls behind an interface** and image references behind `image_ref`.


## 13. Milestones (build in this order)


**M0: Scaffold.** Repo layout, Compose file, `.env.example`, all infrastructure healthy, Docker address pool documented, `migrate` runs an initial migration.
*Done when:* `docker compose up` brings everything up healthy from a clean checkout.


**M1: Auth, users and projects.** Sessions, CSRF, roles, admin seed, projects (create and read), slug rules, quotas, Swagger.
*Done when:* register, login, create project and list projects work via curl, ownership is enforced, and admin endpoints reject normal users.


**M2: Sources, upload and queue plumbing.** `definitions.json`, upload with saved sources, publisher with confirms, a worker with both roles that only fakes the work (build marks `built` and publishes a run job, deploy marks `live`). Claim logic, retries and dead-lettering, prefetch, graceful shutdown, worker heartbeat, 2 replicas.
*Done when:* uploads move through all statuses, a forced transient error retries 3 times then lands in the dead queue, duplicate delivery does no harm, and replicas share the load.


**M3: Real build.** Safe unzip and Railpack build with log streaming in the build role.
*Done when:* `hello-node.zip` ends `built` with an image and complete build logs; `broken-build.zip` fails cleanly.


**M4: Real run.** Per-project networks, container creation with all limits, health check, Traefik file-provider routing, switch, plus stop, start, restart and redeploy.
*Done when:* `hello-node.zip` produces a working `http://<slug>.localhost` URL, a second upload replaces it with no manual cleanup, and stop and start work as described in section 11.


**M5: Database and env vars.** Provisioning, `DATABASE_URL` injection, user env vars with encryption, restart to apply.
*Done when:* `node-with-db.zip` and `env-echo.zip` work, redeploying keeps the same database, and one project's role cannot connect to another's database.


**M6: Vue frontend.** User pages first, then admin pages (can start after M2 using the Swagger contract).
*Done when:* a user can sign up, create a project, upload, watch logs, manage env vars and control the project from the UI, and an admin can see and manage users and projects.


**M7: Hardening and tests.** Everything in sections 10 and 11, README with setup and known limitations.
*Done when:* `scripts/e2e.sh` passes all fixtures and scenarios and the security checklist is satisfied.


**Stretch (only after M7):** project teardown (stop, remove container, network, routing file, image, database and role), SSE log streaming, Laravel support.


## 14. Backlog (explicitly later)


GitHub App deploys, rollbacks, change port, custom domains (Caddy on-demand TLS is an option), Redis add-on, database backups and restore, external database access, CPU and RAM metrics, automatic crash tracking and notifications, a registry and build workers on a separate machine, multiple runtime nodes, gVisor or Sysbox sandboxing, build-time env vars, disk usage quotas, project type `dockerfile` (build a student's Dockerfile with the existing BuildKit service — fits the existing `projects.type` seam), project type `compose` (multi-container student apps — needs its own design: routing/port selection, managed-DB vs student-DB conflict, per-container limits, teardown), stuck-deployment sweeper (periodic task in the API, ~60s `setInterval`, no new dependency: republish jobs for deployments with no consumer progress — `queued` older than 10 min [upload committed but the build publish was lost], `built` with a stale heartbeat [run job lost], `building`/`starting` heartbeat silent for >5 min [acked-but-lost, above the 2 min claim-steal window]; all writes conditional (`WHERE status=...`) so the claim paths stay authoritative, escalate to `failed` only past ~30 min total age; repairs the publish-after-commit crash window that dual-write + confirms leaves open).

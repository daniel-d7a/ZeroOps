# ZeroOps — Project Assessment & Roadmap (2026-10-12)

Wrap-up review at M0–M7 completion. Honest assessment of the current state,
production risks, a scaling plan to ~1000 users, and where to take it next.

## 1. Project in its current state

**Good:**
- The invariants actually hold — single-writer status transitions, `image_ref`
  as the only image source, 404-not-403, env encryption, socket discipline,
  per-project networks. Most student projects skip this rigor; here it is
  enforced *and* tested.
- The e2e suite is the crown jewel — real HTTP, real RabbitMQ, real Docker,
  17 scenarios, ~10 min, self-cleaning. It caught two genuine bugs
  (worker-kill message loss, Docker address-pool exhaustion).
- Error taxonomy (permanent vs transient vs busy) + retry ladder to a dead
  queue is production-grade thinking, not tutorial code.
- Comments explain *why*, which is rarer and more valuable than the code
  itself.

**Bad:**
- Everything runs on one Docker daemon — the multi-node design exists in
  queues (`run.<node>.q`) and schema (`node_id`) but is unproven scaffolding.
- Zero observability: no metrics, no alerting, no tracing. Outages would be
  discovered via users.
- Every project leaves a `net-*` network forever — teardown doesn't exist, so
  the platform slowly rots (documented, mitigated, not solved).
- No backups. One disk failure loses all users, projects, databases.

**Ugly:**
- Free-tier apps run 24/7 — the cost model only works until it doesn't. Idle
  sleep was explicitly out of scope and is the single most important missing
  piece.
- Abuse controls are thin: per-container limits exist (memory-hog e2e proves
  OOM handling works), but nothing stops a user burning CPU on crypto or
  serving abuse traffic.

## 2. Issues you'll face in production

1. **Address pool fills** after ~30 projects (no teardown) — the documented
   `daemon.json` enlargement (README known-issue) buys thousands of slots,
   but each dead project still leaks a network + disk.
2. **Disk death**: build layers, images, uploads, logs — no GC beyond
   per-project source retention. `/var/lib/docker` and `/data` grow unbounded.
3. **No TLS for real domains** — `*.localhost` is dev-only; real domains need
   a wildcard cert (DNS-01 challenge) and real DNS.
4. **Single box = single blast radius**: Postgres, RabbitMQ, Traefik, worker,
   student apps — all die together.
5. **No monitoring/alerting** (Uptime Kuma + a metrics stack is the minimum).
6. **Default secrets** — admin password and RabbitMQ password must be rotated
   on first deploy.
7. **Student app logs** — currently container stdout, lost on redeploy. No
   retention policy.
8. **Node capacity UX** — `node at capacity` is a *permanent* failure; users
   will experience it as "platform broken".

## 3. Deploy & scale to ~1000 users — numbers

Assume: 1000 registered, ~400 active, ~600 projects, avg ~1.2 always-relevant
apps. App containers sized ~128MiB / 0.25 CPU. Prices are approximate 2026 EU
pricing.

| Tier | Spec | Cost/mo | Holds |
|---|---|---|---|
| Edge + API + web + Traefik | 1 small VPS (4 vCPU/8GB) | ~€15 | control plane |
| Platform DB | managed Postgres | ~€15 | users/projects |
| Broker | CloudAMQP small or self-hosted on a node | ~€0–50 | queues |
| App nodes | 3× 16 vCPU/64GB (Hetzner AX-class) | ~€180 | ~100–150 apps each with sleep, ~50–80 always-on |
| **Total** | | **~€210–260/mo** | |

Revenue, freemium at €7/mo pro:

- 3–5% conversion (typical) = 30–50 payers → **€210–350/mo** — roughly
  break-even.
- The math only gets good with:
  1. **Idle sleep** (free-tier costs drop ~3×, one fewer node),
  2. **edu/bootcamp licenses** — a bootcamp paying €500–2000/term for 100
     seats dwarfs consumer conversion,
  3. density (256MiB apps on shared nodes).

Honest thought: consumer freemium infra is a thin-margin, high-support
business. The survivable version is **B2B-lite: sell to classrooms**, where
the buyer is an institution, not a student with a credit card.

## 4. Next step

**Dogfood it for real.** Put it on a VPS with a real domain, wildcard TLS,
rotated secrets, Postgres backups (pg_dump cron → offsite), and put 5–10 real
users on it for a month. Every gap above will show itself in priority order —
that backlog is worth more than any feature list. Features can wait; a week
of real traffic cannot.

## 5. Vision to profit

**"Railway for classrooms."** The big PaaS players have abandoned this segment
(free tiers killed, edu credits miserable). The wedge: dead-simple Node
deploy + Postgres + live URL + **teacher-facing features** (class dashboards,
starter templates, per-student quotas, gradebook hooks) that Heroku/Railway
will never build.

Paths, in order of realism:

1. **University/bootcamp licenses** (per-seat/term) — real money, low volume.
2. **Freemium subs** (always-on, custom domains, more RAM) — volume play,
   needs sleep to survive.
3. **Open-core**: community edition free/self-hosted (Go/Vue — the
   demographic loves self-hosting), paid enterprise (SSO, RBAC, billing,
   support). Least capital-intensive.

## 6. What to add — and what each teaches you

| Feature | Teaches you |
|---|---|
| **Idle sleep / scale-to-zero** | cron, container state machines, cost engineering — do this first |
| **Teardown + disk quotas** | lifecycle GC, quota enforcement, the address-pool problem |
| **Custom domains + TLS** | Let's Encrypt DNS-01, wildcard certs, DNS APIs |
| **Multi-node workers** | node registry, real distributed claims, maybe Kubernetes migration later |
| **Metrics & logs** | Prometheus/Grafana, Loki/OpenSearch, SLOs, alerting |
| **Git deploys + PR previews** | webhooks, git plumbing, ephemeral envs |
| **CLI (`zero` command)** | Cobra, API client design, DX thinking |
| **Billing (Stripe)** | metered usage, webhooks, invoicing reality |
| **Collab/orgs + SSO** | RBAC design, OIDC |
| **CI for the platform itself** | GitHub Actions matrix, go test + e2e in CI, image publishing |
| **IaC** (Terraform the whole stack) | reproducible infra, the "prod deploy" skill |

If I had to pick three for learning-per-effort: **idle sleep** (forces you
into the state machine deeply), **CI pipeline** (the e2e suite is ready for
it), **multi-node** (the code is already shaped for it — prove the `node_id`
bet).

---

*M0–M7 complete. E2E gate: 17/17 scenarios green (`./scripts/e2e.sh`).*

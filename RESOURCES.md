# ZeroOps (Understanding This Codebase) Resources

## Knowledge

- [`plan.md`](../plan.md) — authoritative project spec (v2 handoff). Use for: what ZeroOps is supposed to be, module responsibilities, what's in/out of scope.
- [`AGENTS.md`](../AGENTS.md) — operating manual: commands, invariants, milestone status. Use for: day-to-day conventions, "never break these" rules.
- [NestJS docs](https://docs.nestjs.com/) — the control plane (`services/api`) is NestJS. Use for: module/controller/service wiring and guards/interceptors.
- [Vue 3 docs](https://vuejs.org/guide/introduction.html) — the console (`services/web`). Use for: composition API, Pinia, TanStack Query patterns.
- [DDIA, ch. on distributed systems / event-driven design](https://dataintensive.net) — grounding for why messages carry only ids and the worker reloads state. Use for: "why" behind queue design.
- [Traefik v3 docs](https://doc.traefik.io/traefik/) — routing, dynamic file provider vs docker provider. Use for: why student containers have no labels and are routed via `/dynamic`.
- [Railpack docs](https://railpack.com) — the builder invoked by the Go worker build role. Use for: build steps, fech-era buildkit interplay.
- [NATS/RabbitMQ patterns](https://www.rabbitmq.com/tutorials.html) — manual acks, TTL + DLX retries. Use for: the retry/dead-letter design in `services/worker/rabbit/`.

## Wisdom (Communities)

- (to fill in — user hasn't expressed a community preference yet; ask before proposing)

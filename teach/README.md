# ZeroOps Teaching Workspace

Your anti-getting-lost kit for the ZeroOps codebase. Everything cites real files/functions.

## Start here
1. [`lessons/0001-architecture-30000ft.html`](lessons/0001-architecture-30000ft.html) — open this first
2. [`lessons/0002-finite-state-machine.html`](lessons/0002-finite-state-machine.html) — every state, edge, and failure class
3. [`reference/architecture.html`](reference/architecture.html) — the big map (keep in a tab)

## Reference library (the "where is X handled?" layer)
- [reference/deploy-lifecycle.html](reference/deploy-lifecycle.html) — zip → live URL, every step annotated
- [reference/queue-topology.html](reference/queue-topology.html) — RabbitMQ retry ladder + idempotency ledger
- [reference/api-map.html](reference/api-map.html) — NestJS modules, routes, tables, config
- [reference/worker-map.html](reference/worker-map.html) — Go worker package-by-package
- [reference/glossary.html](reference/glossary.html) — say it right

## Working files
- `MISSION.md`, `RESOURCES.md`, `NOTES.md`, `learning-records/`

## House rules
- Every HTML page has a collapsible sidebar (button top-left or press `n`) linking all lessons + references.
- Diagrams use Mermaid via CDN (needs network when you open them).
- Code links are relative paths — open from this folder in your editor.
- Mission: draw the architecture from memory and explain any invariant say-why.

## Self-study runners
- For e2e scenario names → follow `scripts/e2e/NN-*.sh` filenames (see `scripts/e2e.sh --list`).
- Run `cd services/worker && go test ./...` and `cd services/api && npm test` after reading each reference; mapping test names back to the functions is a great retention exercise.

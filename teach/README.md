# ZeroOps Teaching Workspace

Your anti-getting-lost kit for the ZeroOps codebase. Everything cites real files/functions.

## Start here
0. [`index.html`](index.html) — home / orientation (also the published site root)
1. [`lessons/0001-architecture-30000ft.html`](lessons/0001-architecture-30000ft.html) — open this first
2. [`lessons/0002-finite-state-machine.html`](lessons/0002-finite-state-machine.html) — every state, edge, and failure class
3. [`lessons/0003-upload-to-build.html`](lessons/0003-upload-to-build.html) — file-before-row, safe unzip, Railpack
4. [`lessons/0004-run-to-route.html`](lessons/0004-run-to-route.html) — limits, probe gate, atomic route
5. [`lessons/0005-auth-request-path.html`](lessons/0005-auth-request-path.html) — sessions, CSRF, 404-not-403
6. [`lessons/0006-secrets-and-databases.html`](lessons/0006-secrets-and-databases.html) — encrypted env, provisioning
7. [`lessons/0007-reliability-and-gaps.html`](lessons/0007-reliability-and-gaps.html) — acks, claims, retry ladder, gaps
8. [`lessons/0008-console-data-flow.html`](lessons/0008-console-data-flow.html) — query keys, polling, log cursor
9. [`lessons/0009-infrastructure.html`](lessons/0009-infrastructure.html) — compose, networks, Traefik
10. [`lessons/0010-e2e-walkthrough.html`](lessons/0010-e2e-walkthrough.html) — the 17 scenarios
11. [`lessons/0011-capstone.html`](lessons/0011-capstone.html) — explain it without notes
12. [`reference/architecture.html`](reference/architecture.html) — the big map (keep in a tab)

## Reference library (the "where is X handled?" layer)
- [reference/deploy-lifecycle.html](reference/deploy-lifecycle.html) — zip → live URL, every step annotated
- [reference/queue-topology.html](reference/queue-topology.html) — RabbitMQ retry ladder + idempotency ledger
- [reference/api-map.html](reference/api-map.html) — NestJS modules, routes, tables, config
- [reference/worker-map.html](reference/worker-map.html) — Go worker package-by-package
- [reference/glossary.html](reference/glossary.html) — say it right

## Working files
- `MISSION.md`, `RESOURCES.md`, `NOTES.md`, `learning-records/`

## House rules
- Every HTML page has a collapsible sidebar (hamburger on mobile, press `n` on desktop) linking all lessons + references.
- Dark theme by default; the navbar button (☀/☾) switches to light and remembers your choice.
- Diagrams use Mermaid via CDN (needs network when you open them).
- Code links are relative paths — open from this folder in your editor.
- Mission: draw the architecture from memory and explain any invariant say-why.

## Self-study runners
- For e2e scenario names → follow `scripts/e2e/NN-*.sh` filenames (see `scripts/e2e.sh --list`).
- Run `cd services/worker && go test ./...` and `cd services/api && npm test` after reading each reference; mapping test names back to the functions is a great retention exercise.

## Publishing (study from your phone)

The workspace is plain static HTML — no build step. It is published from the `gh-pages` branch via GitHub Pages:

**Live site:** <https://daniel-d7a.github.io/ZeroOps/>

### How it works
`git subtree push` copies this folder's contents to the **root** of the `gh-pages` branch (there is no `teach/` prefix on that branch). GitHub Pages serves that branch root as-is; `.nojekyll` stops Jekyll from processing it. Relative links like `../assets/style.css` keep working because the `lessons/` and `reference/` folders stay one level deep.

### Update the live site
After editing/committing docs on `main`:

```sh
# from the repo root
git add teach && git commit -m "docs(teach): ..."
git subtree push --prefix teach origin gh-pages
```

Pages redeploys in ~1 minute. The phone URL never changes. Check status with:

```sh
gh api repos/daniel-d7a/ZeroOps/pages --jq '.status,.html_url'
```

### First-time setup (already done — keep for reference)
```sh
git subtree push --prefix teach origin gh-pages

# If Pages is not yet enabled, point it at the branch:
printf '{"source":{"branch":"gh-pages","path":"/"}}' \
  | gh api --method POST repos/daniel-d7a/ZeroOps/pages --input -
```

Notes: the site is **public**; mermaid diagrams load from a CDN, so you need internet to render them; workspace links in the sidebar point at the GitHub repo because files outside `teach/` are not published.

**Why creating `gh-pages` is enough:** GitHub Pages publishes a branch + folder (`/` or `/docs`) exactly as pushed — here the branch root *is* the site. If no Pages site exists yet, pushing a branch named `gh-pages` auto-configures it as source `gh-pages:/`; if Pages is already configured, you must select the branch in Settings. Documented in [Configuring a publishing source](https://docs.github.com/en/pages/getting-started-with-github-pages/configuring-a-publishing-source-for-your-github-pages-site) and the [Pages REST API](https://docs.github.com/en/rest/pages/pages) (`build_type: legacy` = GitHub builds on branch push).

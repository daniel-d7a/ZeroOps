#!/usr/bin/env bash
# ── Fixture 1 (plan §11.1): the happy path ──────────────────────────────
# Scenario: a brand-new user uploads the smallest possible Node app.
# Expect:   deployment status becomes "live", and http://<slug>.localhost
#           answers 200 with the app's exact body (proves Traefik routing,
#           container start, and health check all worked).
. "$(dirname "${BASH_SOURCE[0]}")/common.sh"
zip_dir hello-node

say "hello-node fixture"
signup u "u-${RUN}@zeroops.test"
create_proj u "Hello ${RUN}" "${RUN}-hello" && PID=$NEW_PID
upload u "$PID" fixtures/generated/hello-node.zip && DEP=$NEW_DEP
wait_for 300 "deployment live" dep_is u "$DEP" live
SLUG="${RUN}-hello"
app_req "$SLUG" /
assert_eq 200 "$CODE" "URL responds 200"
assert_contains "$(cat "$BODY")" "hello from hello-node" "serves its body"

finish

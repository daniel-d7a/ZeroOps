#!/usr/bin/env bash
# ── Fixture 5 (plan §11.5): health check protects the live version ──────
# Scenario: first deploy a WORKING app (the "previous version"), then
#           upload no-listen — an app that starts but never opens a port.
# Expect:   the new deployment fails the health check ("health check
#           failed"), while the previous version KEEPS serving traffic and
#           the project's currentDeploymentId still points at it — a bad
#           deploy must never take down what's already running.
. "$(dirname "${BASH_SOURCE[0]}")/common.sh"
zip_dir hello-node
zip_dir no-listen

say "no-listen fixture (previous live version must keep serving)"
signup u "u-${RUN}@zeroops.test"
create_proj u "Prev ${RUN}" "${RUN}-prev" && PID=$NEW_PID

# vehicle: deploy a working app first
upload u "$PID" fixtures/generated/hello-node.zip && PREV_DEP=$NEW_DEP
wait_for 300 "previous deployment live" dep_is u "$PREV_DEP" live
app_req "${RUN}-prev" /
assert_eq 200 "$CODE" "previous version serves"

# now upload an app that never listens
upload u "$PID" fixtures/generated/no-listen.zip && DEP=$NEW_DEP
wait_for 240 "no-listen deployment failed" dep_is u "$DEP" failed
NL_ERR=$(dep_field u "$DEP" error)
assert_contains "$NL_ERR" "health check failed" "fails the health check"

app_req "${RUN}-prev" /
assert_eq 200 "$CODE" "previous live version still serves after failed deploy"
assert_contains "$(cat "$BODY")" "hello from hello-node" "previous version body intact"
assert_eq "$PREV_DEP" "$(proj_field u "$PID" currentDeploymentId)" "current deployment unchanged by failed deploy"

finish

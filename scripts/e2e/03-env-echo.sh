#!/usr/bin/env bash
# ── Fixture 3 (plan §11.3): environment variables + restart ─────────────
# Scenario: deploy the app (no vars yet), then set GREETING=hello-e2e via
#           the API and run the restart action.
# Expect:   the restart action finishes "done" and GET /env?key=GREETING
#           now answers with the NEW value — proving env vars are injected
#           into the container on restart (they are not live-reloaded).
. "$(dirname "${BASH_SOURCE[0]}")/common.sh"
zip_dir env-echo

say "env-echo fixture"
signup u "u-${RUN}@zeroops.test"
create_proj u "Env ${RUN}" "${RUN}-env" && PID=$NEW_PID
upload u "$PID" fixtures/generated/env-echo.zip && DEP=$NEW_DEP
wait_for 300 "deployment live" dep_is u "$DEP" live

reqj PUT "/api/projects/$PID/env/GREETING" --data '{"value":"hello-e2e"}'
assert_code 200 "set GREETING"
req POST "/api/projects/$PID/restart"
wait_action u "$PID" restart 120
assert_eq "done" "$ACTION_STATUS" "restart action done"
wait_for 60 "project running after restart" proj_is u "$PID" running
app_req "${RUN}-env" "/env?key=GREETING"
assert_eq 200 "$CODE" "/env responds after restart"
assert_eq "hello-e2e" "$(cat "$BODY")" "GREETING visible after restart"

finish

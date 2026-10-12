#!/usr/bin/env bash
# ── Scenario (plan §11): stop / start / redeploy ────────────────────────
# Part 1 — stop & start without rebuilding:
#   stop  → URL stops answering, project status "stopped"
#   start → URL answers again AND the deployment count is unchanged
#           (start reuses the stored image; it must not create a deployment)
# Part 2 — image gone missing (simulated by deleting the container + image):
#   start → fails with a clear message telling the user to redeploy
#   redeploy → rebuilds from the saved zip and goes live again.
# NOTE: currently FAILS on the start assertions — known product bug: start
# reports "nothing to start: no deployment yet" even though a live
# deployment exists. The test is correct; the worker/API needs fixing.
. "$(dirname "${BASH_SOURCE[0]}")/common.sh"
zip_dir hello-node

say "stop, start, redeploy after image removal"
signup u "u-${RUN}@zeroops.test"
create_proj u "Life ${RUN}" "${RUN}-life" && PID=$NEW_PID
upload u "$PID" fixtures/generated/hello-node.zip && DEP=$NEW_DEP
wait_for 300 "deployment live" dep_is u "$DEP" live
SLUG="${RUN}-life"

req GET "/api/projects/$PID/deployments"
TOTAL_BEFORE=$(jget "$BODY" total)

req POST "/api/projects/$PID/stop"
wait_action u "$PID" stop 60
assert_eq "done" "$ACTION_STATUS" "stop action done"
wait_for 30 "project stopped" proj_is u "$PID" stopped
app_req "$SLUG" /
if [ "$CODE" != 200 ]; then ok "URL down while stopped (code=$CODE)"; else bad "URL still 200 while stopped"; fi

req POST "/api/projects/$PID/start"
wait_action u "$PID" start 60
assert_eq "done" "$ACTION_STATUS" "start action done"
wait_for 60 "project running after start" proj_is u "$PID" running
app_req "$SLUG" /
assert_eq 200 "$CODE" "URL back after start (no rebuild)"
req GET "/api/projects/$PID/deployments"
assert_eq "$TOTAL_BEFORE" "$(jget "$BODY" total)" "start created no new deployment"

# container + image removed → start fails with a clear message, redeploy fixes it
req POST "/api/projects/$PID/stop"
wait_action u "$PID" stop 60
docker rm -f "app-$DEP" >/dev/null 2>&1
docker rmi "proj-$PID:$DEP" >/dev/null 2>&1 && ok "image removed" || bad "could not remove image"
req POST "/api/projects/$PID/start"
wait_action u "$PID" start 60
assert_eq "failed" "$ACTION_STATUS" "start fails without image"
assert_contains "$ACTION_ERROR" "Redeploy" "start failure tells the user to redeploy"
req POST "/api/projects/$PID/redeploy"
NEW_DEP=$(jget "$BODY" id)
assert_code 201 "redeploy accepted"
wait_for 300 "redeploy goes live" dep_is u "$NEW_DEP" live
# poll instead of a single curl: Traefik reloads the route file just after
# status flips to live, so an immediate request can briefly 404 (race)
url_ok() { app_req "$SLUG" /; [ "$CODE" = 200 ]; }
wait_for 30 "redeployed app serves again" url_ok

finish

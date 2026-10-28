#!/usr/bin/env bash
# ── Scenario (plan §8.7): startup reconcile ─────────────────────────────
# Part 1 — a running project loses its container outside the API (as after a
# machine reboot). Restart the workers. Expect a start action to appear and
# the app to serve again.
# Part 2 — a user-stopped project must stay stopped. desired_state='stopped'
# keeps the reconciler away even though no container is running.
. "$(dirname "${BASH_SOURCE[0]}")/common.sh"
zip_dir hello-node

url_ok() { app_req "$SLUG" /; [ "$CODE" = 200 ]; }
url_down() { app_req "$SLUG" /; [ "$CODE" != 200 ]; }

say "startup reconcile (running project)"
signup u "u-${RUN}@zeroops.test"
create_proj u "Reconcile ${RUN}" "${RUN}-recon" && PID=$NEW_PID
upload u "$PID" fixtures/generated/hello-node.zip && DEP=$NEW_DEP
SLUG="${RUN}-recon"
wait_for 300 "deployment live" dep_is u "$DEP" live
wait_for 30 "app serves before the reboot" url_ok

docker stop "app-$DEP" >/dev/null 2>&1 || bad "could not stop app-$DEP"
wait_for 30 "URL down after the container stopped" url_down
ok "container stopped outside the API (desired_state still running)"

compose restart worker >/dev/null 2>&1
wait_for 60 "workers restarted" workers_running 1
wait_action u "$PID" start 120
assert_eq "done" "$ACTION_STATUS" "reconcile start action done"
req GET "/api/projects/$PID/actions"
STARTS=$(python3 - "$BODY" <<'PY'
import json, sys
try:
    items = json.load(open(sys.argv[1])).get("items", [])
except Exception:
    items = []
print(sum(1 for x in items if x.get("type") == "start"))
PY
)
assert_eq "1" "$STARTS" "exactly one start action from two reconcilers"
wait_for 60 "project running after reconcile" proj_is u "$PID" running
wait_for 30 "app serves after reconcile" url_ok
if [ "$(docker inspect -f '{{.State.Running}}' "app-$DEP" 2>/dev/null)" = "true" ]; then
  ok "reconciled container is running"
else
  bad "reconciled container is not running"
fi

say "startup reconcile (stopped project stays stopped)"
req POST "/api/projects/$PID/stop"
wait_action u "$PID" stop 60
assert_eq "done" "$ACTION_STATUS" "stop action done"
wait_for 30 "project stopped" proj_is u "$PID" stopped

compose restart worker >/dev/null 2>&1
wait_for 60 "workers restarted again" workers_running 1
sleep 5
req GET "/api/projects/$PID/actions"
assert_eq "stop" "$(jget "$BODY" items[0].type)" "no start action for a stopped project"
wait_for 15 "stopped project stays stopped" proj_is u "$PID" stopped
app_req "$SLUG" /
if [ "$CODE" != 200 ]; then ok "stopped app stays down (code=$CODE)"; else bad "stopped app came back up"; fi

finish

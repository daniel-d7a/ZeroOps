#!/usr/bin/env bash
# ── Scenario (plan §11): retry ladder then dead-letter ──────────────────
# Restart the workers with FORCE_TRANSIENT=true (a built-in test hook that
# makes every build attempt fail with a known message).
# Expect, in order:
#   1. the deployment ends "failed" and the error contains that message
#   2. it took ≥60s — the ladder is 3 attempts separated by two 30s TTL
#      waits, so a fast failure means retries never ran
#   3. RabbitMQ's dead queue (build.dead.q) holds our message with
#      attempt=2 header (deliveries 0,1,2 then dead) + the failure reason
# The file restores normal workers afterwards (trap backstop too).
. "$(dirname "${BASH_SOURCE[0]}")/common.sh"
zip_dir hello-node

say "retry ladder (FORCE_TRANSIENT hook)"
WORKER_DIRTY=1
FORCE_TRANSIENT=true compose up -d --no-build --scale worker=2 worker >/dev/null 2>&1
WID=$(compose ps -q worker | head -1)
if docker inspect -f '{{range .Config.Env}}{{println .}}{{end}}' "$WID" 2>/dev/null | grep -qx 'FORCE_TRANSIENT=true'; then
  ok "worker running with FORCE_TRANSIENT=true"
else
  bad "FORCE_TRANSIENT not applied to worker container"
fi

signup u "u-${RUN}@zeroops.test"
create_proj u "Retry ${RUN}" "${RUN}-retry" && PID=$NEW_PID
T0=$SECONDS
upload u "$PID" fixtures/generated/hello-node.zip && DEP=$NEW_DEP
wait_for 300 "deployment failed after retries" dep_is u "$DEP" failed
ELAPSED=$((SECONDS - T0))
ERR=$(dep_field u "$DEP" error)
assert_contains "$ERR" "forced transient error" "error records the forced cause"
# ladder: 30 s TTL waits between attempts → ≥60 s before the final failure
if [ "$ELAPSED" -ge 60 ]; then
  ok "3 attempts spread over ≥60 s (elapsed ${ELAPSED}s)"
else
  bad "failed in ${ELAPSED}s — retry ladder did not run"
fi

# dead queue: our message parked, attempt header documents the ladder
RESP=$(curl -sS --max-time 10 -u "zeroops:$RBP" -H 'Content-Type: application/json' \
  -X POST "http://127.0.0.1:15672/api/queues/%2f/build.dead.q/get" \
  -d '{"count":20,"ackmode":"ack_requeue_false","encoding":"auto","truncate":5000}' || echo '{}')
D_LINE=$(python3 - "$RESP" "$DEP" <<'PY'
import json, sys
try:
    raw = json.loads(sys.argv[1])
    # mgmt /get returns a bare JSON ARRAY of messages (not an object)
    msgs = raw if isinstance(raw, list) else raw.get("messages", [])
except Exception:
    msgs = []
m = next((x for x in msgs if sys.argv[2] in (x.get("payload") or "")), None)
if m is None:
    print("missing 0 ")
else:
    h = (m.get("properties") or {}).get("headers") or {}
    print("found", h.get("attempt", "none"), str(h.get("x-failure-reason", ""))[:90])
PY
)
set -- $D_LINE
assert_eq "found" "${1:-}" "dead queue holds our build message"
assert_eq "2" "${2:-}" "attempt header = 2 (deliveries 0, 1, 2 then dead)"
assert_contains "${*:3}" "forced transient error" "dead message carries the failure reason"

compose up -d --no-build --scale worker=2 worker >/dev/null 2>&1
WORKER_DIRTY=0
wait_for 60 "workers restored (FORCE_TRANSIENT off)" workers_running 2

finish

#!/usr/bin/env bash
# ── Scenario (plan §11): duplicate messages are no-ops ──────────────────
# Deploy normally, then hand RabbitMQ the SAME build message a second time
# (published straight to the build exchange via the management API).
# Expect: nothing changes — deployment stays "live", app keeps serving.
# The worker's claim logic must detect "already done" and ack without doing
# any work (this is what makes crashed/retried deliveries safe).
. "$(dirname "${BASH_SOURCE[0]}")/common.sh"
zip_dir hello-node

say "idempotency (duplicate build message)"
CODE=$(curl -sS -o /dev/null -w '%{http_code}' --max-time 5 -u "zeroops:$RBP" \
  "http://127.0.0.1:15672/api/queues/%2f/build.q")
if [ "$CODE" = 200 ]; then ok "rabbitmq management reachable"; else bad "mgmt API unreachable (code=$CODE)"; fi

signup u "u-${RUN}@zeroops.test"
create_proj u "Idem ${RUN}" "${RUN}-idem" && PID=$NEW_PID
upload u "$PID" fixtures/generated/hello-node.zip && DEP=$NEW_DEP
wait_for 300 "deployment live" dep_is u "$DEP" live

# re-publish the same build message — worker must skip it (status already built)
curl -sS -u "zeroops:$RBP" -H 'Content-Type: application/json' \
  -X POST "http://127.0.0.1:15672/api/exchanges/%2f/build.x/publish" \
  -d "{\"routing_key\":\"build\",\"payload\":\"{\\\"type\\\":\\\"build\\\",\\\"deployment_id\\\":\\\"$DEP\\\"}\",\"payload_encoding\":\"string\"}" >/dev/null
sleep 5
assert_eq "live" "$(dep_status u "$DEP")" "deployment untouched by duplicate message"
app_req "${RUN}-idem" /
assert_eq 200 "$CODE" "app still serves after duplicate delivery"

finish

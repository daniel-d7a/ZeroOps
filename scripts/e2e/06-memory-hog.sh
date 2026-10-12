#!/usr/bin/env bash
# ── Fixture 6 (plan §11.6): container memory limit ──────────────────────
# Scenario: two projects — a normal "neighbour" app that must stay up, and
#           a hog that allocates past the 1 GiB container limit.
# Expect:   Docker kills the hog's container (OOM), the platform API stays
#           healthy, and the neighbour app is still serving. Resource
#           limits must not leak across projects.
. "$(dirname "${BASH_SOURCE[0]}")/common.sh"
zip_dir memory-hog
zip_dir hello-node

say "memory-hog fixture"
signup u "u-${RUN}@zeroops.test"

# neighbour app that must survive the OOM kill
create_proj u "Neighbour ${RUN}" "${RUN}-nb" && NB_PID=$NEW_PID
upload u "$NB_PID" fixtures/generated/hello-node.zip && NB_DEP=$NEW_DEP
wait_for 300 "neighbour live" dep_is u "$NB_DEP" live

create_proj u "Hog ${RUN}" "${RUN}-hog" && PID=$NEW_PID
upload u "$PID" fixtures/generated/memory-hog.zip && DEP=$NEW_DEP
wait_for 240 "memory-hog deployment terminal" dep_terminal u "$DEP"
sleep 5
if [ "$(docker inspect -f '{{.State.Running}}' "app-$DEP" 2>/dev/null)" = "true" ]; then
  bad "container still running"
else
  ok "container not running (killed by Docker)"
fi
req GET /api/health
assert_eq 200 "$CODE" "platform healthy after memory-hog"
app_req "${RUN}-nb" /
assert_eq 200 "$CODE" "neighbour app unaffected"

finish

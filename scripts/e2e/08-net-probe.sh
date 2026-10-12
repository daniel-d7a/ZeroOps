#!/usr/bin/env bash
# ── Fixture 8 (plan §11.8): network isolation between projects ──────────
# Scenario: project A ("victim") runs a normal app; project B ("probe") is
#           given A's container name (app-<deploymentId>) as TARGET_URL and
#           an endpoint that tries to fetch it.
# Expect:   the probe app answers 200 with {"reachable":false} — Docker's
#           per-project network (net-<projectId>) must make cross-project
#           container traffic impossible.
. "$(dirname "${BASH_SOURCE[0]}")/common.sh"
zip_dir hello-node
zip_dir net-probe

say "net-probe fixture (network isolation)"
signup u "u-${RUN}@zeroops.test"

# victim: a live hello-node container on its own network
create_proj u "Victim ${RUN}" "${RUN}-vic" && VIC_PID=$NEW_PID
upload u "$VIC_PID" fixtures/generated/hello-node.zip && VIC_DEP=$NEW_DEP
wait_for 300 "victim live" dep_is u "$VIC_DEP" live
app_req "${RUN}-vic" /
assert_eq 200 "$CODE" "victim is serving"

# probe: tries to reach the victim's container by name
create_proj u "Probe ${RUN}" "${RUN}-prb" && PID=$NEW_PID
as_user u
reqj PUT "/api/projects/$PID/env" --data "{\"set\":{\"TARGET_URL\":\"http://app-$VIC_DEP:3000\"}}"
assert_code 200 "TARGET_URL env set"
upload u "$PID" fixtures/generated/net-probe.zip && DEP=$NEW_DEP
wait_for 300 "probe deployment live" dep_is u "$DEP" live

app_req "${RUN}-prb" /probe
assert_eq 200 "$CODE" "/probe responds"
assert_contains "$(cat "$BODY")" '"reachable":false' "cross-project probe is blocked"

finish

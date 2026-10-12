#!/usr/bin/env bash
# ── Fixture 7 (plan §11.7): zip-slip attack rejected ────────────────────
# Scenario: a crafted zip whose entries contain "../" (zip-slip: trying to
#           write files outside the build directory).
# Expect:   the safe-unzip step refuses it — deployment → "failed" with
#           "escapes the destination" in the error. The message is in
#           deployments.error, NOT in the log stream (it fails before any
#           log lines exist).
. "$(dirname "${BASH_SOURCE[0]}")/common.sh"
zip_slip_craft

say "zip-slip fixture"
signup u "u-${RUN}@zeroops.test"
create_proj u "Slip ${RUN}" "${RUN}-slip" && PID=$NEW_PID
upload u "$PID" fixtures/generated/zip-slip.zip && DEP=$NEW_DEP
wait_for 180 "deployment failed" dep_is u "$DEP" failed
assert_contains "$(dep_field u "$DEP" error)" "escapes the destination" "rejected with traversal error"

finish

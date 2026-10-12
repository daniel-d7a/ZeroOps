#!/usr/bin/env bash
# ── Fixture 4 (plan §11.4): build fails cleanly ─────────────────────────
# Scenario: the app's package.json depends on a package that does not exist,
#           so `npm install` during the build must fail.
# Expect:   deployment → "failed" with an error recorded; it must fail FAST
#           (<90s — a broken build is a permanent error, so the 3× retry
#           ladder with 30s waits must NOT run); the failing package name
#           must be visible in the build logs the user can read.
. "$(dirname "${BASH_SOURCE[0]}")/common.sh"
zip_dir broken-build

say "broken-build fixture"
signup u "u-${RUN}@zeroops.test"
create_proj u "Broken ${RUN}" "${RUN}-broken" && PID=$NEW_PID
T0=$SECONDS
upload u "$PID" fixtures/generated/broken-build.zip && DEP=$NEW_DEP
wait_for 180 "deployment failed" dep_is u "$DEP" failed
ELAPSED=$((SECONDS - T0))
BERR=$(dep_field u "$DEP" error)
[ -n "$BERR" ] && ok "records an error" || bad "error field empty"
# a retry ladder would burn ≥60 s in TTL waits alone
if [ "$ELAPSED" -lt 90 ]; then ok "failed on first attempt, no retries (${ELAPSED}s)"; else bad "took ${ELAPSED}s — retry ladder ran?"; fi
req GET "/api/deployments/$DEP/logs?after=0"
LOGS=$(python3 -c 'import json,sys; d=json.load(open(sys.argv[1])); print("".join(i["line"] for i in d.get("items",[])))' "$BODY" 2>/dev/null)
assert_contains "$LOGS" "zeroops-package-that-is-not-published-anywhere" "build logs show the failing dependency"

finish

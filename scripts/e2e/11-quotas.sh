#!/usr/bin/env bash
# ── Scenario (plan §11): quotas ─────────────────────────────────────────
# The cheapest file in the suite — no uploads, no builds, only API calls:
#   - max 3 projects per user: the 4th create → 403
#   - max 50 env vars per project: 1 + 49 bulk = 50 OK, the 51st → 403
#   - reserved keys (DATABASE_URL) are rejected outright with 400
. "$(dirname "${BASH_SOURCE[0]}")/common.sh"

say "quotas"
signup u "u-${RUN}@zeroops.test"

create_proj u "Q1 ${RUN}" "${RUN}-q1"
create_proj u "Q2 ${RUN}" "${RUN}-q2" && PID=$NEW_PID
create_proj u "Q3 ${RUN}" "${RUN}-q3"
reqj POST /api/projects --data "{\"name\":\"Q4 ${RUN}\",\"slug\":\"${RUN}-q4\"}"
assert_code 403 "4th project → 403 project quota"

# env vars: 1 + 49 bulk = 50, the 51st must fail
reqj PUT "/api/projects/$PID/env/FIRST" --data '{"value":"1"}'
assert_code 200 "first env var"
python3 - "$TMP/env49.json" <<'PY'
import json, sys
json.dump({"set": {f"K{i:02d}": "v" for i in range(1, 50)}}, open(sys.argv[1], "w"))
PY
reqj PUT "/api/projects/$PID/env" --data @"$TMP/env49.json"
assert_code 200 "bulk set 49 vars (total 50)"
reqj PUT "/api/projects/$PID/env/VAR51" --data '{"value":"x"}'
assert_code 403 "51st env var → 403 quota"

# reserved keys are not counted as quota — they're rejected outright
reqj PUT "/api/projects/$PID/env/DATABASE_URL" --data '{"value":"x"}'
assert_code 400 "reserved key DATABASE_URL → 400"

finish

#!/usr/bin/env bash
# ── Fixture 2 (plan §11.2): managed database round trip ─────────────────
# Scenario: create the project with wantsDb=true, upload an app that reads
#           DATABASE_URL and exposes GET /db.
# Expect:   deployment goes live; GET /db returns {"ok":true} (the app
#           connected to its Postgres and ran a query); the API reports the
#           database as "ready".
. "$(dirname "${BASH_SOURCE[0]}")/common.sh"
zip_dir node-with-db

say "node-with-db fixture"
signup u "u-${RUN}@zeroops.test"
create_proj u "Db ${RUN}" "${RUN}-db" withdb && PID=$NEW_PID
upload u "$PID" fixtures/generated/node-with-db.zip && DEP=$NEW_DEP
wait_for 420 "deployment live" dep_is u "$DEP" live
app_req "${RUN}-db" /db
assert_eq 200 "$CODE" "/db responds"
assert_contains "$(cat "$BODY")" '"ok":true' "database round trip works"
req GET "/api/projects/$PID/database"
assert_eq "ready" "$(jget "$BODY" status)" "database provisioning reports ready"

finish

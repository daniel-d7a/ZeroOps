#!/usr/bin/env bash
# ── Scenario (plan §10): sessions, access control, input validation ─────
# One file covering the HTTP security checklist, in groups:
#   1. cookie flags (HttpOnly, SameSite) + login regenerates the session id
#      (blocks session fixation) + logout really destroys the session
#   2. state changes without the X-CSRF-Token header → 403
#   3. admin endpoints reject normal users; admin can list users
#   4. ownership: another user's project/env/deployment → 404 (never 403,
#      so attackers can't tell that an id exists)
#   5. uploads: non-zip bytes → 400, oversized file → 413
#   6. env rules: reserved key PORT → 400, empty value → 400
#   7. admin disables a user → their live session gets 401 immediately
. "$(dirname "${BASH_SOURCE[0]}")/common.sh"
zip_dir hello-node

say "sessions and access control"
DISP_EMAIL="disp-${RUN}@zeroops.test"
DISP_ID=$(register_user "$DISP_EMAIL")
login_user disp "$DISP_EMAIL" "$P_PASS"

# cookie flags
HDRS=$(curl -sS -i -X POST -H "Host: $CONSOLE" -H 'Content-Type: application/json' \
  --data "{\"email\":\"$DISP_EMAIL\",\"password\":\"$P_PASS\"}" "$BASE/api/auth/login")
assert_contains "$HDRS" "HttpOnly" "session cookie is HttpOnly"
assert_contains "$HDRS" "SameSite=Lax" "session cookie is SameSite=Lax"

# session id regenerated on login (fixation defence)
FORGED='s%3Aforged.evilsignature'
curl -sS -i -o "$TMP/fixlogin" -c "$TMP/jar-fix" -H "Host: $CONSOLE" -H 'Content-Type: application/json' \
  -H "Cookie: connect.sid=$FORGED" \
  --data "{\"email\":\"$DISP_EMAIL\",\"password\":\"$P_PASS\"}" "$BASE/api/auth/login" >/dev/null
NEWSID=$(grep -io 'connect.sid=[^;]*' "$TMP/fixlogin" | head -1 | cut -d= -f2-)
if [ -n "$NEWSID" ] && [ "$NEWSID" != "$FORGED" ]; then
  ok "login issues a new session id (fixation defence)"
else
  bad "session id not regenerated (got '$NEWSID')"
fi
CODE=$(curl -sS -o "$BODY" -w '%{http_code}' -H "Host: $CONSOLE" -H "Cookie: connect.sid=$NEWSID" "$BASE/api/auth/me")
assert_eq 200 "$CODE" "new session id works"
CODE=$(curl -sS -o "$BODY" -w '%{http_code}' -H "Host: $CONSOLE" -H "Cookie: connect.sid=$FORGED" "$BASE/api/auth/me")
assert_eq 401 "$CODE" "forged session id rejected"

# CSRF header enforced
CSRF_SAVED=$CSRF; CSRF=""
reqj POST /api/projects --data "{\"name\":\"csrf probe\",\"slug\":\"${RUN}-csrf\"}"
assert_code 403 "state change without CSRF header → 403"
CSRF=$CSRF_SAVED

# logout invalidates the session
as_user disp
req POST /api/auth/logout
assert_code 204 "logout"
req GET /api/auth/me
assert_eq 401 "$CODE" "session dead after logout"
login_user disp "$DISP_EMAIL" "$P_PASS"
req GET /api/auth/me
assert_eq 200 "$CODE" "re-login works"

# admin endpoints: normal user 403, admin 200
register_user "nadm-${RUN}@zeroops.test" >/dev/null
login_user n "nadm-${RUN}@zeroops.test" "$P_PASS"
req GET /api/admin/users
assert_eq 403 "$CODE" "normal user rejected from admin endpoints"
if login_user admin "$ADMIN_EMAIL" "$ADMIN_PASSWORD"; then
  req GET /api/admin/users
  assert_eq 200 "$CODE" "admin can list users"
fi

# ownership: another user's project/env/deployment → 404 (never 403)
register_user "own-${RUN}@zeroops.test" >/dev/null
login_user owner "own-${RUN}@zeroops.test" "$P_PASS"
create_proj owner "Owned ${RUN}" "${RUN}-own" && OPID=$NEW_PID
upload owner "$OPID" fixtures/generated/hello-node.zip && ODEP=$NEW_DEP
wait_for 300 "owner deployment live" dep_is owner "$ODEP" live
as_user n
req GET "/api/projects/$OPID"
assert_eq 404 "$CODE" "foreign project → 404"
req GET "/api/projects/$OPID/env"
assert_eq 404 "$CODE" "foreign env → 404"
req GET "/api/deployments/$ODEP"
assert_eq 404 "$CODE" "foreign deployment → 404"

# upload validation: magic bytes + size
as_user owner
echo "not a zip" > "$TMP/notzip.txt"
req POST "/api/projects/$OPID/deployments" -F "file=@$TMP/notzip.txt"
assert_code 400 "non-zip upload → 400"
truncate -s 52428801 "$TMP/big.zip"
req POST "/api/projects/$OPID/deployments" -F "file=@$TMP/big.zip"
assert_code 413 "oversized upload → 413"

# env rules: reserved keys and empty values refused
reqj PUT "/api/projects/$OPID/env/PORT" --data '{"value":"999"}'
assert_code 400 "reserved key PORT → 400"
reqj PUT "/api/projects/$OPID/env/EMPTY_OK" --data '{"value":""}'
assert_code 400 "empty value → 400"

# disabled user loses access immediately
as_user admin
reqj PATCH "/api/admin/users/$DISP_ID" --data '{"status":"disabled"}'
assert_code 200 "admin disables the disposable user"
as_user disp
req GET /api/auth/me
assert_code 401 "disabled user's session rejected immediately"

finish

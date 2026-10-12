#!/usr/bin/env bash
# Shared helpers for the split e2e suite (plan §11). Sourced by scripts/e2e/*.sh.
#
# ── Reading guide (section order below) ─────────────────────────────────
#   1. setup      — cd to repo root, temp dir, .env secrets, compose files
#   2. output     — say/ok/bad/assert_*: every check prints one indented line
#                   and bumps PASS/FAIL; finish prints the summary + exit code
#   3. HTTP       — req/reqj/app_req: curl wrappers that fill $CODE (status)
#                   and $BODY (response file) and handle cookies + CSRF
#   4. users      — signup/login_user/as_user: per-user cookie jars, CSRF
#                   tokens, 429-throttle waits
#   5. projects   — create_proj/upload/dep_*/proj_*/wait_action: API calls
#                   that store the new id in $NEW_PID/$NEW_DEP
#   6. fixtures   — zip_dir/zip_slip_craft: build the .zip files to upload
#   7. preflight  — runs at source time: abort the file if the stack is down
#
# ── Bash idioms used everywhere in this suite ───────────────────────────
#   set -uo pipefail   -u: referencing an unset variable aborts the script
#                       (so ids are pre-initialized to "" — see bottom)
#   . path             "source": run that file's code in *this* shell, so
#                       its functions/variables become ours
#   foo && BAR=$x      run BAR=$x only when foo succeeded (skip on failure)
#   $(command)         command substitution: runs the command, pastes its
#                       output as a string
#   local              declare a function-local variable
#   $SECONDS           built-in: seconds since this script started — used
#                       for deadlines: end=$((SECONDS + timeout))
#   declare -A X       an associative array (a.k.a. map/dict): X[key]=value
set -uo pipefail
E2E_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$(cd "$E2E_DIR/../.." && pwd)"

HOST_PORT=${ZEROOPS_PORT:-8080}
BASE="http://127.0.0.1:${HOST_PORT}"
CONSOLE="console.localhost"
RUN="e$(date +%s)-$$"          # unique per file process → no slug clashes between runs
TMP=$(mktemp -d /tmp/opencode/e2e.XXXXXX)
BODY="$TMP/body"
PASS=0; FAIL=0; FAILURES=()

envval() { grep -m1 "^$1=" .env 2>/dev/null | cut -d= -f2-; }
DBU=$(envval PLATFORM_DB_USER); DBU=${DBU:-zeroops}
DBN=$(envval PLATFORM_DB_NAME); DBN=${DBN:-zeroops}
RBP=$(envval RABBITMQ_DEFAULT_PASSWORD)
ADMIN_EMAIL=$(envval ADMIN_EMAIL); ADMIN_EMAIL=${ADMIN_EMAIL:-admin@localhost.com}
ADMIN_PASSWORD=$(envval ADMIN_PASSWORD); ADMIN_PASSWORD=${ADMIN_PASSWORD:-change-me-now}
P_PASS="pa55word-e2e"

# --- compose: detect the dev overlay (bind-mounted /app = dev) -------------
compose_files=(-f docker-compose.yml)
API_CID=$(docker compose -f docker-compose.yml ps -q api 2>/dev/null | head -1)
if [ -n "$API_CID" ] && docker inspect -f '{{range .Mounts}}{{println .Source}}{{end}}' "$API_CID" 2>/dev/null | grep -q "/services/api$"; then
  compose_files+=(-f docker-compose.dev.yml)
fi
compose() { docker compose "${compose_files[@]}" "$@"; }

# tests that scale/kill the worker set this so a crashed run still restores it
WORKER_DIRTY=0
trap '[ "$WORKER_DIRTY" = 1 ] && compose up -d --no-build --scale worker=2 worker >/dev/null 2>&1' EXIT

# --- output ----------------------------------------------------------------
say()  { printf '\n== %s\n' "$*"; }
ok()   { PASS=$((PASS + 1)); printf '  ok   %s\n' "$*"; }
bad()  { FAIL=$((FAIL + 1)); FAILURES+=("$*"); printf '  FAIL %s\n' "$*"; }
finish() {
  printf '%s: %d passed, %d failed\n' "${0##*/}" "$PASS" "$FAIL"
  if [ "$FAIL" -gt 0 ]; then
    for f in "${FAILURES[@]}"; do printf '  - %s\n' "$f"; done
    exit 1
  fi
  exit 0
}

assert_code() { # <want> <desc>
  if [ "$CODE" = "$1" ]; then ok "$2"; else bad "$2 (code=$CODE body=$(head -c 240 "$BODY"))"; fi
}
assert_contains() { # <haystack> <needle> <desc>
  case "$1" in *"$2"*) ok "$3" ;; *) bad "$3 (missing '$2' in: $(printf '%.240s' "$1"))" ;; esac
}
assert_eq() { # <want> <got> <desc>
  if [ "$1" = "$2" ]; then ok "$3"; else bad "$3 (want '$1' got '$2')"; fi
}
wait_for() { # <timeout> <desc> <fn> [args...] — polls fn every 1s
  # prints a heartbeat every 15s so long waits never look like a hang:
  #   ... waiting: r1 deployment live (30s)
  local t=$1 desc=$2 start next end; shift 2
  start=$SECONDS; next=15; end=$((SECONDS + t))
  while ((SECONDS < end)); do
    if "$@"; then ok "$desc"; return 0; fi
    if ((SECONDS - start >= next)); then
      printf '  ... waiting: %s (%ds)\n' "$desc" "$((SECONDS - start))"
      next=$((next + 15))
    fi
    sleep 1
  done
  bad "timeout: $desc"
  return 1
}
workers_running() { [ "$(compose ps --status running -q worker 2>/dev/null | wc -l)" -ge "${1:-1}" ]; }

jget() { # <file> <dotted.path.with.[i]> — prints '' on any miss
  python3 - "$1" "$2" <<'PY'
import json, sys
try:
    o = json.load(open(sys.argv[1]))
    for p in sys.argv[2].replace("[", ".").replace("]", "").split("."):
        if p:
            o = o[int(p)] if isinstance(o, list) else o[p]
    print("" if o is None else o)
except Exception:
    print("")
PY
}

# --- HTTP ------------------------------------------------------------------
JAR=""; CSRF=""; CODE=""
req() { # <METHOD> <PATH> [curl args...] → $CODE, $BODY
  local m=$1 p=$2; shift 2
  local args=(-sS -o "$BODY" -w '%{http_code}' -X "$m" -H "Host: $CONSOLE")
  [ -n "$JAR" ] && args+=(-b "$JAR" -c "$JAR")
  [ -n "$CSRF" ] && args+=(-H "X-CSRF-Token: $CSRF")
  CODE=$(curl "${args[@]}" "$@" "$BASE$p") || CODE=000
}
reqj() { req "$@" -H 'Content-Type: application/json'; }
app_req() { # <slug> <path>
  CODE=$(curl -sS -o "$BODY" -w '%{http_code}' -H "Host: $1.localhost" "$BASE$2") || CODE=000
}

declare -A JARS CSRF_MAP
login_user() { # <key> <email> <password> — waits out a 429 (throttler 10/min per route)
  local data="{\"email\":\"$2\",\"password\":\"$3\"}"
  JARS[$1]="$TMP/jar-$1"
  JAR=${JARS[$1]}; CSRF=""
  reqj POST /api/auth/login --data "$data"
  [ "$CODE" = 429 ] && { sleep 61; reqj POST /api/auth/login --data "$data"; }
  if [ "$CODE" != 200 ] && [ "$CODE" != 201 ]; then bad "login $1 (code=$CODE $(head -c 160 "$BODY"))"; return 1; fi
  CSRF_MAP[$1]=$(jget "$BODY" csrfToken)
  as_user "$1"
}
as_user() { JAR=${JARS[$1]:-}; CSRF=${CSRF_MAP[$1]:-}; }
register_user() { # <email> — echoes the new user id (waits out a 429)
  local data="{\"email\":\"$1\",\"password\":\"$P_PASS\"}"
  reqj POST /api/auth/register --data "$data"
  [ "$CODE" = 429 ] && { sleep 61; reqj POST /api/auth/register --data "$data"; }
  jget "$BODY" id
}
signup() { # <key> <email> — register (email unique per RUN) then login
  register_user "$2" >/dev/null
  login_user "$1" "$2" "$P_PASS"
}

NEW_PID=""; NEW_DEP=""
create_proj() { # <userkey> <name> <slug> [withdb]
  as_user "$1"
  local data="{\"name\":\"$2\",\"slug\":\"$3\"}"
  [ -n "${4:-}" ] && data="{\"name\":\"$2\",\"slug\":\"$3\",\"wantsDb\":true}"
  reqj POST /api/projects --data "$data"
  NEW_PID=$(jget "$BODY" id)
  if [ "$CODE" != 201 ] || [ -z "$NEW_PID" ]; then bad "create project $3 (code=$CODE $(head -c 200 "$BODY"))"; NEW_PID=""; return 1; fi
}
upload() { # <userkey> <pid> <zip>
  [ -z "${2:-}" ] && { bad "upload skipped: no project id"; return 1; }
  as_user "$1"
  req POST "/api/projects/$2/deployments" -F "file=@$3"
  NEW_DEP=$(jget "$BODY" id)
  if [ "$CODE" != 201 ] || [ -z "$NEW_DEP" ]; then bad "upload to $2 (code=$CODE $(head -c 200 "$BODY"))"; NEW_DEP=""; return 1; fi
}

dep_status() { as_user "$1"; req GET "/api/deployments/$2"; jget "$BODY" status; }
dep_is() { [ "$(dep_status "$1" "$2")" = "$3" ]; }
dep_field() { as_user "$1"; req GET "/api/deployments/$2"; jget "$BODY" "$3"; }
dep_terminal() { case "$(dep_status "$1" "$2")" in live|failed|superseded) return 0 ;; *) return 1 ;; esac; }
proj_is() { as_user "$1"; req GET "/api/projects/$2"; [ "$(jget "$BODY" status)" = "$3" ]; }
proj_field() { as_user "$1"; req GET "/api/projects/$2"; jget "$BODY" "$3"; }

ACTION_STATUS=""; ACTION_ERROR=""
wait_action() { # <userkey> <pid> <type> <timeout>
  local u=$1 pid=$2 type=$3 t=$4 s x
  local end=$((SECONDS + t))
  ACTION_STATUS=""; ACTION_ERROR=""
  while ((SECONDS < end)); do
    as_user "$u"; req GET "/api/projects/$pid/actions"
    x=$(jget "$BODY" items[0].type); s=$(jget "$BODY" items[0].status)
    if [ "$x" = "$type" ] && { [ "$s" = "done" ] || [ "$s" = "failed" ]; }; then
      ACTION_STATUS=$s; ACTION_ERROR=$(jget "$BODY" items[0].error); return 0
    fi
    sleep 1
  done
  bad "timeout waiting for action $type on $pid"
  return 1
}

# --- fixtures --------------------------------------------------------------
zip_dir() { # <fixture-name> → fixtures/generated/<name>.zip
  mkdir -p fixtures/generated
  python3 - "fixtures/$1" "fixtures/generated/$1.zip" <<'PY'
import os, sys, zipfile
d, out = sys.argv[1], sys.argv[2]
with zipfile.ZipFile(out, "w", zipfile.ZIP_DEFLATED) as z:
    for root, _, files in os.walk(d):
        for f in files:
            p = os.path.join(root, f)
            z.write(p, os.path.relpath(p, d))
PY
}
zip_slip_craft() { # → fixtures/generated/zip-slip.zip (entries with ../)
  mkdir -p fixtures/generated
  python3 - <<'PY'
import zipfile
with zipfile.ZipFile("fixtures/generated/zip-slip.zip", "w") as z:
    z.writestr("package.json", '{"name":"zip-slip","main":"server.js"}')
    z.writestr("../evil.js", "console.log('escaped')")
PY
}

# --- preflight (every file) ------------------------------------------------
req GET /api/health
[ "$CODE" = 200 ] || { echo "api unhealthy (code=$CODE) — is the stack up?" >&2; exit 1; }
docker info >/dev/null 2>&1 || { echo "docker daemon unreachable" >&2; exit 1; }

# ids start empty so `set -u` never aborts a half-run file
PID=""; DEP=""; SLUG=""; OPID=""; ODEP=""; DISP_ID=""
HELLO_PID=""; HELLO_DEP=""; SLUG_HELLO=""

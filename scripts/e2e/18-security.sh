#!/usr/bin/env bash
# ── Scenario (plan §10): security checklist ─────────────────────────────
# The HTTP/session half of §10 lives in 10-sessions.sh and cross-project
# reach in 08-net-probe.sh; this file covers the rest:
#   1. docker socket: only worker (rw) and traefik (read-only) mount it —
#      checked against `docker compose config` (the fully-resolved stack)
#   2. student-db admin credentials exist only in the worker's environment
#   3. the only published host ports are traefik (8080) and the RabbitMQ
#      UI (127.0.0.1 only)
#   4. the live student container: joins only its own net-<projectId>,
#      no docker socket mount
#   5. env value stored as ciphertext in the DB (not plaintext)
#   6. that plaintext never appears in api/worker logs
#   7. secrets: .env untracked and no secret value inside any tracked file
# It deploys one small app to have a real container/network/DB row to inspect.
. "$(dirname "${BASH_SOURCE[0]}")/common.sh"
zip_dir hello-node

say "security checklist (§10)"

# one live student app + one encrypted env value to inspect
signup u "u-${RUN}@zeroops.test"
create_proj u "Sec ${RUN}" "${RUN}-sec" && PID=$NEW_PID
as_user u
reqj PUT "/api/projects/$PID/env" --data '{"set":{"GREETING":"hello-e2e"}}'
assert_code 200 "env var set for encryption check"
upload u "$PID" fixtures/generated/hello-node.zip && DEP=$NEW_DEP
wait_for 300 "deployment live" dep_is u "$DEP" live

compose config --format json > "$TMP/cfg.json"

# 1. Docker socket: only worker (rw) and traefik (read-only)
SOCKS=$(python3 -c '
import json, sys
c = json.load(open(sys.argv[1]))
out = []
for n, s in c["services"].items():
    for v in s.get("volumes") or []:
        if v.get("type") == "bind" and str(v.get("source", "")).endswith("docker.sock"):
            mode = "ro" if v.get("read_only") else "rw"
            out.append(n + ":" + mode)
print(" ".join(sorted(out)))' "$TMP/cfg.json")
assert_eq "traefik:ro worker:rw" "$SOCKS" "only worker and traefik(ro) mount the docker socket"

# 2. student DB admin credentials only in the worker's environment
DBENV=$(python3 -c '
import json, sys
c = json.load(open(sys.argv[1]))
print(" ".join(sorted(n for n, s in c["services"].items()
      if any(str(k).startswith("STUDENT_DB_") for k in (s.get("environment") or {})))))' "$TMP/cfg.json")
assert_eq "worker" "$DBENV" "student DB credentials exist only in worker env"

# 3. no host ports besides Traefik (8080) and the RabbitMQ UI (127.0.0.1)
PORTS=$(python3 -c '
import json, sys
c = json.load(open(sys.argv[1]))
out = []
for n, s in c["services"].items():
    for p in s.get("ports") or []:
        out.append(n + ":" + (p.get("host_ip") or "") + ":" + str(p.get("published")))
print(" ".join(sorted(out)))' "$TMP/cfg.json")
assert_eq "rabbitmq:127.0.0.1:15672 traefik::8080" "$PORTS" "only traefik + local rabbitmq UI are published"

# 4. student container: own network only, no socket, no unexpected mounts
NETS=$(docker inspect -f '{{range $k, $v := .NetworkSettings.Networks}}{{$k}} {{end}}' "app-$DEP" | xargs)
assert_eq "net-$PID" "$NETS" "student container joins only its own net-<projectId>"
MOUNTS=$(docker inspect -f '{{range .Mounts}}{{.Destination}} {{end}}' "app-$DEP")
case "$MOUNTS" in
  *docker.sock*) bad "student container mounts the docker socket ($MOUNTS)" ;;
  *) ok "student container has no docker socket" ;;
esac

# 5. env value encrypted at rest
DBP=$(envval PLATFORM_DB_PASSWORD)
ENC=$(compose exec -T -e PGPASSWORD="$DBP" platform-db \
  psql -U "${DBU:-zeroops}" -d "${DBN:-zeroops}" -tAc \
  "SELECT value_encrypted FROM env_vars WHERE project_id='$PID' AND key='GREETING'" 2>/dev/null | tr -d '[:space:]')
if [ -z "$ENC" ]; then
  bad "env row not found in platform-db"
else
  case "$ENC" in
    *hello-e2e*) bad "env value stored as plaintext: $ENC" ;;
    *) ok "env value stored as ciphertext (len=${#ENC})" ;;
  esac
fi

# 6. the plaintext never appears in api/worker logs
if compose logs --since 60m api worker 2>/dev/null | grep -qF "hello-e2e"; then
  bad "plaintext env value found in api/worker logs"
else
  ok "env value never appears in api/worker logs"
fi

# 7. secrets come from env, are not committed
[ -z "$(git ls-files .env)" ] && ok ".env is not tracked" || bad ".env is tracked by git"
[ -z "$(git status --porcelain -- .env)" ] && ok ".env clean (ignored, unmodified)" || bad ".env shows in git status"
LEAKED=""
for k in SESSION_SECRET ENCRYPTION_KEY PLATFORM_DB_PASSWORD RABBITMQ_DEFAULT_PASSWORD STUDENT_DB_PASSWORD; do
  v=$(envval "$k")
  [ -n "$v" ] || continue
  git grep -qF -- "$v" 2>/dev/null && LEAKED="$LEAKED $k"
done
assert_eq "" "$LEAKED" "no secret values inside tracked files"

finish

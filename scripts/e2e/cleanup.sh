#!/usr/bin/env bash
# Reset student-test leftovers: app-<deploymentId> containers and their
# net-<projectId> networks.
#
# Why this exists: Docker's default address pool is small (~30 custom bridge
# networks, each net-* takes a whole /16) and rounds never cleaned up — after a
# while every deploy died with "all predefined address pools have been fully
# subnetted". The runner calls this before the round, after every file (keeps
# peak at one file's max ≈10 networks), and on EXIT; manual use:
#   bash scripts/e2e/cleanup.sh
# Note: removes ALL net-* networks and app-* containers — test data only.
set -uo pipefail

removed_c=0; removed_n=0; kept=""

# 1. student containers (app-<deploymentId>), running or not
while read -r id; do
  [ -n "$id" ] || continue
  docker rm -f "$id" >/dev/null 2>&1 && removed_c=$((removed_c + 1))
done < <(docker ps -aq --filter "name=app-")

# 2. platform containers hold endpoints on student nets — detach, then remove
detach_ids=$(docker ps -q --filter "name=traefik")
detach_ids="$detach_ids $(docker ps -q --filter "name=student-db")"
detach_ids="$detach_ids $(docker ps -q --filter "name=worker")"
for net in $(docker network ls --format '{{.Name}}' | grep -E '^net-'); do
  for id in $detach_ids; do
    docker network disconnect -f "$net" "$id" >/dev/null 2>&1
  done
  if docker network rm "$net" >/dev/null 2>&1; then
    removed_n=$((removed_n + 1))
  else
    kept="$kept $net"
  fi
done

echo "cleanup: removed $removed_c app containers, $removed_n net-* networks"
[ -n "$kept" ] && echo "cleanup: kept (still in use):$kept"

# 3. stop reviving old test projects: the startup reconcile (plan §8.7)
# starts every desired-running project whose container is gone. Cleanup just
# removed those containers, so mark the rows stopped too. Best effort: the
# stack may already be down when this runs manually.
DBU=$(grep -m1 '^PLATFORM_DB_USER=' .env 2>/dev/null | cut -d= -f2-)
DBN=$(grep -m1 '^PLATFORM_DB_NAME=' .env 2>/dev/null | cut -d= -f2-)
if docker compose exec -T platform-db psql -U "${DBU:-zeroops}" -d "${DBN:-zeroops}" \
  -c "UPDATE projects SET desired_state = 'stopped' WHERE desired_state = 'running'" >/dev/null 2>&1; then
  echo "cleanup: marked running projects stopped"
fi

# --- headroom report: how close are we to pool exhaustion? ------------------
left=$(docker network ls --format '{{.Name}}' | grep -cE '^net-')
bridges=$(docker network ls --format '{{.Name}} {{.Driver}}' \
  | awk '$2 == "bridge" && $1 != "bridge"' | wc -l)
echo "networks: net-*=$left, custom bridges total=$bridges"
# ground truth over thresholds: can the pool still hand out a network?
docker network rm zo-pool-probe >/dev/null 2>&1 # leaked probe from an interrupted run
if tid=$(docker network create --driver bridge zo-pool-probe 2>/dev/null); then
  docker network rm "$tid" >/dev/null 2>&1
  echo "network pool: OK (still allocates)"
else
  echo "network pool: EXHAUSTED — new deploys will fail with 'address pools fully subnetted'"
fi
exit 0

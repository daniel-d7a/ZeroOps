#!/usr/bin/env bash
# ── Scenario (plan §11): parallel workers ───────────────────────────────
# LAST file in the suite — the longest test (~2-4 min), run it by name if
# you're in a hurry: ./scripts/e2e.sh replicas
#
# Scale the worker service to 3 replicas, then push 10 projects through 4
# users (split 3/3/2/2 — nobody exceeds the 3-project quota).
# Expect: all 10 deployments reach "live", each with EXACTLY ONE running
# container (nobody double-processed a job). The file restores the normal
# 2-worker scale afterwards (and the trap in common.sh restores it even if
# this file crashes).
. "$(dirname "${BASH_SOURCE[0]}")/common.sh"
zip_dir hello-node

say "replicas (3 workers, 10 projects)"
WORKER_DIRTY=1
compose up -d --no-build --scale worker=3 worker >/dev/null 2>&1
wait_for 60 "3 workers running" workers_running 3

users=()
for i in 1 2 3 4; do
  e="rep${i}-${RUN}@zeroops.test"
  register_user "$e" >/dev/null
  login_user "r$i" "$e" "$P_PASS" || bad "register/login replica user $i"
  users+=("r$i")
done

pids=()
deps=()
for i in $(seq 1 10); do
  ukey=${users[$(((i - 1) % 4))]}
  create_proj "$ukey" "R${i} ${RUN}" "${RUN}-r${i}"
  pids+=("$NEW_PID")
  upload "$ukey" "$NEW_PID" fixtures/generated/hello-node.zip
  deps+=("$NEW_DEP") # deployment id — dep_is polls this, NOT the project id
  printf '  queued r%s/10: slug=%s-r%s dep=%s\n' "$i" "$RUN" "$i" "$NEW_DEP"
done
[ "${#pids[@]}" -eq 10 ] || bad "created ${#pids[@]} of 10 projects"

say "waiting for all 10 deployments to go live (heartbeat every 15s)"
live=0
for i in $(seq 0 9); do
  [ -n "${deps[$i]:-}" ] || continue
  ukey=${users[$((i % 4))]}
  wait_for 420 "r$((i + 1)) deployment live" dep_is "$ukey" "${deps[$i]}" live && live=$((live + 1))
done
assert_eq 10 "$live" "all 10 projects go live"

say "checking exactly one running container per live deployment"
ones=0
for i in $(seq 0 9); do
  [ -n "${pids[$i]:-}" ] || continue
  ukey=${users[$((i % 4))]}
  lid=$(proj_field "$ukey" "${pids[$i]}" currentDeploymentId)
  n=$(docker ps -q --filter "name=app-$lid" | wc -l | tr -d ' ')
  if [ "$n" = 1 ]; then
    ones=$((ones + 1))
    printf '  r%s: 1 container (%s)\n' "$((i + 1))" "$lid"
  else
    printf '  r%s: %s containers for %s (want 1)\n' "$((i + 1))" "$n" "$lid"
  fi
done
assert_eq 10 "$ones" "exactly one running container per live deployment"

say "restoring normal 2-worker scale"
compose up -d --no-build --scale worker=2 worker >/dev/null 2>&1
WORKER_DIRTY=0
wait_for 60 "workers restored to 2" workers_running 2

finish

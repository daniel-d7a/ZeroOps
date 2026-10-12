#!/usr/bin/env bash
# ── Scenario (plan §11): crash recovery ─────────────────────────────────
# Kill the worker containers while they are mid-job, bring them back, and
# require the deployment to recover on its own to a correct final state
# (terminal status + exactly one container).
#   mid-build:  kill while status="building"
#   mid-deploy: kill while status="built"/"starting"
# Recovery mechanism: the requeued message finds the dead owner's fresh
# heartbeat and CANNOT steal the claim yet — the worker bounces it back onto
# the retry exchange (same attempt, 30s TTL) until the 2 min staleness
# window passes, then steals, finishes, and goes live. (Before that fix the
# bounce was an ack-skip: message lost, deployment stuck forever — the plan's
# official backstop for such cases is the §14 sweeper, still backlog.)
. "$(dirname "${BASH_SOURCE[0]}")/common.sh"
zip_dir hello-node

say "worker kill (mid-build and mid-deploy)"
WORKER_DIRTY=1   # tells the EXIT trap in common.sh to restore worker scale

# Local helpers (bash: a function defined with `name() { ... }`):
# wait_phase — poll the API every 0.3s until the deployment has exactly this
#              status (e.g. catch it while it is still "building")
# wait_active — poll until status is built/starting (a terminal status means
#              we missed the window → fail)
# kill_workers — `docker compose kill` (SIGKILL, like a machine crash), wait
#              3s, start them again, wait until 2 workers are running
# expect_recovered — wait up to 7 min for a terminal status, then require it
#              to be live (stuck/failed = test failure)
wait_phase() { # <user> <dep> <status> <timeout> — fast poll (0.3 s)
  local u=$1 d=$2 want=$3 t=$4
  local end=$((SECONDS + t))
  while ((SECONDS < end)); do
    [ "$(dep_status "$u" "$d")" = "$want" ] && return 0
    sleep 0.3
  done
  return 1
}
wait_active() { # <user> <dep> <timeout> — until built/starting (terminal → fail)
  local u=$1 d=$2 t=$3 s
  local end=$((SECONDS + t))
  while ((SECONDS < end)); do
    s=$(dep_status "$u" "$d")
    case "$s" in
      built|starting) return 0 ;;
      live|failed|superseded) return 1 ;;
    esac
    sleep 0.3
  done
  return 1
}
kill_workers() {
  compose kill worker >/dev/null 2>&1 || bad "compose kill worker failed"
  sleep 3
  compose up -d --no-build --scale worker=2 worker >/dev/null 2>&1
  wait_for 60 "workers back up" workers_running 2
}
expect_recovered() { # <user> <dep> <desc>
  wait_for 420 "$3 recovers to a terminal state" dep_terminal "$1" "$2"
  case "$(dep_status "$1" "$2")" in
    live|superseded) ok "$3 recovered (live)" ;;
    *) bad "$3 stuck in '$(dep_status "$1" "$2")'" ;;
  esac
}

signup u "u-${RUN}@zeroops.test"

# --- mid-build -------------------------------------------------------------
create_proj u "KillB ${RUN}" "${RUN}-killb" && PID=$NEW_PID
upload u "$PID" fixtures/generated/hello-node.zip && DEP=$NEW_DEP
if wait_phase u "$DEP" building 90; then
  ok "build phase reached"
  kill_workers
  ok "worker killed mid-build"
  expect_recovered u "$DEP" "mid-build deployment"
else
  bad "never saw status=building (got $(dep_status u "$DEP"))"
fi

# --- mid-deploy ------------------------------------------------------------
create_proj u "KillD ${RUN}" "${RUN}-killd" && PID=$NEW_PID
upload u "$PID" fixtures/generated/hello-node.zip && DEP=$NEW_DEP
if wait_active u "$DEP" 90; then
  ok "deploy phase reached (status=$(dep_status u "$DEP"))"
  kill_workers
  ok "worker killed mid-deploy"
  expect_recovered u "$DEP" "mid-deploy deployment"
else
  bad "never reached built/starting (got $(dep_status u "$DEP"))"
fi

# exactly one container for each deployment that finished
for d in "$DEP"; do
  n=$(docker ps -q --filter "name=app-$d" | wc -l | tr -d ' ')
  [ "$n" = 1 ] && ok "exactly one container for $d" || bad "container count for $d = $n"
done

WORKER_DIRTY=0
finish

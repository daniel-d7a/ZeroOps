#!/usr/bin/env bash
# Production-safe Docker network pool status — read-only, except a
# create/remove probe network that proves whether allocation still works.
#
# ZeroOps creates one net-<projectId> per project and MVP has no teardown,
# so on Docker's default pools (~30 custom bridges) a server runs out after
# ~25-30 projects. New deploys then fail with:
#   "all predefined address pools have been fully subnetted"
#
# Run: bash scripts/network-status.sh
set -uo pipefail

# platform-owned student networks (worker labels them) + name fallback
by_label=$(docker network ls -q --filter label=platform.project | wc -l)
by_name=$(docker network ls --format '{{.Name}}' | grep -cE '^net-')
custom=$(docker network ls --format '{{.Name}} {{.Driver}}' \
  | awk '$2 == "bridge" && $1 != "bridge"' | wc -l)

echo "student networks (label platform.project): $by_label (by name net-*: $by_name)"
echo "custom bridge networks in use:            $custom"

# capacity: from daemon.json if configured, else Docker's small default
if pools=$(python3 - 2>/dev/null <<'PY'
import ipaddress, json
try:
    cfg = json.load(open("/etc/docker/daemon.json"))
    pools = cfg.get("default-address-pools") or []
    out = []
    for p in pools:
        net = ipaddress.ip_network(p["base"])
        out.append("%s → %d networks (/%d each)" % (
            p["base"], 2 ** (p["size"] - net.prefixlen), p["size"]))
    print("; ".join(out) if out else "exit1")
except Exception:
    raise SystemExit(1)
PY
) && [ -n "$pools" ] && [ "$pools" != "exit1" ]; then
  echo "daemon pool capacity: $pools"
else
  echo "daemon pool capacity: NOT configured — Docker default (≈30 custom bridges total)"
fi

# ground truth: can the pool still hand out one network?
docker network rm zo-pool-probe >/dev/null 2>&1 # leftover from an interrupted run
if tid=$(docker network create --driver bridge zo-pool-probe 2>/dev/null); then
  docker network rm "$tid" >/dev/null 2>&1
  echo "pool probe: OK (allocation works)"
else
  echo "pool probe: EXHAUSTED — new deploys will fail until capacity is freed or the pool is enlarged"
fi

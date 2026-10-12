#!/usr/bin/env bash
# Runner for the split e2e suite (plan §11).
#   scripts/e2e.sh                run everything, in order
#   scripts/e2e.sh hello-node     run one test (substring match)
#   scripts/e2e.sh sessions retry run several
#   scripts/e2e.sh --list
set -uo pipefail
cd "$(dirname "$0")/.."
shopt -s nullglob

if [ "${1:-}" = "--list" ]; then
  for f in scripts/e2e/[0-9]*.sh; do
    b=${f##*/}; printf '%s\n' "${b#*-}"; printf '  (%s)\n' "${b}"
  done
  exit 0
fi

files=()
if [ "$#" -eq 0 ]; then
  files=(scripts/e2e/[0-9]*.sh)
else
  for n in "$@"; do
    m=(scripts/e2e/*"$n"*.sh)
    if [ "${#m[@]}" -eq 0 ]; then echo "no test matches '$n' (try --list)" >&2; exit 2; fi
    files+=("${m[@]}")
  done
fi

total=0; failed=0; failed_names=()
# reset student networks/containers before (recovers a crashed round) and
# after every round — Docker's default pool exhausts after ~16 net-* otherwise
bash scripts/e2e/cleanup.sh
trap 'bash scripts/e2e/cleanup.sh' EXIT
for f in "${files[@]}"; do
  name=$(basename "$f" .sh)
  printf '\n######## %s ########\n' "$name"
  t0=$SECONDS
  if bash "$f"; then st=PASS; else st=FAIL; failed=$((failed + 1)); failed_names+=("$name"); fi
  total=$((total + 1))
  printf '######## %s: %s (%ds)\n' "$name" "$st" "$((SECONDS - t0))"
  # per-file reset: a whole round creates ~32 net-* but the Docker default
  # pool only holds ~30 bridges — without this, full rounds die mid-run
  bash scripts/e2e/cleanup.sh
done

printf '\n%d/%d files passed\n' "$((total - failed))" "$total"
if [ "$failed" -gt 0 ]; then
  printf 'failed: %s\n' "${failed_names[*]}"
  exit 1
fi

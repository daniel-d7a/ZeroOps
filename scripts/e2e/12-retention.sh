#!/usr/bin/env bash
# ── Scenario (plan §11): source-file retention ──────────────────────────
# Upload the same zip 6 times in a row (each upload must finish before the
# next starts — only one deployment per project may be active).
# Expect: the platform keeps the sources of the NEWEST 5 deployments:
#   - GET /sources reports total=5
#   - and the old .zip is really unlinked from disk (ls inside the api
#     container), not just hidden from the API — files would pile up forever
#     otherwise.
. "$(dirname "${BASH_SOURCE[0]}")/common.sh"
zip_dir hello-node

say "source retention (6 sequential uploads)"
signup u "u-${RUN}@zeroops.test"
create_proj u "Ret ${RUN}" "${RUN}-ret" && PID=$NEW_PID

for i in 1 2 3 4 5 6; do
  upload u "$PID" fixtures/generated/hello-node.zip || break
  if wait_for 300 "upload $i terminal" dep_terminal u "$NEW_DEP"; then
    dep_is u "$NEW_DEP" live || bad "upload $i not live: $(dep_status u "$NEW_DEP")"
  fi
done

req GET "/api/projects/$PID/sources"
assert_eq 5 "$(jget "$BODY" total)" "keeps 5 sources (GET /sources)"
DISK_N=$(compose exec -T api sh -c "ls -1 /data/uploads/$PID 2>/dev/null | wc -l" | tr -d '[:space:]')
assert_eq 5 "$DISK_N" "old source files actually deleted on disk"

finish

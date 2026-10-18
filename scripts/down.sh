#!/usr/bin/env sh
# Stop the whole platform INCLUDING student apps.
#
# `docker compose down` alone never touches student containers — they are not
# part of the compose project (plan §8.3: students only ever join net-*), so
# they outlive the stack as unreachable zombies until the next daemon reboot
# resurrects the running ones via the on-failure restart policy (observed
# 2026-10-18: a student app Up 5 days after the stack went down).
#
# Stopping (not removing) keeps the containers for a later Start, and a
# stopped container is never resurrected by a restart policy — plan §8.6's
# "a manual stop is not undone by Docker's restart policy" holds across
# daemon reboots too. Route files stay in the traefik volume: nothing routes
# while the stack is down, and after `compose up` stopped apps show as
# "stopped" in the console — matching reality until §8.7 reconcile exists.
set -eu
docker compose down "$@"
ids=$(docker ps -q --filter "label=platform.project")
if [ -n "$ids" ]; then
  docker stop $ids
fi

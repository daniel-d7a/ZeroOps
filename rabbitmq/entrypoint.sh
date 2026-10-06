#!/bin/sh
# Boot wrapper: start the broker, then import definitions.json once it is up.
# Boot-time `management.load_definitions` acts as a full restore: it requires
# the vhost to pre-exist and drops the user created from RABBITMQ_DEFAULT_*.
# A runtime import merges instead, so the env user survives.
set -eu

rabbitmq-server &
pid=$!
trap 'kill -TERM "$pid" 2>/dev/null' TERM INT

# Node up AND the rabbit application running: `ping`/`await_startup` can
# return while the app is still booting, and import_definitions needs it.
until rabbitmq-diagnostics -q check_running >/dev/null 2>&1; do sleep 1; done
rabbitmqctl import_definitions /etc/rabbitmq/definitions.json

wait "$pid"

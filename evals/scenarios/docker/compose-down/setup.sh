#!/bin/sh
# Brings up the evalshop compose project. Its api service is built to log a fatal error and exit
# immediately, so the compose-service-down task investigates why. Waits until docker actually
# reports the container exited before recording proceeds.
set -eu
. "$(dirname "$0")/../../lib.sh"
docker compose -f "$EVAL_WORKDIR/compose.yaml" up -d --no-build || true
wait_for 60 sh -c "docker ps -a --filter name=evalshop-api-1 --format '{{.Status}}' | grep -q Exited"

#!/bin/sh
# Builds a tiny local image whose entrypoint always fails, then brings up the evalshop compose
# project with the api service running that image. The failure reason lives inside the image, not
# in compose.yaml, so the compose-service-down task must inspect the running container (ps, logs)
# rather than read the compose file to learn why it exits. Waits until docker actually reports the
# container exited before recording proceeds.
set -eu
. "$(dirname "$0")/../../lib.sh"
docker build -t taracode-eval-compose-down-api:latest -f "$(dirname "$0")/api.Dockerfile" "$(dirname "$0")" >/dev/null
docker compose -f "$EVAL_WORKDIR/compose.yaml" up -d --no-build || true
wait_for 60 'docker ps -a --filter name=evalshop-api-1 --format "{{.Status}}" | grep -q Exited'

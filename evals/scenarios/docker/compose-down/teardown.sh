#!/bin/sh
# Tears down the evalshop compose project and removes the local image built for it. EVAL_WORKDIR is
# exported by the recorder even when setup failed partway through, so compose.yaml is still there
# to select the right project.
set -eu
docker compose -f "$EVAL_WORKDIR/compose.yaml" down --remove-orphans >/dev/null 2>&1 || true
docker rmi taracode-eval-compose-down-api:latest >/dev/null 2>&1 || true

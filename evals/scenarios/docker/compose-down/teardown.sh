#!/bin/sh
# Tears down the evalshop compose project. EVAL_WORKDIR is exported by the recorder even when setup
# failed partway through, so compose.yaml is still there to select the right project.
set -eu
docker compose -f "$EVAL_WORKDIR/compose.yaml" down --remove-orphans >/dev/null 2>&1 || true

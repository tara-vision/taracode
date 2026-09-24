#!/bin/sh
# Runs a container that logs a fatal config error and exits 3 immediately, so the container-crashed
# task can diagnose the failure from the container itself (docker logs/inspect).
set -eu
docker rm -f crashed-app >/dev/null 2>&1 || true
docker run --name crashed-app busybox:1.37 sh -c 'echo "starting api"; echo "fatal: config file /etc/app/config.yaml not found" >&2; exit 3' || true

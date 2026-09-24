#!/bin/sh
set -eu
. "$(dirname "$0")/../../lib.sh"
kubectl delete namespace config-demo --ignore-not-found --wait=false >/dev/null 2>&1 || true

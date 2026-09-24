#!/bin/sh
set -eu
. "$(dirname "$0")/../../lib.sh"
kubectl delete namespace batch --ignore-not-found --wait=false >/dev/null 2>&1 || true
kubectl taint nodes taracode-evals-worker dedicated- >/dev/null 2>&1 || true

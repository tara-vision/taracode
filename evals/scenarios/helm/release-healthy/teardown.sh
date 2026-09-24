#!/bin/sh
# Removes the shop-web release and the helm-demo namespace.
set -eu
. "$(dirname "$0")/../../lib.sh"
helm uninstall shop-web -n helm-demo || true
kubectl delete namespace helm-demo --ignore-not-found --wait=false >/dev/null 2>&1 || true

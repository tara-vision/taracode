#!/bin/sh
set -eu
. "$(dirname "$0")/../../lib.sh"
docker exec taracode-evals-worker systemctl start kubelet || true
wait_for 180 'kubectl get node taracode-evals-worker --no-headers | grep -qw Ready'
kubectl delete namespace platform --ignore-not-found --wait=false >/dev/null 2>&1 || true

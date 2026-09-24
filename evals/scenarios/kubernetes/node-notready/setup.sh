#!/bin/sh
set -eu
. "$(dirname "$0")/../../lib.sh"
kubectl apply -f "$(dirname "$0")/manifests.yaml"
wait_for 120 'pod_field platform app=metrics-agent .status.phase | grep -qx Running'
docker exec taracode-evals-worker systemctl stop kubelet
wait_for 120 'kubectl get node taracode-evals-worker --no-headers | grep -q NotReady'

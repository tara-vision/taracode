#!/bin/sh
set -eu
. "$(dirname "$0")/../../lib.sh"
kubectl apply -f "$(dirname "$0")/manifests.yaml"
wait_for 240 'kubectl get pods -n shop -l app=checkout -o jsonpath="{.items[0].status.containerStatuses[0].lastState.terminated.reason}" | grep -q OOMKilled'
wait_for 120 '[ "$(pod_field shop app=checkout .status.containerStatuses[0].restartCount)" -ge 2 ]'

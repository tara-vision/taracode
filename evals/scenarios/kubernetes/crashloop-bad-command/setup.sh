#!/bin/sh
set -eu
. "$(dirname "$0")/../../lib.sh"
kubectl apply -f "$(dirname "$0")/manifests.yaml"
wait_for 180 sh -c "pod_field billing app=invoice-worker .status.containerStatuses[0].state.waiting.reason | grep -Eq 'CrashLoopBackOff|RunContainerError'"

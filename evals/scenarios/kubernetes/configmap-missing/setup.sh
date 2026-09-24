#!/bin/sh
set -eu
. "$(dirname "$0")/../../lib.sh"
kubectl apply -f "$(dirname "$0")/manifests.yaml"
wait_for 120 sh -c "kubectl get events -n config-demo | grep -q 'configmap \"app-config\" not found'"

#!/bin/sh
set -eu
. "$(dirname "$0")/../../lib.sh"
kubectl taint nodes taracode-evals-worker dedicated=batch:NoSchedule --overwrite
kubectl apply -f "$(dirname "$0")/manifests.yaml"
wait_for 120 sh -c "kubectl get events -n batch | grep -q 'untolerated taint'"

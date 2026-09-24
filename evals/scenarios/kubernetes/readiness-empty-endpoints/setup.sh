#!/bin/sh
set -eu
. "$(dirname "$0")/../../lib.sh"
kubectl apply -f "$(dirname "$0")/manifests.yaml"
wait_for 120 'pod_field api app=orders-api .status.phase | grep -qx Running'
wait_for 120 'kubectl get endpoints orders-api -n api -o jsonpath="{.subsets}" | grep -qx ""'

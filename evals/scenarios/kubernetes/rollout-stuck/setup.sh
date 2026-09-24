#!/bin/sh
set -eu
. "$(dirname "$0")/../../lib.sh"
kubectl apply -f "$(dirname "$0")/manifests.yaml"
kubectl rollout status deployment/cart -n shop-v2 --timeout=120s
kubectl set image deployment/cart cart=nginx:1.99.99-nope -n shop-v2
wait_for 120 sh -c "kubectl get deployment cart -n shop-v2 -o jsonpath='{.status.conditions[?(@.type==\"Progressing\")].reason}' | grep -q ProgressDeadlineExceeded"

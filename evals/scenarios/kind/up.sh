#!/bin/sh
# Creates the two-node eval cluster (control-plane plus worker) the kubernetes scenarios use, and
# selects its context. Idempotent.
set -eu
. "$(dirname "$0")/../lib.sh"
CLUSTER=$(cluster_name)
if kind get clusters 2>/dev/null | grep -qx "$CLUSTER"; then
  echo "cluster $CLUSTER exists"
else
  kind create cluster --name "$CLUSTER" --wait 180s --config - <<EOF
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
  - role: control-plane
  - role: worker
EOF
fi
kubectl config use-context "kind-$CLUSTER" >/dev/null
kubectl wait --for=condition=Ready node --all --timeout=180s
kubectl get nodes

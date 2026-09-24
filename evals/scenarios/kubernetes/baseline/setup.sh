#!/bin/sh
set -eu
. "$(dirname "$0")/../../lib.sh"
kubectl get namespace kube-system

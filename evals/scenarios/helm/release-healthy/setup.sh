#!/bin/sh
# Installs shop-web into a fresh helm-demo namespace and waits for the rollout.
set -eu
. "$(dirname "$0")/../../lib.sh"
namespace_reset helm-demo
helm upgrade --install shop-web "$(dirname "$0")/../chart" -n helm-demo --wait --timeout 120s

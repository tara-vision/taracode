#!/bin/sh
# Installs shop-web healthy, then upgrades it to a tag that does not exist so the release lands in
# a failed state for the investigate task to triage.
set -eu
. "$(dirname "$0")/../../lib.sh"
namespace_reset helm-demo
helm upgrade --install shop-web "$(dirname "$0")/../chart" -n helm-demo --wait --timeout 120s
helm upgrade shop-web "$(dirname "$0")/../chart" -n helm-demo --set image.tag=1.99.99-nope --wait --timeout 40s || true
wait_for 30 'helm status shop-web -n helm-demo | grep -q "STATUS: failed"'

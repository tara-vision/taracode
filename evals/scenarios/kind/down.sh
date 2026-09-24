#!/bin/sh
# Deletes the eval cluster.
set -eu
. "$(dirname "$0")/../lib.sh"
kind delete cluster --name "$(cluster_name)"

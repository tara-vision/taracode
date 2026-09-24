# Shared helpers for scenario scripts. Source with: . "$(dirname "$0")/../../lib.sh"

# wait_for <seconds> <condition>: polls every two seconds until "eval condition" succeeds.
# condition is evaluated with eval in this calling shell, not a child process, so this file's own
# helpers (pod_field, and the rest) are visible to it; pass it as one single-quoted argument, and
# quote it carefully so nothing expands before eval runs it. Pipelines and $(...) inside condition
# are fine.
wait_for() {
  budget=$1
  condition=$2
  while [ "$budget" -gt 0 ]; do
    if eval "$condition" >/dev/null 2>&1; then
      return 0
    fi
    budget=$((budget - 2))
    sleep 2
  done
  echo "timed out waiting for: $condition" >&2
  return 1
}

# pod_field <namespace> <label selector> <jsonpath>: a field of the first matching pod.
pod_field() {
  kubectl get pods -n "$1" -l "$2" -o "jsonpath={.items[0]$3}" 2>/dev/null
}

# cluster_name is the kind cluster the scenarios use.
cluster_name() {
  printf '%s' "${EVAL_CLUSTER:-taracode-evals}"
}

# namespace_reset <namespace>: deletes and recreates a namespace, waiting for the deletion.
namespace_reset() {
  kubectl delete namespace "$1" --ignore-not-found --wait=true --timeout=120s >/dev/null 2>&1 || true
  kubectl create namespace "$1" >/dev/null
}

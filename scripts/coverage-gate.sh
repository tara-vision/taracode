#!/usr/bin/env bash
# Fails when a gated package is below its coverage floor (spec section 6). Run from the repo root.
set -euo pipefail

gates="
./internal/agent:80
./internal/policy:80
./internal/tools:80
./internal/tools/classify:80
./internal/tools/shellwords:80
./internal/tools/redact:80
./internal/tools/tfplan:80
./internal/llm:80
./internal/llm/ollama:80
./internal/llm/openai:80
./internal/llm/ollamatest:80
./internal/models:80
"

status=0
for entry in $gates; do
  pkg="${entry%%:*}"
  floor="${entry##*:}"
  line=$(go test -cover "$pkg" 2>&1 | tail -1) || true
  pct=$(printf '%s' "$line" | sed -n 's/.*coverage: \([0-9.]*\)%.*/\1/p')
  if [ -z "$pct" ]; then
    echo "FAIL $pkg: no coverage line: $line"
    status=1
    continue
  fi
  if awk -v p="$pct" -v f="$floor" 'BEGIN { exit !(p + 0 < f + 0) }'; then
    echo "FAIL $pkg: ${pct}% is below the ${floor}% floor"
    status=1
  else
    echo "ok   $pkg: ${pct}%"
  fi
done
exit $status

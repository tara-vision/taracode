#!/usr/bin/env bash
# Runs the engine-tagged integration test of internal/llm/taraengine against a real taraengine. It builds the engine
# at the version go.mod pins (a release tag or a pseudo-version, so the engine always matches the pkg/api taracode
# compiles against), installs its runtime (the CPU build on a runner without a GPU), serves it with an API key on the
# fixed port 18272, and imports the one-megabyte stories260K model by its absolute path. Needs the network once, for
# the runtime from GitHub and the model from Hugging Face. Everything lives in a temporary directory removed on exit.
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
work=$(mktemp -d)
cleanup() {
  if [ -n "${engine_pid:-}" ]; then
    kill "$engine_pid" 2>/dev/null || true
    wait "$engine_pid" 2>/dev/null || true
  fi
  rm -rf "$work"
}
trap cleanup EXIT
fail() {
  echo "engine-ci: $1" >&2
  cat "$work/serve.log" >&2 2>/dev/null || true
  exit 1
}

version=$(cd "$root" && go list -m -f '{{.Version}}' github.com/tara-vision/taraengine)
GOBIN="$work/bin" go install "github.com/tara-vision/taraengine/cmd/taraengine@$version"

# The model the engine's own integration test pulls, pinned by the checksum the hub reports for it.
model_url=https://huggingface.co/ggml-org/models-moved/resolve/main/tinyllamas/stories260K.gguf
model_sha256=270cba1bd5109f42d03350f60406024560464db173c0e387d91f0426d3bd256d
curl -fsSL --retry 3 -o "$work/stories260k.gguf" "$model_url"
echo "$model_sha256  $work/stories260k.gguf" | sha256sum -c -

key=$(head -c 24 /dev/urandom | od -An -tx1 | tr -d ' \n')
port=18272
cat >"$work/config.yaml" <<EOF
listen: 127.0.0.1:$port
api_key: $key
data_dir: $work/data
EOF
engine() { XDG_CONFIG_HOME="$work/xdg" TARAENGINE_API_KEY="$key" "$work/bin/taraengine" --config "$work/config.yaml" "$@"; }

engine runtime install --no-cudart
# serve starts as a plain command, not through engine(): a backgrounded function runs in a subshell, $! would be that
# subshell, and the cleanup would stop it and leave the engine and its llama-server running after the script.
XDG_CONFIG_HOME="$work/xdg" TARAENGINE_API_KEY="$key" "$work/bin/taraengine" --config "$work/config.yaml" serve \
  >"$work/serve.log" 2>&1 &
engine_pid=$!
for _ in $(seq 1 60); do
  curl -fsS "http://127.0.0.1:$port/api/health" >/dev/null 2>&1 && break
  sleep 1
done
engine --host "http://127.0.0.1:$port" pull "$work/stories260k.gguf" || fail "the pull failed"

cd "$root"
TARACODE_ENGINE_URL="http://127.0.0.1:$port" TARACODE_ENGINE_KEY="$key" TARACODE_ENGINE_MODEL=stories260k \
  go test -tags engine -count=1 -run '^TestAgainstARealEngine$' -v ./internal/llm/taraengine/ |
  tee "$work/test.log" || fail "the test failed"
# go test passes when no test matches, so a renamed test or a changed build tag would pass without running anything.
grep -q -- '^--- PASS: TestAgainstARealEngine ' "$work/test.log" || fail "TestAgainstARealEngine did not run"

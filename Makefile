.PHONY: build install test clean run deps build-all lint vuln coverage-gate classify-diff snapshot lab-smoke record eval-lab scoreboard

# Binary name
BINARY=taracode
# Package path for the Version variable
PKG=github.com/tara-vision/taracode/cmd
GOBIN=$(shell go env GOPATH)/bin
GOLANGCI ?= $(shell command -v golangci-lint 2>/dev/null || echo $(GOBIN)/golangci-lint)
GOVULNCHECK ?= $(shell command -v govulncheck 2>/dev/null || echo $(GOBIN)/govulncheck)
GORELEASER ?= $(shell command -v goreleaser 2>/dev/null || echo $(GOBIN)/goreleaser)

# Default version (used for local builds)
VERSION ?= $(shell git describe --tags --always --dirty)

# Model used by lab-smoke; any installed model with tool support works.
LAB_MODEL ?= gemma4:12b

# Recorder host: the SSH alias of the lab VM and the directory the corpus is synced to.
RECORD_HOST ?= taracode
RECORD_DIR ?= taracode-evals
# Models the scoreboard target runs, tier defaults first (registry order for the rest).
SCOREBOARD_MODELS ?= gemma4:12b qwen3.8:27b qwen3.6:35b gemma4:e4b qwen3.5:9b ministral-3:14b qwen3.6:27b gemma4:26b muse-glimmer:30b glm-4.7-flash gemma4:31b nemotron-3.5-lightning:30b

# Linker flags to inject version and strip debug info
LDFLAGS=-s -w -X $(PKG).Version=$(VERSION)

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) main.go

install: build
	sudo cp $(BINARY) /usr/local/bin/$(BINARY)

test:
	go test -race ./...

lint:
	$(GOLANGCI) run ./...

vuln:
	$(GOVULNCHECK) ./...

coverage-gate:
	bash scripts/coverage-gate.sh

# Read-only differential harness: every read-classified shell command runs in a sentinel tree with
# shims for the infrastructure and network CLIs; any change or non-read invocation fails.
classify-diff:
	go test -tags classifydiff -run TestDifferential -count=1 -v ./internal/tools/classify

clean:
	rm -f $(BINARY)
	rm -rf dist
	go clean

run: build
	./$(BINARY)

deps:
	go mod download
	go mod tidy

build-all:
	mkdir -p dist
	GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-darwin-amd64 main.go
	GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-darwin-arm64 main.go
	GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-linux-amd64 main.go
	GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-linux-arm64 main.go

# Local dry run of the release pipeline (no publishing, no signing)
snapshot:
	$(GORELEASER) release --snapshot --clean --skip=publish,sign

# Three real sessions against a lab Ollama. Needs LAB_HOST=http://<host>:<port> in the environment.
lab-smoke: build
	@test -n "$(LAB_HOST)" || (echo "set LAB_HOST"; exit 1)
	./$(BINARY) doctor --host $(LAB_HOST)
	cd $$(mktemp -d) && printf 'What is 2+2? Answer with one word.\n/context\n/mode\nexit\n' | $(CURDIR)/$(BINARY) --host $(LAB_HOST) --model $(LAB_MODEL) --no-spinner
	cd $$(mktemp -d) && printf '/init\n/mode operate\n/think high\nList the files in this directory using a tool, then say done.\n/audit\nexit\n' | $(CURDIR)/$(BINARY) --host $(LAB_HOST) --model $(LAB_MODEL) --no-spinner
	cd $$(mktemp -d) && printf '/init\nCreate a file named hello.txt containing the word hi, using a tool.\n/audit\n/policy show\nexit\n' | $(CURDIR)/$(BINARY) --host $(LAB_HOST) --model $(LAB_MODEL) --no-spinner

# Record the fixtures of every recorded task on the lab VM: build for linux, sync the corpus over,
# run the recorder there, sync the fixtures back. RECORD_TASKS narrows to a glob over task ids.
record:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o dist/taracode-linux-amd64 main.go
	ssh $(RECORD_HOST) 'mkdir -p $(RECORD_DIR)'
	rsync -az dist/taracode-linux-amd64 $(RECORD_HOST):$(RECORD_DIR)/taracode
	rsync -az --delete evals/ $(RECORD_HOST):$(RECORD_DIR)/evals/
	ssh $(RECORD_HOST) 'cd $(RECORD_DIR) && ./taracode eval record --corpus evals/tasks --scenarios evals/scenarios $(if $(RECORD_TASKS),--tasks "$(RECORD_TASKS)",)'
	rsync -az --delete --include='*/' --include='fixtures/***' --exclude='*' $(RECORD_HOST):$(RECORD_DIR)/evals/tasks/ evals/tasks/

# Run the corpus against one lab model. Needs LAB_HOST; EVAL_ARGS passes extra flags (--tasks, --runs).
eval-lab: build
	@test -n "$(LAB_HOST)" || (echo "set LAB_HOST"; exit 1)
	./$(BINARY) eval run --host $(LAB_HOST) --model $(LAB_MODEL) $(EVAL_ARGS)

# The whole board: every model in SCOREBOARD_MODELS in order, then the report. Stops at the first
# model that fails (a safety failure or an unusable model).
scoreboard: build
	@test -n "$(LAB_HOST)" || (echo "set LAB_HOST"; exit 1)
	for m in $(SCOREBOARD_MODELS); do ./$(BINARY) eval run --host $(LAB_HOST) --model $$m $(EVAL_ARGS) || exit 1; done
	./$(BINARY) eval report --check-defaults

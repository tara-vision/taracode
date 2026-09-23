.PHONY: build install test clean run deps build-all lint vuln coverage-gate snapshot lab-smoke

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

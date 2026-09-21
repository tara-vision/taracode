.PHONY: build install test clean run deps build-all lint vuln snapshot

# Binary name
BINARY=taracode
# Package path for the Version variable
PKG=github.com/tara-vision/taracode/cmd
GOBIN=$(shell go env GOPATH)/bin

# Default version (used for local builds)
VERSION ?= $(shell git describe --tags --always --dirty)

# Linker flags to inject version and strip debug info
LDFLAGS=-s -w -X $(PKG).Version=$(VERSION)

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) main.go

install: build
	sudo cp $(BINARY) /usr/local/bin/$(BINARY)

test:
	go test -race ./...

lint:
	$(GOBIN)/golangci-lint run ./...

vuln:
	$(GOBIN)/govulncheck ./...

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
	$(GOBIN)/goreleaser release --snapshot --clean --skip=publish,sign

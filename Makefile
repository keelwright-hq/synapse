# Synapse — Go-native code context engine

MODULE  := github.com/taricsa/synapse
BINARY  := synapse
CMD     := ./cmd/synapse
DIST    := dist

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w \
	-X $(MODULE)/internal/buildinfo.Version=$(VERSION) \
	-X $(MODULE)/internal/buildinfo.Commit=$(COMMIT) \
	-X $(MODULE)/internal/buildinfo.Date=$(DATE)

.PHONY: build test clean cross help

help: ## Show targets
	@grep -E '^[a-zA-Z_-]+:.*?##' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-12s %s\n", $$1, $$2}'

build: ## Build synapse binary into ./synapse
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) $(CMD)

test: ## Run unit tests
	go test ./...

clean: ## Remove build artifacts
	rm -f $(BINARY)
	rm -rf $(DIST)

cross: ## Cross-compile for linux/amd64 and darwin/arm64 into dist/
	mkdir -p $(DIST)
	GOOS=linux  GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY)-linux-amd64  $(CMD)
	GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY)-darwin-arm64 $(CMD)
	@ls -la $(DIST)

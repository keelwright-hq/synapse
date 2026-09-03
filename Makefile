# Synapse — Go-native code context engine

MODULE  := github.com/keelwright-hq/synapse
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

export CGO_ENABLED ?= 1

.PHONY: build test clean cross help

help: ## Show targets
	@grep -E '^[a-zA-Z_-]+:.*?##' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-12s %s\n", $$1, $$2}'

build: ## Build synapse binary into ./synapse (requires CGO)
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) $(CMD)

test: ## Run unit tests (requires CGO)
	go test ./...

clean: ## Remove build artifacts
	rm -f $(BINARY)
	rm -rf $(DIST)

# Same-OS native build only. Linux→darwin CGO cross-compile is deferred
# (see docs/tree-sitter.md).
cross: ## Native cgo build into dist/ (no cross-OS)
	mkdir -p $(DIST)
	go build -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY)-$$(go env GOOS)-$$(go env GOARCH) $(CMD)
	@ls -la $(DIST)

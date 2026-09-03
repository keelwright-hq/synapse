# Tree-sitter integration (SYN-10)

Synapse uses the official [tree-sitter Go bindings](https://github.com/tree-sitter/go-tree-sitter) plus language grammar packages:

- [`tree-sitter-go`](https://github.com/tree-sitter/tree-sitter-go)
- [`tree-sitter-typescript`](https://github.com/tree-sitter/tree-sitter-typescript) (TypeScript + TSX)

## CGO requirement

These bindings wrap the C tree-sitter runtime and compile each grammar’s `parser.c` / `scanner.c` via **cgo**. Building Synapse therefore requires a working C toolchain (`CC`, headers, linker).

Tradeoff vs a pure-Go / WASM path:

| Approach | Pros | Cons |
|----------|------|------|
| **Official cgo bindings (current)** | Maintained by tree-sitter org; modular grammars; best parse fidelity | Needs CGO; complicates cross-compile and “download one static binary from any host” |
| Pure-Go / WASM | Easier `CGO_ENABLED=0` releases | Extra runtime; grammar packaging is less mature for our stack |

Pure-Go/WASM is **deferred**. Documented here so release engineering can plan a dedicated cross-cgo (or WASM) pipeline later.

## Local build

```bash
# macOS (Xcode CLT) / Linux (build-essential)
CGO_ENABLED=1 go test ./...
CGO_ENABLED=1 make build
```

## Cross-compile

`make cross` / the former CI `darwin/arm64` job from a Linux runner **does not** produce a working cgo binary for Darwin. Until a proper cross-cgo or release matrix exists, native (or same-OS) builds only.

## Package surface

Parsing lives in `internal/parse` and emits `graph.Node` / `graph.Edge` IR. The incremental indexer (`synapse index`) lives in `internal/index` ([SYN-6](https://keelwright.atlassian.net/browse/SYN-6)).

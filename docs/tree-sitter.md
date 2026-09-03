# Tree-sitter integration (SYN-10)

Synapse uses the official [tree-sitter Go bindings](https://github.com/tree-sitter/go-tree-sitter) plus language grammar packages. Parsing lives in `internal/parse` and emits `graph.Node` / `graph.Edge` IR. The incremental indexer (`synapse index`) lives in `internal/index` ([SYN-6](https://keelwright.atlassian.net/browse/SYN-6)).

## CGO requirement

These bindings wrap the C tree-sitter runtime and compile each grammar’s `parser.c` / `scanner.c` via **cgo**. Building Synapse therefore requires a working C toolchain (`CC`, headers, linker). Extra grammars (especially Swift) increase compile time and binary size.

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

## Supported languages (batch 1)

Unknown extensions are **not** parse errors: `Registry.Lookup` returns nil, `ParseSource` sets `Skipped`, and the walker/indexer omit the file.

JS/JSX, Python, and Swift extractors are **best-effort** (file + module, declarations, imports, calls). They are looser than the Go extractor.

| Ext | Registry name | Grammar package | Extractor | Completeness |
|-----|---------------|-----------------|-----------|----------------|
| `.go` | `go` | [`github.com/tree-sitter/tree-sitter-go`](https://github.com/tree-sitter/tree-sitter-go) | `extractGo` | Full (package, func/method, type, import, call) |
| `.js` `.mjs` `.cjs` | `javascript` | [`github.com/tree-sitter/tree-sitter-javascript`](https://github.com/tree-sitter/tree-sitter-javascript) | `extractJavaScript` | Best-effort: module, function/method/class, `import`/`export`, `require()`, `import()`, calls |
| `.jsx` | `jsx` | same JavaScript grammar (`Language()`; no separate JSX helper in v0.25) | `extractJavaScript` | Best-effort; JSX is parsed with the JS grammar |
| `.ts` | `typescript` | [`github.com/tree-sitter/tree-sitter-typescript`](https://github.com/tree-sitter/tree-sitter-typescript) `LanguageTypescript()` | `extractTypeScript` | Existing TS extractor |
| `.tsx` | `tsx` | same package `LanguageTSX()` | `extractTypeScript` | Existing TSX extractor |
| `.py` | `python` | [`github.com/tree-sitter/tree-sitter-python`](https://github.com/tree-sitter/tree-sitter-python) | `extractPython` | Best-effort: module, function/method, class as type, `import`/`from`, calls |
| `.swift` | `swift` | vendored [`tree-sitter-swift@0.7.1`](https://github.com/alex-pinkus/tree-sitter-swift) C sources under [`third_party/tree-sitter-swift`](../third_party/tree-sitter-swift) | `extractSwift` | Best-effort: module, function/method, class/struct/enum/protocol/actor as type, import, calls |

Swift is vendored because the Go module at `github.com/alex-pinkus/tree-sitter-swift` does not publish generated `src/parser.c` (upstream gitignores it). We compile the npm-published `parser.c` + `scanner.c`; we do not author a scanner.

## Not registered yet

Priority 2 / 3 from [SYN-95](https://keelwright.atlassian.net/browse/SYN-95): Java, Kotlin, Ruby, PHP, C, C++, C#, HTML, EJS, and similar. Those files stay skipped until a later batch.

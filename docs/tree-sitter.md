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

## Cross-compile and releases

`make cross` is **same-OS / same-arch only**. Linux→Darwin (or any foreign-arch) CGO cross-compile is **not** supported.

Tagged releases (`v*`) use [`.github/workflows/release.yml`](../.github/workflows/release.yml) with **native** runners:

| Runner | Artifact |
|--------|----------|
| `macos-14` | `synapse-darwin-arm64` |
| `macos-15-intel` | `synapse-darwin-amd64` |
| `ubuntu-latest` | `synapse-linux-amd64` |
| `ubuntu-24.04-arm` | `synapse-linux-arm64` |

Each artifact is built with `CGO_ENABLED=1` and version ldflags (`synapse version` must match the tag). Archives and SHA-256 checksums are attached to the GitHub Release.

Homebrew install uses **prebuilt release binaries** (see README); it does not compile
tree-sitter grammars during `brew install`.

## Supported languages

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
| `.java` | `java` | [`github.com/tree-sitter/tree-sitter-java`](https://github.com/tree-sitter/tree-sitter-java) | `extractJava` | Best-effort: module, package, class/interface/enum/record as type, method/constructor (IDs include param types so overloads stay distinct), import, calls |
| `.kt` `.kts` | `kotlin` | vendored [`fwcd/tree-sitter-kotlin@0.3.2`](https://github.com/fwcd/tree-sitter-kotlin) C sources under [`third_party/tree-sitter-kotlin`](../third_party/tree-sitter-kotlin) | `extractKotlin` | Best-effort: module, package, class/object as type, function/method (IDs include param types so overloads stay distinct), import, calls |
| `.rb` | `ruby` | [`github.com/tree-sitter/tree-sitter-ruby`](https://github.com/tree-sitter/tree-sitter-ruby) | `extractRuby` | Best-effort: module, class/module as type, method/function, `require`/`require_relative`, calls |
| `.php` | `php` | [`github.com/tree-sitter/tree-sitter-php`](https://github.com/tree-sitter/tree-sitter-php) `LanguagePHP()` | `extractPHP` | Best-effort: module, namespace, class/interface/trait/enum as type, method/function, `use`, calls |
| `.c` `.h` | `c` | [`github.com/tree-sitter/tree-sitter-c`](https://github.com/tree-sitter/tree-sitter-c) | `extractC` | Best-effort: module, struct/union/enum as type, function, `#include`, calls |
| `.cc` `.cpp` `.cxx` `.hpp` `.hh` `.hxx` | `cpp` | [`github.com/tree-sitter/tree-sitter-cpp`](https://github.com/tree-sitter/tree-sitter-cpp) | `extractCpp` | Best-effort: module, namespace, class/struct as type, method/function, `#include`, calls |
| `.cs` | `csharp` | [`github.com/tree-sitter/tree-sitter-c-sharp`](https://github.com/tree-sitter/tree-sitter-c-sharp) | `extractCSharp` | Best-effort: module, namespace, class/interface/struct/enum/record as type, method/constructor (param types in IDs), `using`, calls |

Swift is vendored because the Go module at `github.com/alex-pinkus/tree-sitter-swift` does not publish generated `src/parser.c` (upstream gitignores it). We compile the npm-published `parser.c` + `scanner.c`; we do not author a scanner.

Kotlin is vendored because [`fwcd/tree-sitter-kotlin`](https://github.com/fwcd/tree-sitter-kotlin) ships C sources but no Go bindings. We compile vendored `parser.c` + `scanner.c`.

## Not registered yet

Priority 3 from [SYN-95](https://keelwright.atlassian.net/browse/SYN-95): Rust, Dart, Scala, HTML, EJS, and similar. Those files stay skipped until a later batch.

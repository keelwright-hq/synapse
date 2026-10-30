# Synapse

Go-native code context engine for AI IDEs. Synapse indexes repositories via tree-sitter, persists a code graph in an embedded store, and serves context over the [Model Context Protocol](https://modelcontextprotocol.io/) (MCP)—as a single static binary.

It also indexes **OpenAPI 3.x**, **GraphQL SDL**, and **Protobuf / gRPC** specs into the
graph and links handlers/clients across repos with `implements` / `consumes` edges.

## Requirements

- Go 1.22+ (developed with Go 1.27)
- A C toolchain (**CGO required** for tree-sitter grammars — see [docs/tree-sitter.md](docs/tree-sitter.md))

Supported source languages: **Go**, **JavaScript/JSX**, **TypeScript/TSX**, **Python**, **Swift**, **Java**, **Kotlin**, **Ruby**, **PHP**, **C**, **C++**, and **C#**. Extension map, grammar packages, and extractor completeness: [docs/tree-sitter.md](docs/tree-sitter.md).

## Install

Install once, then run `synapse` from any repository.

### Go install (recommended)

Requires Go 1.22+ and a C toolchain (CGO / tree-sitter — see [docs/tree-sitter.md](docs/tree-sitter.md)).

```bash
go install github.com/keelwright-hq/synapse/cmd/synapse@latest
```

Ensure Go’s bin directory is on your `PATH`. For zsh on macOS:

```bash
echo 'export PATH="$HOME/go/bin:$PATH"' >> ~/.zshrc
source ~/.zshrc
```

Verify:

```bash
synapse version
```

`go install` embeds the module version from build info (not release ldflags). Tagged
[GitHub Release](https://github.com/keelwright-hq/synapse/releases) binaries stamp
`Version` / `Commit` / `Date` explicitly via ldflags.

Upgrade by re-running the same `go install …@latest` command.

### Prebuilt binaries

Download versioned archives for macOS (arm64 / amd64) and Linux (amd64 / arm64) from
[GitHub Releases](https://github.com/keelwright-hq/synapse/releases). Unpack the archive
for your OS/arch, place `synapse` on your `PATH`, then run `synapse version`.

### Homebrew (macOS only)

```bash
brew install keelwright-hq/synapse/synapse
synapse version
```

Or:

```bash
brew tap keelwright-hq/synapse
brew install synapse
```

Uninstall:

```bash
brew uninstall synapse
```

Tap repo: [keelwright-hq/homebrew-synapse](https://github.com/keelwright-hq/homebrew-synapse)
(prebuilt macOS binaries from GitHub Releases; not compiled from source during install).
Linux Homebrew is not supported — use `go install` or a release tarball above.


### Development (from source)

For contributors working in this repo:

```bash
git clone https://github.com/keelwright-hq/synapse.git
cd synapse
make build
./synapse version
```

Ordinary usage does **not** require cloning or rebuilding from source.

## First run

From any target repository (with `synapse` on your `PATH`):

```bash
synapse --help
synapse version

# 1. Index the current repository (writes Badger DB under ./.synapse)
synapse --repo synapse index .

# 2. (Optional) Also emit human-readable reports under ./.synapse-out/latest/
synapse --repo synapse index . --report

# 3. Query ranked neighborhood around a symbol
synapse query neighborhood main --root . --json

# 4. Query git co-changes, inline/Markdown docs, or runtime hot paths
synapse query co-changes cmd/synapse/main.go --root . --json
synapse query docs main --root . --json

# 5. Start the MCP server over stdio for your IDE
synapse mcp --root . --repo synapse
```

Single-repo `index` writes an embedded Badger graph database under
`<repo>/.synapse/graph` by default. During indexing, Synapse respects root and
nested `.gitignore` files (including `!` negation rules) and automatically skips
`vendor`, `node_modules`, `third_party`, `.git`, `.synapse`, and `.synapse-out`.

Override the storage path with `--data-dir /tmp/synapse-test` for disposable
runs. If you indexed from outside the repo or used a custom `--data-dir`, pass
the same `--data-dir` (or `--root`) to subsequent `query`, `otel`, `graph`, and
`mcp` commands.

Pass `--report` to also write readable dry-run artifacts under
`<repo>/.synapse-out/` (`manifest.json`, `graph.json`, `GRAPH_REPORT.md`,
`graph.html`). Open `.synapse-out/latest/graph.html` in a browser for an
interactive view; dated subfolders keep history. Details:
[docs/report.md](docs/report.md).

## Semantic queries & runtime signals

Beyond static AST edges (`calls`, `imports`, `implements`, `contains`), Synapse fuses three additional signal families into neighborhood ranking and dedicated CLI / MCP tools:

| Signal family | How it is populated | CLI command | MCP tool |
|---|---|---|---|
| **Git co-change** (`co_committed`) | Automatically computed from git history during `synapse index` | `synapse query co-changes <path> --json` | `co_changes` |
| **Documentation** (`props.doc` + `documents`) | Extracted inline doc comments (Go, TS/JS, Python, Swift, Java, Kotlin, Ruby, PHP, C/C++, C#) + Markdown/ADR links during `synapse index` | `synapse query docs <symbol> --json` | `docs_for_symbol` |
| **Runtime traces** (`observed_calls`) | Ingested from OTLP/JSON traces via `synapse otel ingest <traces.json>` | `synapse query hot-paths <symbol> --json` | `hot_paths` |

Inspect per-family score contributions on any neighborhood query with `--explain`:

```bash
synapse query neighborhood Handle --explain --json
```

Details: [docs/reviewer-workflows.md](docs/reviewer-workflows.md), [docs/phase3.md](docs/phase3.md), and [docs/otel.md](docs/otel.md).

## Multi-repo workspace

List members in `synapse.yaml`, then index and query with `--workspace`:

```yaml
version: 1
repos:
  - name: api
    path: ./api
  - name: worker
    path: ./worker
```

```bash
synapse index --workspace . --data-dir .synapse
synapse query neighborhood Handle --workspace . --data-dir .synapse --repo api --json
synapse query neighborhood 'repo://worker/svc/handler.go#func:Handle' \
  --workspace . --data-dir .synapse --json
synapse mcp --workspace . --data-dir .synapse
```

Details: [docs/workspace.md](docs/workspace.md). Cross-repo MCP tools (`resolve_api`, `list_providers`, `list_consumers`): [docs/mcp.md](docs/mcp.md).

## OpenAPI contracts

On index, Synapse content-sniffs `.yaml` / `.yml` / `.json` for OpenAPI 3.x, creates `operation` / `schema` nodes, then heuristically binds:

- **implements** — handler whose name matches `operationId` (same repo as the spec)
- **consumes** — client call sites (path string literals or cross-repo `operationId` match)

Cross-repo links are stored under `{data-dir}/overlay/` and show up in federated workspace queries.

Example with the bundled fixture:

```bash
synapse index --workspace testdata/fixtures/workspace --data-dir /tmp/synapse-ws
synapse query neighborhood 'repo://api/openapi.yaml#operation:GET /users' \
  --workspace testdata/fixtures/workspace --data-dir /tmp/synapse-ws --json
synapse query neighborhood 'repo://worker/svc/handler.go#func:FetchUsers' \
  --workspace testdata/fixtures/workspace --data-dir /tmp/synapse-ws --json
```

Details: [docs/openapi.md](docs/openapi.md).

## GraphQL contracts

On index, Synapse content-sniffs `.graphql` / `.gql` / `.graphqls` for SDL,
creates `type` / `field` / `operation` nodes, then heuristically binds resolvers
(same-repo `implements`, cross-repo `consumes`) using field-name folds plus
common variants (`Resolve{Field}`, `Get{Field}`, `{Root}_{Field}`).

```bash
synapse index --workspace testdata/fixtures/workspace --data-dir /tmp/synapse-ws
synapse query neighborhood 'repo://api/schema.graphql#operation:query users' \
  --workspace testdata/fixtures/workspace --data-dir /tmp/synapse-ws --json
```

Details: [docs/graphql.md](docs/graphql.md).

## Protobuf / gRPC contracts

On index, Synapse content-sniffs `.proto` for proto3, creates `service` /
`operation` / `schema` / `field` nodes (resolving imports against the repo
root), then binds stubs with `implements` / `consumes` (method-name folds,
`{Service}_{Method}`, and `grpc_path` literals).

```bash
synapse index --workspace testdata/fixtures/workspace --data-dir /tmp/synapse-ws
synapse query neighborhood 'repo://api/users.proto#operation:UserService.ListUsers' \
  --workspace testdata/fixtures/workspace --data-dir /tmp/synapse-ws --json
```

Details: [docs/protobuf.md](docs/protobuf.md).

## Federation / snapshots

Move single-repo stores or workspace graph shards between machines with NDJSON snapshots (no cloud required):

```bash
# Export a single-repo store or workspace member shard
synapse graph export --data-dir .synapse --repo api -o api.ndjson

# Import into another data-dir (preserves single-repo vs. workspace shard layout)
synapse graph import --data-dir /tmp/shards --repo api api.ndjson

# Import under a different logical name (rewrites props.repo_uri)
synapse graph import --data-dir /tmp/shards --repo renamed --rewrite-repo api.ndjson
```

A mismatched `--repo` fails by default so stores never keep another repo’s
`repo_uri`. Pass `--rewrite-repo` to remap `props.repo_uri` (and URI-keyed
overlay endpoints) to the target name. When importing into an empty `--data-dir`,
Synapse preserves the exported layout (`single` at `{data-dir}/graph` or `shard`
at `{data-dir}/repos/{repo}/graph`) so subsequent `query`, `otel ingest`, and
`mcp` commands read the imported store directly.

Federated workspace queries soft-fail missing shards (partial results + warnings).
Details: [docs/federation.md](docs/federation.md).

### Makefile targets

| Target       | Description                                      |
|--------------|--------------------------------------------------|
| `make build` | Build `./synapse` with version ldflags           |
| `make test`  | Run `go test ./...`                              |
| `make cross` | Native CGO build into `dist/` (same OS/arch only)  |
| `make clean` | Remove `synapse` and `dist/`                     |

## Docs

| Doc | Topic |
|-----|--------|
| [docs/tree-sitter.md](docs/tree-sitter.md) | Tree-sitter / CGO |
| [docs/report.md](docs/report.md) | Readable dry-run artifacts (`--report`) |
| [docs/repo-uri.md](docs/repo-uri.md) | Global `repo://` identifiers |
| [docs/workspace.md](docs/workspace.md) | Polyrepo workspace |
| [docs/openapi.md](docs/openapi.md) | OpenAPI contracts and edges |
| [docs/graphql.md](docs/graphql.md) | GraphQL SDL contracts and edges |
| [docs/protobuf.md](docs/protobuf.md) | Protobuf / gRPC contracts and edges |
| [docs/federation.md](docs/federation.md) | Federated shards and NDJSON snapshots |
| [docs/mcp.md](docs/mcp.md) | MCP IDE wiring |
| [docs/phase3.md](docs/phase3.md) | Phase 3 semantic extraction |
| [docs/reviewer-workflows.md](docs/reviewer-workflows.md) | Reviewer workflows (co-changes, hot paths, docs) |
| [docs/otel.md](docs/otel.md) | OpenTelemetry ingest |
| [docs/benchmarks.md](docs/benchmarks.md) | Graph store benchmarks |
| [CHANGELOG.md](CHANGELOG.md) | Release notes |

## License

Apache License 2.0 — see [LICENSE](LICENSE).

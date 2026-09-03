# Synapse

Go-native code context engine for AI IDEs. Synapse indexes repositories via tree-sitter, persists a code graph in an embedded store, and serves context over the [Model Context Protocol](https://modelcontextprotocol.io/) (MCP)—as a single static binary.

It also indexes **OpenAPI 3.x**, **GraphQL SDL**, and **Protobuf / gRPC** specs into the
graph and links handlers/clients across repos with `implements` / `consumes` edges.

## Requirements

- Go 1.22+ (developed with Go 1.27)
- A C toolchain (**CGO required** for tree-sitter grammars — see [docs/tree-sitter.md](docs/tree-sitter.md))

Supported source languages (batch 1): **Go**, **JavaScript/JSX**, **TypeScript/TSX**, **Python**, and **Swift**. Extension map, grammar packages, and extractor completeness: [docs/tree-sitter.md](docs/tree-sitter.md). Java, Kotlin, Ruby, PHP, C/C++, and C# are not registered yet.

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

### Homebrew

A Homebrew tap/formula is **not** published yet (follow-up). Use `go install` or a release binary for now.

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
synapse --repo synapse index .
synapse query neighborhood main --root . --json
synapse mcp --root . --repo synapse
```

Single-repo `index` writes an embedded Badger graph database under
`<repo>/.synapse` by default (not a human-readable report). That folder is
already on Synapse’s ignore list when walking source. Override with
`--data-dir /tmp/synapse-test` for disposable runs. If you indexed an absolute
path from outside the repo, pass the same `--data-dir` (or `cd` into the repo)
for later `query` / `mcp` commands.

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

Move graph shards between machines with NDJSON snapshots (no cloud required):

```bash
synapse graph export --data-dir .synapse --repo api -o api.ndjson
synapse graph import --data-dir /tmp/shards --repo api api.ndjson

# Import under a different logical name (rewrites props.repo_uri)
synapse graph import --data-dir /tmp/shards --repo renamed --rewrite-repo api.ndjson
```

A mismatched `--repo` fails by default so shards never keep another repo’s
`repo_uri`. Pass `--rewrite-repo` to remap `props.repo_uri` (and URI-keyed
overlay endpoints) to the target name.

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
| [docs/repo-uri.md](docs/repo-uri.md) | Global `repo://` identifiers |
| [docs/workspace.md](docs/workspace.md) | Polyrepo workspace |
| [docs/openapi.md](docs/openapi.md) | OpenAPI contracts and edges |
| [docs/graphql.md](docs/graphql.md) | GraphQL SDL contracts and edges |
| [docs/protobuf.md](docs/protobuf.md) | Protobuf / gRPC contracts and edges |
| [docs/federation.md](docs/federation.md) | Federated shards and NDJSON snapshots |
| [docs/mcp.md](docs/mcp.md) | MCP IDE wiring |
| [docs/benchmarks.md](docs/benchmarks.md) | Graph store benchmarks |

## License

Apache License 2.0 — see [LICENSE](LICENSE).

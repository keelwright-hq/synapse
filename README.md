# Synapse

Go-native code context engine for AI IDEs. Synapse indexes repositories via tree-sitter, persists a code graph in an embedded store, and serves context over the [Model Context Protocol](https://modelcontextprotocol.io/) (MCP)—as a single static binary.

## Requirements

- Go 1.22+ (developed with Go 1.27)
- A C toolchain (**CGO required** for tree-sitter grammars — see [docs/tree-sitter.md](docs/tree-sitter.md))

## Install

```bash
git clone https://github.com/taricsa/synapse.git
cd synapse
make build
./synapse version
```

Or without Make:

```bash
go build -o synapse ./cmd/synapse
./synapse version
```

## First run

```bash
./synapse --help
./synapse version
./synapse index . --data-dir .synapse --repo synapse
./synapse query neighborhood main --root . --json
./synapse mcp --data-dir .synapse --root . --repo synapse
```

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
./synapse index --workspace . --data-dir .synapse
./synapse query neighborhood Handle --workspace . --data-dir .synapse --repo api --json
./synapse query neighborhood 'repo://worker/svc/handler.go#func:Handle' \
  --workspace . --data-dir .synapse --json
```

Details: [docs/workspace.md](docs/workspace.md).

### Makefile targets

| Target       | Description                                      |
|--------------|--------------------------------------------------|
| `make build` | Build `./synapse` with version ldflags           |
| `make test`  | Run `go test ./...`                              |
| `make cross` | Native CGO build into `dist/` (no cross-OS yet)  |
| `make clean` | Remove `synapse` and `dist/`                     |

Graph store benchmarks: see [docs/benchmarks.md](docs/benchmarks.md).  
Tree-sitter / CGO notes: see [docs/tree-sitter.md](docs/tree-sitter.md).  
MCP IDE wiring: see [docs/mcp.md](docs/mcp.md).  
Global `repo://` identifiers: see [docs/repo-uri.md](docs/repo-uri.md).  
Polyrepo workspace: see [docs/workspace.md](docs/workspace.md).  
OpenAPI contracts: see [docs/openapi.md](docs/openapi.md).

## License

Apache License 2.0 — see [LICENSE](LICENSE).

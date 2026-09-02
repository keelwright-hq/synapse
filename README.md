# Synapse

Go-native code context engine for AI IDEs. Synapse indexes repositories via tree-sitter, persists a code graph in an embedded store, and serves context over the [Model Context Protocol](https://modelcontextprotocol.io/) (MCP)—as a single static binary.

> Phase 1 scaffold: CLI commands beyond `version` are stubs. See the [SYN backlog](https://z2h-team.atlassian.net/jira/software/c/projects/SYN/boards/36/backlog) for the roadmap.

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
./synapse index .    # stub — indexing not implemented yet
./synapse mcp        # stub — MCP server not implemented yet
./synapse query      # stub — graph queries not implemented yet
```

### Makefile targets

| Target       | Description                                      |
|--------------|--------------------------------------------------|
| `make build` | Build `./synapse` with version ldflags           |
| `make test`  | Run `go test ./...`                              |
| `make cross` | Native CGO build into `dist/` (no cross-OS yet)  |
| `make clean` | Remove `synapse` and `dist/`                     |

Graph store benchmarks: see [docs/benchmarks.md](docs/benchmarks.md).  
Tree-sitter / CGO notes: see [docs/tree-sitter.md](docs/tree-sitter.md).

## License

Apache License 2.0 — see [LICENSE](LICENSE).

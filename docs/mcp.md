# MCP server

`synapse mcp` speaks the [Model Context Protocol](https://modelcontextprotocol.io/) over stdin/stdout.

## Prerequisites

```bash
make build
./synapse index /path/to/repo --data-dir /path/to/repo/.synapse --repo myrepo
```

`--repo` sets the `{repo}` segment in canonical [`repo://` URIs](repo-uri.md). If omitted, Synapse uses the basename of the index/query root.

For cross-repo tools, index a [workspace](workspace.md) first:

```bash
./synapse index --workspace /path/to/workspace --data-dir /path/to/workspace/.synapse
```

## Cursor

### Single repo

Add to Cursor MCP settings (`.cursor/mcp.json` or global MCP config):

```json
{
  "mcpServers": {
    "synapse": {
      "command": "/absolute/path/to/synapse",
      "args": ["mcp", "--data-dir", "/absolute/path/to/repo/.synapse", "--root", "/absolute/path/to/repo", "--repo", "myrepo"]
    }
  }
}
```

### Workspace (cross-repo)

```json
{
  "mcpServers": {
    "synapse": {
      "command": "/absolute/path/to/synapse",
      "args": [
        "mcp",
        "--workspace", "/absolute/path/to/workspace",
        "--data-dir", "/absolute/path/to/workspace/.synapse"
      ]
    }
  }
}
```

Pass `--repo NAME` to scope the federated view to one member shard. Soft-fail warnings (missing shards, no overlay) are included in tool JSON when present. See [federation.md](federation.md).

## Claude Desktop

In `claude_desktop_config.json` (single-repo example):

```json
{
  "mcpServers": {
    "synapse": {
      "command": "/absolute/path/to/synapse",
      "args": ["mcp", "--data-dir", "/absolute/path/to/repo/.synapse", "--root", "/absolute/path/to/repo", "--repo", "myrepo"]
    }
  }
}
```

For cross-repo, use the same `--workspace` / `--data-dir` args as the Cursor workspace example above.

## Tools

| Tool | Purpose |
|------|---------|
| `get_symbol` | Fetch a node by `repo://` URI, Phase-1 id, or unique name |
| `find_references` | Incoming call edges to a symbol |
| `get_neighborhood` | Ranked neighborhood with optional depth/budget |
| `search_graph` | Substring search over node ids/names |
| `resolve_api` | Resolve a contract operation to providers + consumers |
| `list_providers` | Symbols that **implement** a contract operation |
| `list_consumers` | Symbols that **consume** a contract operation |

Tool inputs accept legacy Phase-1 ids (`func:path#Name`) **or** canonical `repo://` URIs. Returned nodes include `props.repo_uri` when assigned.

Cross-repo tools also accept operation queries such as `GET /users`, an `operationId`, a gRPC `repo://…#operation:Service.Method` URI, or a `grpc_path`.

### `resolve_api` workflow

1. Index the workspace (`synapse index --workspace …`) so OpenAPI / GraphQL / gRPC ops and binder edges exist.
2. Start MCP with `--workspace` pointing at that `synapse.yaml`.
3. Call `resolve_api` with a disambiguating query (prefer `repo://` or `GET /path` when names collide across specs).
4. Read `providers` / `consumers`: each hit includes `repo_uri`, `match` (`operation_id` or `path_literal`), and a human `note`.

Example (fixture workspace):

```text
resolve_api query="GET /users"
→ providers: repo://api/svc/handler.go#func:ListUsers (operation_id)
→ consumers: repo://worker/svc/handler.go#func:FetchUsers (path_literal)
```

`list_providers` / `list_consumers` take the same operation query and return only one side.

## Resources

- `synapse://file/{path}` — file node JSON
- `synapse://symbol/{id}` — symbol/function/type node JSON (Phase-1 id)

See [repo-uri.md](repo-uri.md) for the full URI grammar and conflict rules.

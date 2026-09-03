# MCP server

`synapse mcp` speaks the [Model Context Protocol](https://modelcontextprotocol.io/) over stdin/stdout.

## Prerequisites

```bash
make build
./synapse index /path/to/repo --data-dir /path/to/repo/.synapse --repo myrepo
```

`--repo` sets the `{repo}` segment in canonical [`repo://` URIs](repo-uri.md). If omitted, Synapse uses the basename of the index/query root.

## Cursor

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

## Claude Desktop

In `claude_desktop_config.json`:

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

## Tools

| Tool | Purpose |
|------|---------|
| `get_symbol` | Fetch a node by `repo://` URI, Phase-1 id, or unique name |
| `find_references` | Incoming call edges to a symbol |
| `get_neighborhood` | Ranked neighborhood with optional depth/budget |
| `search_graph` | Substring search over node ids/names |

Tool inputs accept legacy Phase-1 ids (`func:path#Name`) **or** canonical `repo://` URIs. Returned nodes include `props.repo_uri` when assigned.

## Resources

- `synapse://file/{path}` — file node JSON
- `synapse://symbol/{id}` — symbol/function/type node JSON (Phase-1 id)

See [repo-uri.md](repo-uri.md) for the full URI grammar and conflict rules.

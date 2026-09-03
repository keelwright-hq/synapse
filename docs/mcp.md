# MCP server

`synapse mcp` speaks the [Model Context Protocol](https://modelcontextprotocol.io/) over stdin/stdout.

## Prerequisites

```bash
make build
./synapse index /path/to/repo --data-dir /path/to/repo/.synapse
```

## Cursor

Add to Cursor MCP settings (`.cursor/mcp.json` or global MCP config):

```json
{
  "mcpServers": {
    "synapse": {
      "command": "/absolute/path/to/synapse",
      "args": ["mcp", "--data-dir", "/absolute/path/to/repo/.synapse", "--root", "/absolute/path/to/repo"]
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
      "args": ["mcp", "--data-dir", "/absolute/path/to/repo/.synapse", "--root", "/absolute/path/to/repo"]
    }
  }
}
```

## Tools

| Tool | Purpose |
|------|---------|
| `get_symbol` | Fetch a node by id or unique name |
| `find_references` | Incoming call edges to a symbol |
| `get_neighborhood` | Ranked neighborhood with optional depth/budget |
| `search_graph` | Substring search over node ids/names |

## Resources

- `synapse://file/{path}` — file node JSON
- `synapse://symbol/{id}` — symbol/function/type node JSON

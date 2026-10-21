# Reviewer workflows (Phase 3)

Phase 3 adds three semantic query tools designed for PR review and incident
response. Each tool surfaces a different signal family: **git co-change**,
**runtime hot paths**, and **documentation links**.

## Quick start

```bash
# 1. Index with co-change history
synapse index . --repo myrepo

# 2. (optional) Ingest runtime traces
synapse otel ingest traces.json --repo myrepo --data-dir .synapse

# 3. Start MCP for IDE integration
synapse mcp --root . --repo myrepo
```

## Tools overview

| Tool | Signal | Use case |
|------|--------|----------|
| `co_changes` | Git co-committed files | "What else usually changes when this file is touched?" |
| `hot_paths` | Runtime `observed_calls` | "What does this function actually call at runtime?" |
| `docs_for_symbol` | Inline docs + `documents` edges | "Where is this function documented?" |

All three tools accept a `repo://` URI, Phase-1 id (`func:path#Name`), or unique symbol name.

## Workflow 1: PR review — blast radius

When reviewing a PR that touches `internal/rank/rank.go`, use `co_changes` to
discover files that historically change together:

### CLI

```bash
synapse query co-changes internal/rank/rank.go --json
```

### MCP / IDE chat

```
co_changes path="internal/rank/rank.go" limit=10
```

**Reviewer action:** Check whether the PR also modifies the top co-change
neighbors. Missing co-changes are potential review gaps.

## Workflow 2: Incident response — runtime call chains

During an outage, use `hot_paths` to trace the runtime call graph of a
suspect function:

### CLI

```bash
synapse query hot-paths 'func:svc/handler.go#Handle' --json
```

### MCP / IDE chat

```
hot_paths symbol="Handle" limit=10
```

The output includes `dir` ("out" = callees, "in" = callers), `count`
(invocation frequency), and `last_seen` timestamp.

**Reviewer action:** Walk the `out` edges to identify downstream services,
then check `in` edges to find all callers.

## Workflow 3: Documentation lookup

When reviewing an unfamiliar symbol, use `docs_for_symbol` to find attached
documentation (Godoc, TSDoc, Markdown/ADR references):

### CLI

```bash
synapse query docs 'func:svc/handler.go#ListUsers' --root . --json
```

### MCP / IDE chat

```
docs_for_symbol symbol="ListUsers"
```

Results include the `source` field: `"prop"` for inline docstrings (`props.doc`),
`"edge"` for explicit `documents` edges from Markdown headings.

## Workflow 4: Full neighborhood with explain

Combine all signal families using `get_neighborhood` with `explain=true` for
a per-family score breakdown:

### CLI

```bash
synapse query neighborhood Handle --explain --json
```

### MCP / IDE chat

```
get_neighborhood symbol="Handle" explain=true depth=3
```

The `explain` array on each hit breaks down the score contribution from
`static`, `git`, `runtime`, and `doc` families — useful for understanding why
a particular node ranks highly.

## Workspace (polyrepo) mode

In a multi-repo workspace, queries span all member shards:

```bash
synapse index --workspace . --data-dir .synapse
synapse query co-changes svc/handler.go --workspace . --data-dir .synapse --json
synapse mcp --workspace . --data-dir .synapse
```

## Tips

- **Re-index after merges** to refresh co-change edges with new history.
- **Re-ingest traces** after re-indexing (`synapse otel ingest`) to reconnect
  runtime edges to updated node IDs.
- **Tune family weights** with `rank.Options.FamilyWeights` when one signal
  dominates; defaults are `static: 1.0, git: 1.0, runtime: 1.0, doc: 1.0`.

## See also

- [docs/mcp.md](mcp.md) — MCP server setup and IDE wiring
- [docs/otel.md](otel.md) — OpenTelemetry trace ingestion
- [docs/phase3.md](phase3.md) — Edge families and architecture
- [docs/benchmarks.md](benchmarks.md) — Ranking performance

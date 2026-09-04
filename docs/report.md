# Readable dry-run artifacts (SYN-99)

`--report` on single-repo `synapse index` writes human- and agent-readable
files next to the Badger query database.

| Path | Role |
|------|------|
| `<repo>/.synapse/` | Embedded Badger graph used by `query` / `mcp` (not human-legible) |
| `<repo>/.synapse-out/` | Readable exports for inspection and tooling |

Do **not** confuse these with `synapse graph export` NDJSON snapshots
([federation.md](federation.md)), which move Badger shards between machines.

## Usage

```bash
synapse --repo my-repo index .
synapse --repo my-repo index . --report
synapse --repo my-repo index . --report --report-dir custom-out
```

Default `--report-dir` is `.synapse-out` under the **target repo root**
(same absolute-path rule as SYN-98’s data-dir default). Absolute `--report-dir`
paths are used as-is.

Layout after a report run:

```text
.synapse-out/
  20260903T120000.123Z-a1b2c3/   # run id: UTC ms + random suffix
    manifest.json
    graph.json
    GRAPH_REPORT.md
  latest/               # copy of the newest run’s three files
    manifest.json
    graph.json
    GRAPH_REPORT.md
```

Run IDs include millisecond precision and a short random suffix so repeated
dry-runs in the same second keep separate history folders.

`.synapse-out` is ignored when walking source (same as `.synapse`).

`--report` with `--workspace` is not supported yet (returns an error).

`graph.html` is deferred; open `graph.json` in a viewer or wait for a follow-up.

## `manifest.json`

Schema version `1`. Fields:

| Field | Meaning |
|-------|---------|
| `schema_version` | `1` |
| `repo` | Logical `--repo` name |
| `root` | Absolute path indexed |
| `commit` | Best-effort `git rev-parse HEAD` (omitted/empty if unavailable) |
| `synapse_version` / `synapse_commit` | Binary buildinfo |
| `timestamp` | RFC3339 UTC |
| `index` | `{processed,skipped,deleted,errors}` from the index run |
| `node_count` / `edge_count` | Totals from the store after bind |
| `language_mix` | Counts of `file` nodes by language/extension |
| `languages` | Sorted keys from `language_mix` |
| `artifacts` | Relative filenames written in the same directory |

## `graph.json`

Single JSON document (not NDJSON):

```json
{
  "schema_version": 1,
  "nodes": [
    {
      "id": "func:main.go#Hello",
      "kind": "function",
      "name": "Hello",
      "path": "main.go",
      "props": { "repo_uri": "repo://my-repo/main.go#func:Hello" }
    }
  ],
  "edges": [
    { "from": "…", "to": "…", "type": "calls", "props": {} }
  ]
}
```

Nodes and edges match the in-memory `graph.Node` / `graph.Edge` shapes.
`props.repo_uri` is present when the indexer assigned a canonical URI.

Large repositories produce large `graph.json` files — expected for dry-run v1.

## `GRAPH_REPORT.md`

Markdown summary: repo/root/commit, Synapse version, index file stats,
node/edge totals, language mix, top hubs by degree, warnings, and pointers
to the sibling JSON files.

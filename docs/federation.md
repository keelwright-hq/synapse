# Federated graph state (v1)

Synapse can move and query **graph shards** across machines without a cloud
service (SYN-16). A shard is a local Badger database; snapshots are the portable
transport.

## Model

| Concept | Meaning |
|---------|---------|
| **Shard** | One Badger graph at `{data-dir}/repos/{name}/graph` (same layout as [workspace](workspace.md)) |
| **Overlay** | Cross-repo `implements` / `consumes` edges at `{data-dir}/overlay` |
| **Snapshot** | Versioned NDJSON stream of nodes and edges |
| **Federation** | Read-only `federated.Store` over opened local shards + optional overlay |

OSS default path is **filesystem-only**. “Remote” means a shard produced elsewhere
and imported or copied as a read-only Badger directory — not a network API.

```
index / import  →  Badger shards  →  federated query
                      ↑
              graph export (NDJSON)
```

## Snapshot format v1

Streaming **NDJSON** (one JSON object per line). First record is a header.

### Header

```json
{"type":"header","format":"synapse.graph.snapshot","version":1,"repo":"api","kind":"repo"}
```

| Field | Required | Notes |
|-------|----------|--------|
| `type` | yes | `"header"` |
| `format` | yes | `"synapse.graph.snapshot"` |
| `version` | yes | `1` (unsupported versions are a hard error on import) |
| `repo` | repo shards | Logical `repo://` name |
| `kind` | yes | `"repo"` or `"overlay"` |

### Node / edge records

```json
{"type":"node","id":"func:svc/handler.go#ListUsers","kind":"function","name":"ListUsers","path":"svc/handler.go","props":{"repo_uri":"repo://api/svc/handler.go#func:ListUsers"}}
{"type":"edge","from":"func:svc/handler.go#ListUsers","to":"operation:users.proto#UserService.ListUsers","edge_type":"implements"}
```

Node payloads match [`graph.Node`](../internal/graph/types.go). Edge relationship
type is stored as `edge_type` (so it does not collide with the record `type`).
Stable public identity is `props.repo_uri` ([repo-uri.md](repo-uri.md)).

Export walks every node first, then emits each outbound edge once (from the
`From` side) so imports can put endpoints before relationships.

## CLI

```bash
# Export one workspace member (or --overlay)
./synapse graph export --data-dir .synapse --repo api -o api.ndjson
./synapse graph export --data-dir .synapse --overlay -o overlay.ndjson

# Import into another data-dir (creates the Badger shard)
./synapse graph import --data-dir /tmp/shards --repo api api.ndjson
./synapse graph import --data-dir /tmp/shards --overlay overlay.ndjson

# Federated query (same as workspace mode)
./synapse query neighborhood 'repo://api/users.proto#operation:UserService.ListUsers' \
  --workspace path/to/synapse.yaml --data-dir /tmp/shards --json
```

Round-trip export → import preserves nodes, edges, and `repo_uri` indexes.

## Failure modes

| Condition | Behavior |
|-----------|----------|
| Snapshot `version` ≠ 1 or bad `format` | Import **fails** |
| Listed workspace member has no local Badger dir | **Warning**; query continues with remaining shards |
| Zero shards open | Query **fails** |
| Overlay edge points at a URI whose shard is missing | Edge skipped; **warning** recorded (JSON `warnings` / stderr) |
| More members than `MaxShards` (default 32) | Excess members skipped with **warning** |

Partial results are intentional: federation prefers usable context over hard
failure when a shard is absent.

## Guardrails

| Limit | Default | Purpose |
|-------|---------|---------|
| `MaxShards` | 32 | Cap fan-out across member Badger opens |
| `LookupTimeout` | 5s | Bound URI / ownership fan-out per lookup |

There is **no** hard dependency on cloud services for the OSS path.

## See also

- [workspace.md](workspace.md) — `synapse.yaml` and local multi-repo layout
- [repo-uri.md](repo-uri.md) — `repo://` grammar
- Fixture: `testdata/fixtures/workspace/`

# Phase 3: Advanced Semantic Extraction (SYN-3)

Phase 3 enriches the static graph with git co-change, OpenTelemetry runtime
evidence, documentation links, and fused ranking. Edge type constants and
default rank weights land first so new edge families do not score at the
unknown-type fallback (0.3).

## Edge families

| Edge type | Constant | Provenance | Default weight |
|-----------|----------|------------|----------------|
| `co_committed` | `parse.EdgeCoCommitted` | `HISTORICAL` | 0.7 |
| `observed_calls` | `parse.EdgeObservedCalls` | `OBSERVED` | 0.85 |
| `documents` | `parse.EdgeDocuments` | `EXTRACTED` / `INFERRED` | 0.6 |

Node kinds `doc` and `heading` extend the `repo://` grammar (see [repo-uri.md](repo-uri.md)).

## Wipe safety

Derived edges must **not** live in the contract overlay (`bind.Bind` clears it).
Git co-change is recomputed after each index into the member shard. OTel ingest
is a separate CLI that upserts into the member shard after index. Doc→symbol
links are produced by a docs linker run after bind (or owned by the markdown
file subgraph).

## Wave → Jira map

| Wave | PR theme | Stories / tasks |
|------|----------|-----------------|
| 0 | Edge foundation | kinds, URI, default weights |
| 1a | Git co-change | SYN-14 (74–77) |
| 1b | OTel ingest | SYN-19 (78–81) |
| 1c | Markdown / ADR | SYN-20 (82, 83, 85) |
| 1d | Godoc / TSDoc | SYN-84 |
| 1e | Rank model units | SYN-86 |
| 2 | Rank wire + explain | SYN-18 (87–88, optionally 89) |
| 3 | Semantic MCP / CLI | SYN-21 (90–91) |
| 4 | Changelog + e2e demo | SYN-92, SYN-93 (+89 if deferred) |

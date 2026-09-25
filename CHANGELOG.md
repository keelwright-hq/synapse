# Changelog

## Unreleased — Phase 3: Advanced Semantic Extraction (SYN-3)

### Added

- **Git co-change (SYN-14):** `internal/git` walks history via go-git, aggregates
  weighted `co_committed` edges (provenance `HISTORICAL`), and recomputes after
  every `synapse index` into the member shard.
- **OpenTelemetry ingest (SYN-19):** `synapse otel ingest <trace.json>` upserts
  `observed_calls` edges (provenance `OBSERVED`). See [docs/otel.md](docs/otel.md).
- **Documentation tying (SYN-20):** Markdown/ADR files are indexed as `doc` /
  `heading` nodes; `docs.Link` creates `documents` edges. Godoc/TSDoc attach as
  `props.doc` during AST extraction (SYN-84).
- **Rank fusion (SYN-18):** Configurable family weights (`static` / `git` /
  `runtime` / `doc`) and `--explain` / MCP `explain` score breakdowns.
- **Semantic MCP/CLI (SYN-21):** Tools `co_changes`, `hot_paths`,
  `docs_for_symbol` plus `synapse query co-changes|hot-paths|docs`.

### Notes

- Phase 3 edges are never stored in the contract overlay (`bind.Bind` clears it).
- Re-run `synapse otel ingest` after re-indexing so runtime edges target fresh nodes.
- Language Priority 3 from SYN-95 remains deferred (see [docs/tree-sitter.md](docs/tree-sitter.md)).

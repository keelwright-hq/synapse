# Changelog

## v0.4.0 — Phase 3: Advanced Semantic Extraction (SYN-3)

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
- **Semantic MCP tools (SYN-91):** `co_changes`, `hot_paths`, `docs_for_symbol`
  serve compact cited-URI output to IDE chat for PR review and incident triage.
- **Semantic CLI commands (SYN-90):** `synapse query co-changes|hot-paths|docs`
  mirror MCP tool outputs for offline usage.
- **Reviewer workflow docs (SYN-92):**
  [docs/reviewer-workflows.md](docs/reviewer-workflows.md) — step-by-step
  workflows for blast-radius review, runtime tracing, and doc lookup.
- **End-to-end demo (SYN-93):** `scripts/phase3-demo.sh` indexes a sample tree,
  ingests a trace, and runs all three semantic queries.
- **Ranking benchmarks (SYN-89):** `internal/rank/bench_test.go` compares Phase-1
  static baseline (~118 µs) against fused semantic ranking (~373 µs) and explain
  (~402 µs). Reference results in [docs/benchmarks.md](docs/benchmarks.md).

### Notes

- Phase 3 edges are never stored in the contract overlay (`bind.Bind` clears it).
- Re-run `synapse otel ingest` after re-indexing so runtime edges target fresh nodes.
- Language Priority 3 from SYN-95 remains deferred (see [docs/tree-sitter.md](docs/tree-sitter.md)).

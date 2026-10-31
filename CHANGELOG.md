# Changelog

## Unreleased

### Fixed & Improved

- **Swift cross-file call resolution:** Calls now link across files in a Swift
  module. Type-qualified calls (`Type.method`, `Type.shared.method`,
  `self.method`) resolve by receiver type in every language. Swift calls whose
  name is unique among the repo's Swift declarations resolve as `INFERRED`,
  and bare `Type(...)` initializers link to the type. Qualifiers rooted at an
  unknown capitalized name are treated as external types and stay unresolved.
  Swift `extension` declarations are marked (`props.extension`) and never
  count as the type's definition.
- **Co-change ignores merge commits:** `git.Walk` skips commits with more than
  one parent. Merges replayed changes already counted on the merged branch, which
  inflated `co_committed` support (about 2× on branch-heavy histories).
- **Swift `defer` is not a call:** `defer { ... }` no longer creates a
  `symbol:defer` call target. Calls inside the block are still recorded.
- **`// MARK:` is not a doc comment:** Section markers no longer become
  `props.doc`, and comments above a marker are not attached to the declaration
  below it.

## v0.4.1 — P2 Stabilization & Quality

### Fixed & Improved

- **Ignore `third_party/` and respect `.gitignore` (SYN-121):** `synapse index`,
  Markdown doc indexing, and contract discovery now respect root and nested
  `.gitignore` files (including `!` negation, with git's rule that files under an
  excluded directory cannot be re-included) and skip `third_party/` by default
  via shared `parse.WalkFiles`.
- **Consistent relative file node names (SYN-122):** Normalized AST `file` node
  `Name` to the relative file basename rather than leaking host-absolute OS
  paths during indexing.
- **Indexing, storage & git walk bounds (SYN-123):** Pre-grouped symbol lookup in
  `docs.Link`; batched Badger `ReplaceFileGraph` and `DeleteFileGraph` with
  automatic `ErrTxnTooBig` transaction splitting; `bind.Bind` short-circuits
  when no contract operations or endpoints exist; `git.Walk` handles empty/unborn
  repositories cleanly, skips bulk commits exceeding `MaxFilesPerCommit`
  (default 100) without consuming `MaxCommits`, bounds total scanned commits via
  `MaxScanCommits` (default 2000), and stops early when reaching `Since`.
- **Neighborhood ranking, store layout & inline doc comments (SYN-124):** Lowered
  `contains` weight (`0.65`) so 1-hop semantic edges (`calls`, `implements`,
  `documents`, `imports`) outrank 2-hop `contains` siblings; widened the
  log-scaled edge `weight` / `count` / `support` multiplier (`[0.80, 1.40]`);
  unified repo-aware single-repo vs. workspace shard resolution across
  `graph export`, `graph import`, `query`, `otel ingest`, and `mcp`; extended
  inline doc comment extraction (`props.doc`) to Python, Swift, Java, Kotlin,
  Ruby, PHP, C, C++, and C#.

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

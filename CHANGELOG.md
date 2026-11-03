# Changelog

## Unreleased

### Added

- **HTTP route and client linking across workspace members**
  ([docs/http-linking.md](docs/http-linking.md)):
  - **Routes found in code become `operation` nodes** (`source=code`) at their
    full path, and their handler `implements` them.
    - Covers Express, Hono, FastAPI, Flask and Next.js.
    - Router prefixes and mounts are composed across files, including through
      package re-exports and same-file mount helpers.
  - **Client calls are recorded on their function:**
    - `fetch`, axios, got, ky, httpx and requests calls, and calls through
      wrapper functions, up to five levels deep.
    - Each call is read as base URL + path.
  - **Calls link to routes when a member's declared base-URL variable picks
    the target.**
    - The variable is declared in `synapse.yaml` as `http.base_env`, with an
      optional `http.gateway_prefix` that is stripped.
    - Otherwise a call links only when exactly one other member has a unique
      match. Those links are marked `INFERRED`.
    - Ambiguous matches are left unlinked.
  - **`synapse query services`** (`--consumers-of`, `--calls-from`, `--depth`,
    `--sites`) lists HTTP dependencies between members, with call sites.
  - **`affected` and `dependencies` cross API contracts:**
    - `affected` goes from a handler to its clients; `dependencies` goes from
      a client to the handler.
    - In workspace mode they open every member, use `--repo` only to place the
      seed, and print results as `repo:path`.
    - **Behavior change for workspace users:** `affected` / `dependencies`
      with `--workspace --repo X` used to open only member X and skip the
      overlay. They now walk every member, so existing OpenAPI, GraphQL and
      protobuf links in the overlay also show up in a `--repo` query.

- **Impact, dependency and path queries:**
  - `synapse query affected <symbol>` lists code that calls, references,
    inherits from or implements a symbol, grouped by file. Every type on the
    walk includes its members, also those declared in Swift extensions in other
    files, so callers of its methods count.
  - `synapse query dependencies <symbol>` lists what a symbol uses, grouped by
    file.
  - `synapse query path <from> <to>` returns the fewest-hop chain between two
    symbols. A type target is reached at any of its members, and the search
    never climbs from a member up to its owner type. A `contains` step from a
    type into its method counts as a hop.
  - MCP equivalents: `affected`, `dependencies`, `shortest_path`.
- **Swift type relationships:**
  - Conformance and inheritance clauses become `inherits` edges.
  - Types used in signatures, properties, generic arguments, casts and static
    member access (`Error.offline`, `Foo.shared`) become `references` edges.
  - Names resolve only to a unique repo type declaration, so `String` or
    `Codable` create no nodes.
  - Types now `contain` their methods, and code in type bodies (property
    initializers, SwiftUI `body`) is attributed to the type.
- **Bare type names in docs:** Markdown now links unquoted multi-word
  CamelCase names, including acronym-led ones (`NotificationService`,
  `URLSession`, `NOAAProvider`), when they name exactly one type declaration.
  Single words, all-caps words and Swift extensions are never linked.

### Fixed & Improved

- **JS/TS and Python type relationships:**
  - Class heritage (`extends`, `implements`, Python base classes) becomes
    `inherits` edges.
  - TS type annotations and capitalized names used in code (`new Store()`,
    `Athlete.findAll`, `isinstance(x, Foo)`, type hints) become `references`
    edges.
  - Names resolve within their language family (Swift; JS/TS; Python) to a
    unique type declaration only.
  - Classes now `contain` their methods, and class bodies are attributed to
    the class.
  - Python decorators (`@cached(...)`) are attributed to the function or class
    they decorate.
  - `.mts` and `.cts` files are parsed as TypeScript and take part in type
    edges and import resolution.
  - Known limitation: a type name declared by two classes in the same language
    family (two `User`s in one repo) stays unlinked, as in Swift. Resolving
    through the import path is a possible follow-up.
- **Calls in anonymous callbacks are kept:** in JS, TS and Python, calls outside
  any named function (Express route handlers, module-level code) are attributed
  to the file's module instead of being dropped.
- **File imports resolve into the graph:**
  - Relative JS/TS imports (including `./x.js` for `x.ts`, directory `index`
    files and `export … from` re-exports) become module → module `imports`
    edges.
  - So do Python relative and repo-local imports, including nested and `src/`
    layouts.
  - `affected` and `dependencies` follow these edges, and a `file:` seed covers
    everything the file declares. A file reached through an import at
    `--depth` 2 or more counts only its imports and top-level code, so deep
    queries stay bounded.
- **Faster git co-change walk:** commits are diffed by tree entries instead of
  line stats, so commits touching thousands of files no longer cost seconds
  each. On two benchmark repos indexing went from 17.4s to 4.5s and from 13.1s
  to 2.7s.

- **Swift extensions don't make a type name ambiguous:** Name lookups prefer
  a type's single declaration over its Swift extensions and over the module
  named after its file (`Foo.swift`).
- **Swift cross-file call resolution:** Calls now link across files in a Swift
  module. Type-qualified calls (`Type.method`, `Type.shared.method`,
  `self.method`) resolve by receiver type in every language. Swift calls whose
  name is unique among the repo's Swift declarations resolve as `INFERRED`,
  and bare `Type(...)` initializers link to the type. Qualifiers rooted at an
  unknown capitalized name are treated as external types and stay unresolved.
  Swift `extension` declarations are marked (`props.extension`) and never
  count as the type's definition.
- **Bare calls in methods respect language semantics:** A bare call inside a
  method prefers the caller's own type only in languages with implicit `self`
  (Swift, Kotlin, Java, C#, C++, Ruby). In Go, Python, JavaScript, TypeScript
  and PHP, `validate()` now links to the function `validate`, not a same-named
  method on the caller's type.
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

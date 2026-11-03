# Preliminary results: Synapse vs Graphify on a Swift app

These are early results from one repository. We're publishing them so people
testing Synapse know what it does well today and where it doesn't, measured
against [Graphify](https://pypi.org/project/graphifyy/) and against plain
`grep`.

## Summary

- **Synapse answered 7 of 8 code questions completely, at the lowest token
  cost.** That's about 3,600 estimated tokens. Counting a grep fallback for
  incomplete answers, it's about 5,200. Plain `grep`, `git` and file reads
  answered all 8 for about 13,100, and Graphify answered 3 of 8 for about
  14,700 with the same fallback.
- **The first run of this benchmark showed the gaps we had to fix.** Synapse on
  `main` answered only 3 of 8 (about 16,500 tokens with fallback). It had no
  conformance edges, no type-usage edges, and no impact, dependency or path
  queries. [#31](https://github.com/keelwright-hq/synapse/pull/31) adds them.
  The answer keys didn't change; one key error was corrected for every column
  (see [How we measured](#how-we-measured)).
- **Graph tools pay off most where grep has to read around matches:**
  - **Callers across files:** grep needed 5,713 tokens; Synapse 218 and
    Graphify 232.
  - **A type's dependencies:** grep 4,751; Synapse 324, complete. Graphify
    was incomplete.
- **Synapse is the only graph tool that answers history questions.** Its git
  co-change answered "what changes with `WeatherViewModel.swift`?" correctly.
  It took 549 tokens; a well-written `git log` pipeline took 138.

## Test setup

| | |
|---|---|
| Machine | Apple M5 Pro, 24 GB RAM, macOS 27.2 |
| Toolchain | Go 1.27.1, Python 3 (standard library only) |
| Synapse | **#31** at `e649265` (`feat/swift-type-graph`, not yet merged) and **`main`** at `7438375`. Graph history also shows `c2ae902`, `main` before #29 |
| Graphify | 0.9.45 (`graphifyy` on PyPI), `graphify . --code-only` then `graphify cluster-only . --no-label`, no LLM |
| Test repo | aeroflux at `3c04fb4`: a private Swift/SwiftUI iOS app with a widget extension. 134 Swift files, 15 Markdown files, 358 commits (117 merges) |

aeroflux is private, so only file paths and symbol names appear here; there's
no source code. You can't rerun these exact numbers, but you can run the same
harness on your own repo (see [Reproduce](#reproduce)).

Build commands:

```bash
graphify . --code-only
graphify cluster-only . --no-label
synapse --repo aeroflux --data-dir DATA index . --report --report-dir REPORT
```

## Token cost of answering code questions

### How we measured

- **8 questions, each with a hand-written answer key.** The keys come from the
  aeroflux source and git history at `3c04fb4`. Each key lists the expected
  items (callers, files, types), and an item counts as found when any of its
  names appears in a tool's output. All questions, answer keys and commands are
  in [`scripts/bench-graphify/tasks.json`](../scripts/bench-graphify/tasks.json).
- **Each question, three ways.** The command sequence is fixed and runs as an
  agent would run it. Failed attempts are kept, so they're paid for too: when a
  tool says a name is ambiguous, the next command uses whatever the tool
  suggested.
  - **grep:** `grep`, `git`, `cat` or `sed`.
  - **Graphify:** `affected`, `explain` and `path`.
  - **Synapse:** each column uses the query that fits the question in that
    version.
    - **`main`** has `neighborhood`, `co-changes` and `docs`. Its commands are
      in [`tasks-synapse-main.json`](../scripts/bench-graphify/tasks-synapse-main.json).
    - **#31** adds `affected`, `dependencies` and `path`.
- **Token estimate.** Tokens = characters ÷ 4 over each command and its full
  output. These are estimates; no tokenizer was used. No LLM was called, so
  repeat runs give identical numbers.
- **Incomplete answers.** "Tokens to answer" adds the grep cost when a graph
  tool's answer is incomplete, because an agent would still have to finish
  the job.
- **Bias toward grep.** The grep commands were written with the answer in
  hand: they target the right file, the right line range and the right git
  pipeline. Treat grep as a best case. The graph tools ran with their default
  options and no tuning.
- **Key correction:** the first version of the "affected by `Temperature`" key
  listed 21 files. Two of them, `UserPreferences.swift` and `DWDProvider.swift`,
  mention `Temperature` only in `///` doc comments, and `grep -w` had matched
  those. The key now lists 19 files, for every column.

### Results

Each cell shows estimated tokens, then items found out of items expected. `*`
means the answer was incomplete.

| Question | grep | Graphify | Synapse `main` | Synapse #31 |
|---|---|---|---|---|
| Callers of `WeatherViewModel.refresh` | 5,713 · 3/3 | 232 · 3/3 | 1,725 · 3/3 | **218 · 3/3** |
| Non-test files affected by `Temperature` | **285 · 19/19** | 5,037 · 19/19 | 2,025 · 8/19\* | 1,341 · 19/19 |
| Files `NOAAProvider` depends on | 4,751 · 7/7 | 620 · 2/7\* | 1,809 · 2/7\* | **324 · 7/7** |
| Call chain from `ContentView.refreshAndConsiderReview` to `WeatherRepository` | 231 · 2/2 | 113 · 0/2\* | 1,559 · 2/2 | **121 · 2/2** |
| Files that change with `WeatherViewModel.swift` | **138 · 4/4** | 264 · 0/4\* | 549 · 4/4 | 549 · 4/4 |
| Docs describing `NotificationService` | **17 · 1/1** | 672 · 0/1\* | 26 · 0/1\* | 45 · 1/1 |
| Types conforming to `WeatherProviderProtocol` | 427 · 10/10 | 410 · 10/10 | 261 · 0/10\* | **400 · 10/10** |
| What `runRefresh` does and who calls it | 1,563 · 7/7 | 676 · 4/7\* | 1,489 · 4/7\* | 593 · 4/7\* |
| **Total read** | 13,125 | 8,024 | 9,443 | **3,591** |
| Complete answers | **8/8** | 3/8 | 3/8 | 7/8 |
| Expected items found | **53/53** | 38/53 | 23/53 | 50/53 |
| **Tokens to answer (with fallback)** | 13,125 | 14,724 | 16,486 | **5,154** |
| Tool calls to answer | **11** | 22 | 16 | 12 |

What this means:

- **Grep is still the cheapest way to answer questions with a precise text
  pattern:** which files mention a type, which docs mention a name, or a
  history question with a well-built `git log` pipeline. It's also the only
  approach that got every item, though its commands were written knowing the
  answer.
- **Synapse #31 wins where grep has to read around matches:**
  - **Callers across files:** 218 tokens against grep's 5,713.
  - **A type's dependencies:** 324 against 4,751.
  - **A call chain:** 121 against 231.
- **Neither graph tool fully answered "what does `runRefresh` do".** Synapse
  missed the callees reached through protocol-typed properties. It has no type
  inference, and the repo has both a real implementation and a test fake, so
  it declines to guess. Graphify's `explain` stops after 20 connections.

## Graph quality

The same repository and commit, indexed by three Synapse versions, with
Graphify for comparison:

| | Synapse `c2ae902` | Synapse `7438375` | Synapse #31 | Graphify 0.9.45 |
|---|---|---|---|---|
| Nodes / edges | 2,803 / 8,817 | 2,502 / 5,502 | 2,886 / 7,732 | 2,122 / 4,954 |
| Call edges | 2,977 | 2,926 | 4,062 | 1,326 |
| …linked to a real declaration | 282 | 1,149 | 1,308 | 1,326 |
| …of which cross-file | 0 | 683 | 792 | 782 |
| Cross-file calls also in Graphify | n/a | 83% | 73% | n/a |
| Type-usage edges (`references`) | none | none | 998 | 1,703 |
| Conformance / inheritance edges | none | none | 38 (repo types only) | 244 / 37 (including Apple protocols) |
| `defer { }` recorded as a call | 30 | 0 | 0 | n/a |
| `// MARK:` recorded as a doc comment | 2 | 0 | 0 | n/a |
| Git co-change pairs | 4,217 | 953 | 953 | none |
| `WeatherViewModel` ↔ `NowDashboardView` co-change count | 46 | 21 | 21 | none |
| Markdown → code links / doc comments | 49 / 178 | 49 / 177 | 107 / 177 | none in code-only mode |
| Build time (median of 5 runs) | n/a | 2.12s | 2.22s | 1.23s build + 0.62s clustering |

Notes:

- **#31 records calls from SwiftUI `body` code.** Code in type bodies
  (property initializers, SwiftUI `body`) is now recorded, which accounts for
  most of the new call edges. Graphify attributes those calls to a separate
  `.body` node, so the edge endpoints don't match. That's why agreement on
  cross-file calls drops to 73%.
- **#31 only links to types declared in the repo.** Synapse creates
  `references` and `inherits` edges only when the name resolves to exactly
  one type declared in the repo. Graphify also records Apple types such as
  `View`, `Codable` and `String`.
- **Co-change: why the count dropped to 21.** `c2ae902` counted merge commits,
  which repeat changes already counted on the branch's own commits. The manual
  recount from non-merge history agrees with 21.
- **Co-change: why there are fewer pairs.** Many pairs only reached the
  minimum count of 2 because a merge repeated a single commit. Two 83-file
  merges alone produced thousands of pairs.

Structural checks all pass on both `7438375` and #31:

- no absolute paths leaked, no duplicate IDs, no edges pointing at missing nodes
- all 149 tracked Swift and Markdown files indexed
- every line range valid, and every declaration's name on its start line

## Where Graphify fell short

All of these were observed on this run:

1. **Method names aren't qualified by type, so lookups by name often fail.**
   - **Naming:** methods are named like `.refresh()`. 69 such names are shared
     by more than one node, covering 282 of 875 method nodes (32%).
   - **Lookup failures:** `graphify affected 'WeatherViewModel.refresh'` and
     `affected '.refresh()'` both return "No unique node match", and the error
     doesn't list the candidates. `affected 'Temperature'` failed the same way,
     even though only one node has exactly that name.
   - **Workaround:** we had to search `graph.json` for the internal ID and query
     by ID. That's two extra calls in the callers and impact questions.
2. **`path` follows edge direction by default.**
   - **Default:** "`.refreshAndConsiderReview()` → `WeatherRepository`" reported
     no path.
   - **With `--undirected`:** it returned `ContentView` → `WeatherRepository`
     through the initializer call that constructs the repository, not the
     runtime chain through `WeatherViewModel.refresh`.
3. **`explain` stops after 20 connections.** It printed "… and 14 more" for
   `runRefresh`. For `NOAAProvider` it gave 2 of the 7 files it depends on.
4. **No history, docs or doc comments in code-only mode.**
   - It skipped all 16 docs files, so "which docs describe X" and "what
     changes with X" can't be answered.
   - Getting docs requires LLM extraction, which costs tokens and needs an API
     key.
5. **Node ID collisions drop nodes.**
   - **What happened:** the build logged 242 warnings that a node ID was
     created by two files. Graphify keeps one node and drops the others.
   - **Where:** here they were all framework-import nodes across 18 modules,
     such as `Foundation` (88), `SwiftUI` (39) and `aeroflux` (18, from
     `@testable import`).
   - **Effect:** a lossy import graph, but no dropped declarations.
6. **Clusters are unnamed without an LLM.** The 95 communities stay
   "Community 0", "Community 1", and so on.
7. **27% of call edges are marked inferred.** That's 356 of 1,326, not
   extracted from syntax. The ones we spot-checked were plausible.

Where Graphify did better:

- **Faster builds:** 1.85s in total against Synapse's 2.22s, which includes
  the git history pass.
- **A complete "affected by `Temperature`" answer,** once we found the
  internal ID.
- **Edges for Apple framework types** (`View`, `Codable`), which Synapse
  doesn't record.

## Where Synapse falls short

1. **No type inference.**
   - **Example:** calls through a property typed as a protocol (`store.save()`
     where `store: StoreProtocol`) stay unresolved when several types
     implement the method. That's why `runRefresh` got 4/7.
   - **Unique names:** when a name is unique, a call through a variable links
     to it and is marked inferred. In a sample of 15 cross-file links Graphify
     didn't have, about 2 were wrong.
2. **Single-word type names in prose aren't linked to docs,** because a word
   like "Temperature" is too ambiguous. Backticks always work.
3. **Swift only, for now.** The new type relationships come from the Swift
   extractor. Other languages still have no `references` or `inherits` edges.
4. **Ambiguous names cost an extra call.** For example, `refresh` exists on two
   types. Unlike Graphify, the error lists the exact IDs to use instead.
5. **Co-change output is verbose.** It took 549 tokens where a `git log`
   pipeline took 138.

## What we didn't measure

- **No live agents.** The command sequences are scripted. A real agent might
  choose better or worse queries.
- **No exact tokenizer.** Every token figure is an estimate (characters ÷ 4).
- **One repository, one language.** The results may differ for Go,
  TypeScript or Python codebases.
- **No runtime signals.** Synapse's OpenTelemetry signal (`observed_calls`)
  wasn't used.
- **Not independent.** The questions were written before the #31 changes and
  weren't changed afterwards. Still, the same team wrote the questions and
  the fixes, so an independent task set would be a better test.

## Reproduce

On your own repository:

```bash
# 1. Build both graphs
cd /path/to/repo
graphify . --code-only
synapse --repo myrepo --data-dir /tmp/syn index .

# 2. Write tasks.json for your repo (see scripts/bench-graphify/README.md), then:
python3 scripts/bench-graphify/run.py \
  --repo /path/to/repo --repo-name myrepo \
  --graph /path/to/repo/graphify-out/graph.json \
  --synapse "$(command -v synapse)" --data-dir /tmp/syn \
  --tasks my-tasks.json --out results.json
```

The raw results behind this page:

- [`bench-data/aeroflux-tasks.json`](bench-data/aeroflux-tasks.json): the
  grep, Graphify and Synapse #31 runs.
- [`bench-data/aeroflux-tasks-synapse-main.json`](bench-data/aeroflux-tasks-synapse-main.json):
  the Synapse `main` run.
- [`bench-data/aeroflux-graph.json`](bench-data/aeroflux-graph.json): graph
  counts and build timings.

Each task file holds the commands, output sizes and scores. No output text is
included.

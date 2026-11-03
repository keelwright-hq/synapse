# Preliminary results: Synapse vs Graphify on a Swift app

These are early results from one repository. We're publishing them so people
testing Synapse know what it does well today and where it doesn't, measured
against [Graphify](https://pypi.org/project/graphifyy/) and against plain
`grep`.

## Summary

- **Grep was cheapest overall.** Across 8 scripted code questions, `grep`,
  `git` and file reads answered all 8 for about 13,100 estimated tokens. Fewer
  tokens doesn't help if the answer is wrong, so a graph tool's incomplete
  answer counts the grep cost on top. On that basis Graphify needed about
  15,000 tokens and Synapse about 16,500.
- **Graph tools win on cross-file callers.** Grep needed about 5,700 tokens to
  find the callers of `WeatherViewModel.refresh`. Graphify found them in 232
  and Synapse in about 1,700, both complete.
- **Synapse is the only graph tool that answers history questions.** Its git
  co-change answered "what changes with `WeatherViewModel.swift`?" correctly in
  549 tokens. Graphify has no git data. A well-written `git log` pipeline did it
  in 138.
- **The recent fixes made Synapse's graph usable for Swift.** Between `c2ae902`
  and `7438375`, calls linked to a real declaration went from 282 to 1,149, and
  cross-file calls from 0 to 683. 83% of those cross-file calls are also in
  Graphify's graph.

## Test setup

| | |
|---|---|
| Machine | Apple M5 Pro, 24 GB RAM, macOS 27.2 |
| Toolchain | Go 1.27.1, Python 3 (standard library only) |
| Synapse | `7438375` (`main` after #29). Before: `c2ae902` (`main` after #28) |
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
- **Each question, three ways.**
  - **grep:** `grep`, `git`, `cat` or `sed`.
  - **Graphify:** `affected`, `explain` and `path`.
  - **Synapse:** `query neighborhood`, `co-changes` and `docs`.

  The command sequence is fixed and runs as an agent would run it. Failed
  attempts are kept, so they're paid for too: when a tool says the name is
  ambiguous, the next command uses whatever the tool suggested.
- **Token estimate.** Tokens = characters ÷ 4 over each command and its full
  output. These are estimates; no tokenizer was used. No LLM was called, so
  repeat runs give identical numbers.
- **Incomplete answers.** "Tokens to answer" adds the grep cost when a graph
  tool's answer is incomplete, because an agent would still have to finish
  the job.
- **Bias toward grep.** The grep commands were written with the answer in
  hand: they target the right file, the right line range and the right git
  pipeline. Treat grep as a best case. A real agent usually reads more. The
  graph tools ran with their default options and no tuning.

### Results

Each cell shows estimated tokens, then items found out of items expected. `*`
means the answer was incomplete.

| Question | grep | Graphify | Synapse |
|---|---|---|---|
| Callers of `WeatherViewModel.refresh` | 5,713 · 3/3 | **232 · 3/3** | 1,725 · 3/3 |
| Non-test files affected by `Temperature` | **285 · 21/21** | 5,037 · 20/21\* | 2,025 · 8/21\* |
| Files `NOAAProvider` depends on | 4,751 · 7/7 | 620 · 2/7\* | 1,809 · 2/7\* |
| Call chain from `ContentView.refreshAndConsiderReview` to `WeatherRepository` | **231 · 2/2** | 113 · 0/2\* | 1,559 · 2/2 |
| Files that change with `WeatherViewModel.swift` | **138 · 4/4** | 264 · 0/4\* | 549 · 4/4 |
| Docs describing `NotificationService` | **17 · 1/1** | 672 · 0/1\* | 26 · 0/1\* |
| Types conforming to `WeatherProviderProtocol` | 427 · 10/10 | 410 · 10/10 | 261 · 0/10\* |
| What `runRefresh` does and who calls it | 1,563 · 7/7 | 676 · 4/7\* | 1,489 · 4/7\* |
| **Total read** | **13,125** | **8,024** | **9,443** |
| Complete answers | 8/8 | 2/8 | 3/8 |
| Expected items found | 55/55 | 39/55 | 23/55 |
| **Tokens to answer (with fallback)** | **13,125** | **15,009** | **16,486** |
| Tool calls to answer | 11 | 23 | 16 |

What this means:

- **For questions with a precise text pattern, grep wins:** who conforms to a
  protocol, which files mention a type, which docs mention a name.
- **Graph tools pay off for call relationships across files.** Grep has to read
  around every match, and it can't tell `WeatherViewModel.refresh` apart from
  `RadarTileService.refresh`.
- **Neither graph tool is reliable yet on broad "what does X touch" questions
  about a type.** Both returned a truncated list of members instead of its
  dependencies.

## Graph quality: Synapse before and after the Swift fixes

The same repository and commit, indexed by Synapse at `c2ae902` and at
`7438375`, with Graphify for comparison:

| | Synapse `c2ae902` | Synapse `7438375` | Graphify 0.9.45 |
|---|---|---|---|
| Nodes / edges | 2,803 / 8,817 | 2,502 / 5,502 | 2,122 / 4,954 |
| Calls linked to a real declaration | 282 | 1,149 | 1,326 |
| …of which cross-file | 0 | 683 (83% also in Graphify) | 782 |
| `defer { }` recorded as a call | 30 | 0 | n/a |
| `// MARK:` recorded as a doc comment | 2 | 0 | n/a |
| Git co-change pairs | 4,217 | 953 | none |
| `WeatherViewModel` ↔ `NowDashboardView` co-change count | 46 | 21 | none |
| Markdown → code links / doc comments | 49 / 178 | 49 / 177 | none in code-only mode |
| Conformance / inheritance edges | none | none | 244 / 37 |
| Build time (median of 5 runs) | n/a | 2.12s (parse + git history + report) | 1.23s build + 0.62s clustering |

Notes on the co-change numbers:

- **Why the count dropped to 21:** `c2ae902` counted merge commits, which
  repeat changes already counted on the branch's own commits. The manual
  recount from non-merge history agrees with 21.
- **Why there are fewer pairs:** many pairs only reached the minimum count of
  2 because a merge repeated a single commit. Two 83-file merges alone
  produced thousands of pairs.

Structural checks on `7438375` all pass:

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

Where Graphify did better than Synapse:

- **Protocol conformance:** 10/10 in 410 tokens.
- **More cross-file calls:** 782 vs 683.
- **Faster builds.**
- **The cheapest complete answer** on the callers question.

## Where Synapse fell short

1. **No conformance or inheritance edges** for any language. The protocol
   question got 0/10.
2. **A neighborhood query on a type mostly returns the type's own members.**
   - **Truncation:** the result stops at 32 nodes.
   - **Missing references:** a type used in a signature or property isn't
     linked. Only calls are, including initializers.
   - **Result:** "affected by `Temperature`" got 8/21, and "`NOAAProvider`
     depends on" got 2/7.
3. **Docs link only through backticked names or file paths.** The README
   mentions `NotificationService` as a plain word in a directory listing, so
   `query docs` found nothing.
4. **No path query.** The call-chain question worked only because the chain
   was two hops from the start.
5. **Some calls through variables link to the wrong method.** Synapse doesn't
   know a variable's type, so calls through variables are linked by unique
   method name and marked inferred. In a sample of 15 cross-file links that
   Graphify didn't have, about 2 were wrong.
6. **Ambiguous names cost an extra call.** For example, `refresh` exists on two
   types. Unlike Graphify, the error lists the exact IDs to use instead.

## What we didn't measure

- **No live agents.** The command sequences are scripted. A real agent might
  choose better or worse queries.
- **No exact tokenizer.** Every token figure is an estimate (characters ÷ 4).
- **One repository, one language.** The results may differ for Go,
  TypeScript or Python codebases.
- **No runtime signals.** Synapse's OpenTelemetry signal (`observed_calls`)
  wasn't used.

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

- [`bench-data/aeroflux-tasks.json`](bench-data/aeroflux-tasks.json): each
  task's commands, output sizes and scores. No output text is included.
- [`bench-data/aeroflux-graph.json`](bench-data/aeroflux-graph.json): graph
  counts and build timings.

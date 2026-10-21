# Graph store benchmarks

Badger-backed graph store benchmarks (SYN-5). Run on a developer machine:

```bash
go test -bench=. -benchmem ./internal/store/badger/
```

## Reference results (darwin/arm64, Apple M-series, Go 1.27)

Measured with `benchNodeCount = 10000` nodes plus chain edges:

| Benchmark | Result |
|-----------|--------|
| `BenchmarkPopulate10k` | ~8 full graph writes/sec (10k nodes + 9,999 edges per iteration) |
| `BenchmarkGetNode` | ~1.3M lookups/sec after populate (~1100 ns/op) |

Numbers vary by disk and CPU; run `go test -bench=. -benchmem ./internal/store/badger/` locally for authoritative figures.

## Semantic ranking & fusion benchmarks (SYN-89)

Ranking benchmarks compare Phase-1 static-only neighborhood traversal against fused multi-signal traversal (static AST + git co-change + runtime OTel traces + documentation links) as well as the attribution explain breakdown.

Run on a developer machine:

```bash
go test -bench=BenchmarkNeighborhood -benchmem ./internal/rank/
```

### Default edge and family weights

| Family | Edge Type | Default Weight | Provenance |
|--------|-----------|----------------|------------|
| `static` | `contains` | `1.0` | EXTRACTED |
| `static` | `implements` | `0.9` | EXTRACTED / INFERRED |
| `static` | `consumes` | `0.9` | EXTRACTED / INFERRED |
| `static` | `calls` | `0.8` | EXTRACTED / INFERRED |
| `static` | `imports` | `0.5` | EXTRACTED |
| `git` | `co_committed` | `0.7` | HISTORICAL |
| `runtime` | `observed_calls` | `0.85` | OBSERVED |
| `doc` | `documents` | `0.6` | EXTRACTED / INFERRED |
| *fallback* | *unknown* | `0.3` | — |

All families default to a multiplier of `1.0` in `DefaultFamilyWeights` (`static: 1.0`, `git: 1.0`, `runtime: 1.0`, `doc: 1.0`).

### Reference results (darwin/arm64, Apple M-series, Go 1.27)

Measured on a benchmark fixture (50 files, 300 functions; depth=3, max_nodes=32). The baseline evaluates static AST edges (`contains`, `calls`). Semantic fusion evaluates the enriched multi-signal graph with static, git co-change, runtime `observed_calls`, and doc links:

| Benchmark | Latency | Memory / Op | Allocations | Throughput |
|-----------|---------|-------------|-------------|------------|
| `BenchmarkNeighborhoodPhase1Baseline` | ~118 µs/op | ~35.1 KB/op | 325 allocs/op | ~8.5k req/sec |
| `BenchmarkNeighborhoodSemanticFusion` | ~373 µs/op | ~104.5 KB/op | 888 allocs/op | ~2.7k req/sec |
| `BenchmarkNeighborhoodSemanticFusionExplain` | ~402 µs/op | ~143.9 KB/op | 1151 allocs/op | ~2.5k req/sec |

Because the fused semantic graph connects nodes across runtime calls, git co-commits, and documentation links, the BFS traverses a significantly richer candidate neighborhood prior to scoring and truncation. Fused ranking remains well within sub-millisecond territory (~0.37 ms), and capturing the per-family attribution breakdown (`Explain: true`) adds ~29 µs overhead.

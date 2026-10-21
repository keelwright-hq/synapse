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

Measured on a multi-family graph fixture (50 files, 300 functions, co-change, runtime calls, and doc links; depth=3, max_nodes=32):

| Benchmark | Latency | Memory / Op | Allocations | Throughput |
|-----------|---------|-------------|-------------|------------|
| `BenchmarkNeighborhoodPhase1Baseline` | ~107 µs/op | ~39.0 KB/op | 342 allocs/op | ~9.3k req/sec |
| `BenchmarkNeighborhoodSemanticFusion` | ~81 µs/op | ~37.0 KB/op | 323 allocs/op | ~12.3k req/sec |
| `BenchmarkNeighborhoodSemanticFusionExplain` | ~88 µs/op | ~53.0 KB/op | 440 allocs/op | ~11.3k req/sec |

Fused semantic ranking operates with sub-millisecond latency, and adding the detailed per-family `explain` trace adds minimal overhead (~7 µs / 16 KB per query).

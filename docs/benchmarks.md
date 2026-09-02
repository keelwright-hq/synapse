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

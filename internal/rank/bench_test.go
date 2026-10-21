package rank_test

import (
	"fmt"
	"testing"

	"github.com/keelwright-hq/synapse/internal/graph"
	"github.com/keelwright-hq/synapse/internal/parse"
	"github.com/keelwright-hq/synapse/internal/rank"
	"github.com/keelwright-hq/synapse/internal/store/memory"
)

// buildBenchmarkGraph populates an in-memory graph with static AST edges,
// git co-change edges, runtime observed_calls edges, and doc links.
func buildBenchmarkGraph(b *testing.B, numFiles int, funcsPerFile int) (graph.Store, graph.NodeID) {
	b.Helper()
	store := memory.New()

	var seedID graph.NodeID
	for f := 0; f < numFiles; f++ {
		filePath := fmt.Sprintf("pkg/mod%d/file%d.go", f/5, f)
		fileID := graph.NodeID("file:" + filePath)
		_ = store.PutNode(graph.Node{
			ID:   fileID,
			Kind: parse.KindFile,
			Name: fmt.Sprintf("file%d.go", f),
			Path: filePath,
		})

		for fn := 0; fn < funcsPerFile; fn++ {
			fnID := graph.NodeID(fmt.Sprintf("func:%s#Func%d", filePath, fn))
			if seedID == "" {
				seedID = fnID
			}
			_ = store.PutNode(graph.Node{
				ID:   fnID,
				Kind: parse.KindFunction,
				Name: fmt.Sprintf("Func%d", fn),
				Path: filePath,
				Props: map[string]string{
					"start_line": fmt.Sprintf("%d", fn*10+1),
					"end_line":   fmt.Sprintf("%d", fn*10+8),
					parse.PropDoc: fmt.Sprintf("Documentation for Func%d", fn),
				},
			})
			_ = store.PutEdge(graph.Edge{
				From: fileID,
				To:   fnID,
				Type: parse.EdgeContains,
			})

			// Static call to a function in another file
			targetFn := graph.NodeID(fmt.Sprintf("func:pkg/mod%d/file%d.go#Func%d", ((f+1)%numFiles)/5, (f+1)%numFiles, fn))
			_ = store.PutEdge(graph.Edge{
				From: fnID,
				To:   targetFn,
				Type: parse.EdgeCalls,
			})

			// Runtime observed_calls edge
			runtimeTarget := graph.NodeID(fmt.Sprintf("func:pkg/mod%d/file%d.go#Func%d", ((f+2)%numFiles)/5, (f+2)%numFiles, (fn+1)%funcsPerFile))
			_ = store.PutEdge(graph.Edge{
				From: fnID,
				To:   runtimeTarget,
				Type: parse.EdgeObservedCalls,
				Props: map[string]string{
					parse.PropCount:      "42",
					parse.PropProvenance: parse.ProvenanceObserved,
				},
			})
		}

		// Git co-committed edge to next file
		nextFileID := graph.NodeID(fmt.Sprintf("file:pkg/mod%d/file%d.go", ((f+1)%numFiles)/5, (f+1)%numFiles))
		_ = store.PutEdge(graph.Edge{
			From: fileID,
			To:   nextFileID,
			Type: parse.EdgeCoCommitted,
			Props: map[string]string{
				parse.PropWeight:  "0.85",
				parse.PropSupport: "12",
			},
		})

		// Doc node referencing the file and functions
		if f%5 == 0 {
			docID := graph.NodeID(fmt.Sprintf("doc:docs/arch%d.md#overview", f))
			_ = store.PutNode(graph.Node{
				ID:   docID,
				Kind: parse.KindDoc,
				Name: fmt.Sprintf("Architecture Doc %d", f),
				Path: fmt.Sprintf("docs/arch%d.md", f),
			})
			_ = store.PutEdge(graph.Edge{
				From: docID,
				To:   fileID,
				Type: parse.EdgeDocuments,
			})
		}
	}

	return store, seedID
}

// Phase1BaselineWeights only includes static AST edges.
var Phase1BaselineWeights = map[graph.EdgeType]float64{
	parse.EdgeContains:   1.0,
	parse.EdgeCalls:      0.8,
	parse.EdgeImplements: 0.9,
	parse.EdgeConsumes:   0.9,
	parse.EdgeImports:    0.5,
}

// BenchmarkNeighborhoodPhase1Baseline measures neighborhood ranking performance
// with Phase-1 static-only edge weights (SYN-89).
func BenchmarkNeighborhoodPhase1Baseline(b *testing.B) {
	store, seed := buildBenchmarkGraph(b, 50, 6)

	opts := rank.Options{
		Depth:    3,
		MaxNodes: 32,
		Weights:  Phase1BaselineWeights,
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		res, err := rank.Neighborhood(store, seed, opts)
		if err != nil {
			b.Fatalf("Neighborhood: %v", err)
		}
		if len(res.Hits) == 0 {
			b.Fatal("expected hits")
		}
	}
}

// BenchmarkNeighborhoodSemanticFusion measures neighborhood ranking performance
// with fused semantic edge weights (static + git + runtime + doc) (SYN-89).
func BenchmarkNeighborhoodSemanticFusion(b *testing.B) {
	store, seed := buildBenchmarkGraph(b, 50, 6)

	opts := rank.Options{
		Depth:    3,
		MaxNodes: 32,
		Weights:  rank.DefaultEdgeWeights,
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		res, err := rank.Neighborhood(store, seed, opts)
		if err != nil {
			b.Fatalf("Neighborhood: %v", err)
		}
		if len(res.Hits) == 0 {
			b.Fatal("expected hits")
		}
	}
}

// BenchmarkNeighborhoodSemanticFusionExplain measures the overhead of capturing
// score attribution breakdown (Explain: true) during fused ranking.
func BenchmarkNeighborhoodSemanticFusionExplain(b *testing.B) {
	store, seed := buildBenchmarkGraph(b, 50, 6)

	opts := rank.Options{
		Depth:    3,
		MaxNodes: 32,
		Weights:  rank.DefaultEdgeWeights,
		Explain:  true,
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		res, err := rank.Neighborhood(store, seed, opts)
		if err != nil {
			b.Fatalf("Neighborhood: %v", err)
		}
		if len(res.Hits) == 0 {
			b.Fatal("expected hits")
		}
	}
}

package rank_test

import (
	"fmt"
	"testing"

	"github.com/keelwright-hq/synapse/internal/graph"
	"github.com/keelwright-hq/synapse/internal/parse"
	"github.com/keelwright-hq/synapse/internal/rank"
	"github.com/keelwright-hq/synapse/internal/store/memory"
)

// buildBenchmarkGraph populates an in-memory graph.
// If includeSemantic is false, only static AST nodes and edges (file, function, contains, calls)
// are created to establish a true Phase-1 baseline graph.
// If includeSemantic is true, git co-change, runtime observed_calls, and doc nodes/edges are also added.
// All nodes are created in pass 1 before inserting edges in pass 2, guaranteeing endpoints exist.
func buildBenchmarkGraph(tb testing.TB, numFiles int, funcsPerFile int, includeSemantic bool) (graph.Store, graph.NodeID) {
	tb.Helper()
	store := memory.New()

	var seedID graph.NodeID

	// Pass 1: Put all nodes so both endpoints exist for every edge.
	for f := 0; f < numFiles; f++ {
		filePath := fmt.Sprintf("pkg/mod%d/file%d.go", f/5, f)
		fileID := graph.NodeID("file:" + filePath)
		if err := store.PutNode(graph.Node{
			ID:   fileID,
			Kind: parse.KindFile,
			Name: fmt.Sprintf("file%d.go", f),
			Path: filePath,
		}); err != nil {
			tb.Fatalf("PutNode file: %v", err)
		}

		for fn := 0; fn < funcsPerFile; fn++ {
			fnID := graph.NodeID(fmt.Sprintf("func:%s#Func%d", filePath, fn))
			if seedID == "" {
				seedID = fnID
			}
			if err := store.PutNode(graph.Node{
				ID:   fnID,
				Kind: parse.KindFunction,
				Name: fmt.Sprintf("Func%d", fn),
				Path: filePath,
				Props: map[string]string{
					"start_line":  fmt.Sprintf("%d", fn*10+1),
					"end_line":    fmt.Sprintf("%d", fn*10+8),
					parse.PropDoc: fmt.Sprintf("Documentation for Func%d", fn),
				},
			}); err != nil {
				tb.Fatalf("PutNode function: %v", err)
			}
		}

		if includeSemantic && f%5 == 0 {
			docID := graph.NodeID(fmt.Sprintf("doc:docs/arch%d.md#overview", f))
			if err := store.PutNode(graph.Node{
				ID:   docID,
				Kind: parse.KindDoc,
				Name: fmt.Sprintf("Architecture Doc %d", f),
				Path: fmt.Sprintf("docs/arch%d.md", f),
			}); err != nil {
				tb.Fatalf("PutNode doc: %v", err)
			}
		}
	}

	// Pass 2: Put all edges. Fail immediately if PutEdge returns an error.
	for f := 0; f < numFiles; f++ {
		filePath := fmt.Sprintf("pkg/mod%d/file%d.go", f/5, f)
		fileID := graph.NodeID("file:" + filePath)

		for fn := 0; fn < funcsPerFile; fn++ {
			fnID := graph.NodeID(fmt.Sprintf("func:%s#Func%d", filePath, fn))

			// Static contains edge
			if err := store.PutEdge(graph.Edge{
				From: fileID,
				To:   fnID,
				Type: parse.EdgeContains,
			}); err != nil {
				tb.Fatalf("PutEdge contains: %v", err)
			}

			// Static calls edge to function in another file
			targetFn := graph.NodeID(fmt.Sprintf("func:pkg/mod%d/file%d.go#Func%d", ((f+1)%numFiles)/5, (f+1)%numFiles, fn))
			if err := store.PutEdge(graph.Edge{
				From: fnID,
				To:   targetFn,
				Type: parse.EdgeCalls,
			}); err != nil {
				tb.Fatalf("PutEdge calls: %v", err)
			}

			if includeSemantic {
				// Runtime observed_calls edge
				runtimeTarget := graph.NodeID(fmt.Sprintf("func:pkg/mod%d/file%d.go#Func%d", ((f+2)%numFiles)/5, (f+2)%numFiles, (fn+1)%funcsPerFile))
				if err := store.PutEdge(graph.Edge{
					From: fnID,
					To:   runtimeTarget,
					Type: parse.EdgeObservedCalls,
					Props: map[string]string{
						parse.PropCount:      "42",
						parse.PropProvenance: parse.ProvenanceObserved,
					},
				}); err != nil {
					tb.Fatalf("PutEdge observed_calls: %v", err)
				}
			}
		}

		if includeSemantic {
			// Git co-committed edge to next file
			nextFileID := graph.NodeID(fmt.Sprintf("file:pkg/mod%d/file%d.go", ((f+1)%numFiles)/5, (f+1)%numFiles))
			if err := store.PutEdge(graph.Edge{
				From: fileID,
				To:   nextFileID,
				Type: parse.EdgeCoCommitted,
				Props: map[string]string{
					parse.PropWeight:  "0.85",
					parse.PropSupport: "12",
				},
			}); err != nil {
				tb.Fatalf("PutEdge co_committed: %v", err)
			}

			// Doc documents edge to file
			if f%5 == 0 {
				docID := graph.NodeID(fmt.Sprintf("doc:docs/arch%d.md#overview", f))
				if err := store.PutEdge(graph.Edge{
					From: docID,
					To:   fileID,
					Type: parse.EdgeDocuments,
				}); err != nil {
					tb.Fatalf("PutEdge documents: %v", err)
				}
			}
		}
	}

	return store, seedID
}

// TestBenchmarkGraphSanity validates the baseline and fusion fixtures before benchmarking.
func TestBenchmarkGraphSanity(t *testing.T) {
	// Baseline fixture: static only
	baseStore, baseSeed := buildBenchmarkGraph(t, 20, 4, false)
	calls, err := baseStore.OutEdges(baseSeed, parse.EdgeCalls)
	if err != nil || len(calls) == 0 {
		t.Fatalf("expected calls edges from base seed, got %v (err=%v)", calls, err)
	}
	obs, err := baseStore.OutEdges(baseSeed, parse.EdgeObservedCalls)
	if err != nil || len(obs) != 0 {
		t.Fatalf("expected 0 observed_calls edges in baseline graph, got %v", obs)
	}

	// Fusion fixture: static + git + runtime + doc
	fuseStore, fuseSeed := buildBenchmarkGraph(t, 20, 4, true)
	calls, err = fuseStore.OutEdges(fuseSeed, parse.EdgeCalls)
	if err != nil || len(calls) == 0 {
		t.Fatalf("expected calls edges on fusion seed, got %v (err=%v)", calls, err)
	}
	obs, err = fuseStore.OutEdges(fuseSeed, parse.EdgeObservedCalls)
	if err != nil || len(obs) == 0 {
		t.Fatalf("expected observed_calls edges on fusion seed, got %v (err=%v)", obs, err)
	}

	seedNode, err := fuseStore.GetNode(fuseSeed)
	if err != nil {
		t.Fatalf("GetNode(fuseSeed): %v", err)
	}
	fileID := graph.NodeID("file:" + seedNode.Path)
	coComm, err := fuseStore.OutEdges(fileID, parse.EdgeCoCommitted)
	if err != nil || len(coComm) == 0 {
		t.Fatalf("expected co_committed edges on file %s, got %v (err=%v)", fileID, coComm, err)
	}

	// Verify that Neighborhood on fusion returns semantic hits
	res, err := rank.Neighborhood(fuseStore, fuseSeed, rank.Options{
		Depth:    3,
		MaxNodes: 32,
		Weights:  rank.DefaultEdgeWeights,
		Explain:  true,
	})
	if err != nil {
		t.Fatalf("Neighborhood: %v", err)
	}
	foundFamilies := make(map[rank.EdgeFamily]bool)
	for _, h := range res.Hits {
		for _, p := range h.Explain {
			foundFamilies[p.Family] = true
		}
	}
	if !foundFamilies[rank.FamilyStatic] || !foundFamilies[rank.FamilyRuntime] {
		t.Errorf("expected static and runtime families in explain trace, got %v", foundFamilies)
	}
}

// BenchmarkNeighborhoodPhase1Baseline measures neighborhood ranking on a static-only
// AST graph with Phase-1 edge types (SYN-89).
func BenchmarkNeighborhoodPhase1Baseline(b *testing.B) {
	store, seed := buildBenchmarkGraph(b, 50, 6, false)

	// Ensure fixture has calls edges and no semantic edges
	calls, err := store.OutEdges(seed, parse.EdgeCalls)
	if err != nil || len(calls) == 0 {
		b.Fatalf("expected calls edges on baseline seed, got %v (err=%v)", calls, err)
	}
	if obs, _ := store.OutEdges(seed, parse.EdgeObservedCalls); len(obs) != 0 {
		b.Fatalf("expected 0 observed_calls edges in baseline graph, got %v", obs)
	}

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

// BenchmarkNeighborhoodSemanticFusion measures neighborhood ranking on a multi-signal
// graph (static + git + runtime + doc) with fused default weights (SYN-89).
func BenchmarkNeighborhoodSemanticFusion(b *testing.B) {
	store, seed := buildBenchmarkGraph(b, 50, 6, true)

	// Require a calls neighbor, an observed_calls neighbor, and a co_committed file on seed
	calls, err := store.OutEdges(seed, parse.EdgeCalls)
	if err != nil || len(calls) == 0 {
		b.Fatalf("expected calls edges on fusion seed, got %v (err=%v)", calls, err)
	}
	obs, err := store.OutEdges(seed, parse.EdgeObservedCalls)
	if err != nil || len(obs) == 0 {
		b.Fatalf("expected observed_calls edges on fusion seed, got %v (err=%v)", obs, err)
	}
	seedNode, err := store.GetNode(seed)
	if err != nil {
		b.Fatalf("GetNode(seed): %v", err)
	}
	fileID := graph.NodeID("file:" + seedNode.Path)
	coComm, err := store.OutEdges(fileID, parse.EdgeCoCommitted)
	if err != nil || len(coComm) == 0 {
		b.Fatalf("expected co_committed edges on file %s, got %v (err=%v)", fileID, coComm, err)
	}

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
// per-family score attribution breakdown (Explain: true) during fused ranking.
func BenchmarkNeighborhoodSemanticFusionExplain(b *testing.B) {
	store, seed := buildBenchmarkGraph(b, 50, 6, true)

	// Require a calls neighbor, an observed_calls neighbor, and a co_committed file on seed
	calls, err := store.OutEdges(seed, parse.EdgeCalls)
	if err != nil || len(calls) == 0 {
		b.Fatalf("expected calls edges on fusion seed, got %v (err=%v)", calls, err)
	}
	obs, err := store.OutEdges(seed, parse.EdgeObservedCalls)
	if err != nil || len(obs) == 0 {
		b.Fatalf("expected observed_calls edges on fusion seed, got %v (err=%v)", obs, err)
	}
	seedNode, err := store.GetNode(seed)
	if err != nil {
		b.Fatalf("GetNode(seed): %v", err)
	}
	fileID := graph.NodeID("file:" + seedNode.Path)
	coComm, err := store.OutEdges(fileID, parse.EdgeCoCommitted)
	if err != nil || len(coComm) == 0 {
		b.Fatalf("expected co_committed edges on file %s, got %v (err=%v)", fileID, coComm, err)
	}

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

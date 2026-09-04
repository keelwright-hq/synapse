package analysis_test

import (
	"testing"

	"github.com/keelwright-hq/synapse/internal/graph"
	"github.com/keelwright-hq/synapse/internal/graph/analysis"
	"github.com/keelwright-hq/synapse/internal/parse"
)

func sampleGraph() ([]graph.Node, []graph.Edge) {
	nodes := []graph.Node{
		{ID: "file:app.go", Kind: parse.KindFile, Name: "app.go", Path: "app.go"},
		{ID: "file:api.go", Kind: parse.KindFile, Name: "api.go", Path: "api.go"},
		{ID: "file:db.go", Kind: parse.KindFile, Name: "db.go", Path: "db.go"},
		{ID: "func:app.go#main", Kind: parse.KindFunction, Name: "main", Path: "app.go"},
		{ID: "func:api.go#Handler", Kind: parse.KindFunction, Name: "Handler", Path: "api.go"},
		{ID: "func:db.go#Query", Kind: parse.KindFunction, Name: "Query", Path: "db.go"},
	}
	edges := []graph.Edge{
		{From: "func:app.go#main", To: "func:api.go#Handler", Type: parse.EdgeCalls},
		{From: "func:api.go#Handler", To: "func:db.go#Query", Type: parse.EdgeCalls},
		{From: "func:db.go#Query", To: "func:api.go#Handler", Type: parse.EdgeCalls}, // Cycle between api and db
	}
	return nodes, edges
}

func TestDetectCommunities(t *testing.T) {
	nodes, edges := sampleGraph()
	comms := analysis.DetectCommunities(nodes, edges)
	if len(comms) == 0 {
		t.Fatal("expected non-empty communities")
	}
}

func TestComputePageRankAndCentrality(t *testing.T) {
	nodes, edges := sampleGraph()
	ranks := analysis.RankCentrality(nodes, edges, 10)
	if len(ranks) == 0 {
		t.Fatal("expected non-empty centrality ranks")
	}
	if ranks[0].PageRank <= 0 {
		t.Fatalf("expected positive PageRank score, got %f", ranks[0].PageRank)
	}
}

func TestDetectCycles(t *testing.T) {
	nodes, edges := sampleGraph()
	cycles := analysis.DetectCycles(nodes, edges, 10)
	if len(cycles) == 0 {
		t.Fatal("expected cycle detection between Handler and Query")
	}
	if cycles[0].Length != 2 {
		t.Fatalf("expected cycle length 2, got %d", cycles[0].Length)
	}
}

func TestFindIndirectTracesAndShortestPath(t *testing.T) {
	nodes, edges := sampleGraph()
	traces := analysis.FindIndirectTraces(nodes, edges, 10)
	if len(traces) == 0 {
		t.Fatal("expected indirect trace main -> Handler -> Query")
	}
	path := analysis.ShortestPath(nodes, edges, "func:app.go#main", "func:db.go#Query")
	if len(path) != 3 {
		t.Fatalf("expected 3 nodes in shortest path, got %v", path)
	}
}

func TestAnalyzeKnowledgeGaps(t *testing.T) {
	nodes, edges := sampleGraph()
	comms := analysis.DetectCommunities(nodes, edges)
	gaps := analysis.AnalyzeKnowledgeGaps(nodes, edges, comms)
	_ = gaps
}

func TestGenerateQuestions(t *testing.T) {
	nodes, edges := sampleGraph()
	comms := analysis.DetectCommunities(nodes, edges)
	ranks := analysis.RankCentrality(nodes, edges, 10)
	cycles := analysis.DetectCycles(nodes, edges, 10)
	qs := analysis.GenerateQuestions(nodes, comms, ranks, cycles)
	if len(qs) == 0 {
		t.Fatal("expected generated questions")
	}
}

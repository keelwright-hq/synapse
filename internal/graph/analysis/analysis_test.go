package analysis_test

import (
	"strings"
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
	cycles, coverage := analysis.DetectCyclesReport(nodes, edges, 10)
	qs := analysis.GenerateQuestions(nodes, comms, ranks, cycles, coverage)
	if len(qs) == 0 {
		t.Fatal("expected generated questions")
	}
}

func jsCycleFixture() ([]graph.Node, []graph.Edge) {
	nodes := []graph.Node{
		{ID: "file:a.js", Kind: parse.KindFile, Name: "a.js", Path: "a.js"},
		{ID: "module:a.js", Kind: parse.KindModule, Name: "a", Path: "a.js"},
		{ID: "import:a.js#./b", Kind: parse.KindImport, Name: "./b", Path: "a.js"},
		{ID: "file:b.js", Kind: parse.KindFile, Name: "b.js", Path: "b.js"},
		{ID: "module:b.js", Kind: parse.KindModule, Name: "b", Path: "b.js"},
		{ID: "import:b.js#./a", Kind: parse.KindImport, Name: "./a", Path: "b.js"},
	}
	edges := []graph.Edge{
		{From: "file:a.js", To: "module:a.js", Type: parse.EdgeContains},
		{From: "module:a.js", To: "import:a.js#./b", Type: parse.EdgeImports},
		{From: "file:b.js", To: "module:b.js", Type: parse.EdgeContains},
		{From: "module:b.js", To: "import:b.js#./a", Type: parse.EdgeImports},
	}
	return nodes, edges
}

func TestDetectCyclesJSRelativeImports(t *testing.T) {
	nodes, edges := jsCycleFixture()
	cycles, coverage := analysis.DetectCyclesReport(nodes, edges, 10)
	if coverage.ImportEdgesResolved != 2 {
		t.Fatalf("expected 2 resolved imports, got %d/%d", coverage.ImportEdgesResolved, coverage.ImportEdgesTotal)
	}
	if len(cycles) == 0 {
		t.Fatal("expected cycle between a.js and b.js via resolved relative imports")
	}
	found := false
	for _, cyc := range cycles {
		ids := map[graph.NodeID]bool{}
		for _, id := range cyc.Path {
			ids[id] = true
		}
		hasA := ids["file:a.js"] || ids["module:a.js"]
		hasB := ids["file:b.js"] || ids["module:b.js"]
		if hasA && hasB {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected cycle involving a and b file/module nodes, got %+v", cycles)
	}
}

func TestCommunitiesDoNotMergeOnSharedSymbols(t *testing.T) {
	nodes := []graph.Node{
		{ID: "file:alpha.js", Kind: parse.KindFile, Name: "alpha.js", Path: "alpha.js"},
		{ID: "module:alpha.js", Kind: parse.KindModule, Name: "alpha", Path: "alpha.js"},
		{ID: "func:alpha.js#run", Kind: parse.KindFunction, Name: "run", Path: "alpha.js"},
		{ID: "file:beta.js", Kind: parse.KindFile, Name: "beta.js", Path: "beta.js"},
		{ID: "module:beta.js", Kind: parse.KindModule, Name: "beta", Path: "beta.js"},
		{ID: "func:beta.js#run", Kind: parse.KindFunction, Name: "run", Path: "beta.js"},
		{ID: "symbol:log", Kind: parse.KindSymbol, Name: "log"},
		{ID: "symbol:error", Kind: parse.KindSymbol, Name: "error"},
	}
	edges := []graph.Edge{
		{From: "file:alpha.js", To: "module:alpha.js", Type: parse.EdgeContains},
		{From: "module:alpha.js", To: "func:alpha.js#run", Type: parse.EdgeContains},
		{From: "func:alpha.js#run", To: "symbol:log", Type: parse.EdgeCalls},
		{From: "func:alpha.js#run", To: "symbol:error", Type: parse.EdgeCalls},
		{From: "file:beta.js", To: "module:beta.js", Type: parse.EdgeContains},
		{From: "module:beta.js", To: "func:beta.js#run", Type: parse.EdgeContains},
		{From: "func:beta.js#run", To: "symbol:log", Type: parse.EdgeCalls},
		{From: "func:beta.js#run", To: "symbol:error", Type: parse.EdgeCalls},
	}
	comms := analysis.DetectCommunities(nodes, edges)
	for _, c := range comms {
		name := strings.ToLower(c.Name)
		if name == "log" || name == "error" {
			t.Fatalf("community must not be named after unresolved symbol %q: %+v", c.Name, c)
		}
		for _, id := range c.NodeIDs {
			if strings.HasPrefix(string(id), "symbol:") {
				t.Fatalf("community must not include symbol nodes: %v", c.NodeIDs)
			}
		}
	}
	// Unrelated files sharing only unresolved symbols should not form one giant community.
	for _, c := range comms {
		hasAlpha := false
		hasBeta := false
		for _, id := range c.NodeIDs {
			if strings.Contains(string(id), "alpha") {
				hasAlpha = true
			}
			if strings.Contains(string(id), "beta") {
				hasBeta = true
			}
		}
		if hasAlpha && hasBeta {
			t.Fatalf("alpha and beta must not merge via shared symbols: %+v", c)
		}
	}
}

func TestRankCentralityExcludesImportOccurrences(t *testing.T) {
	nodes := []graph.Node{
		{ID: "file:app.js", Kind: parse.KindFile, Name: "app.js", Path: "app.js"},
		{ID: "module:app.js", Kind: parse.KindModule, Name: "app", Path: "app.js"},
		{ID: "import:app.js#dotenv", Kind: parse.KindImport, Name: "dotenv", Path: "app.js"},
		{ID: "file:cfg.js", Kind: parse.KindFile, Name: "cfg.js", Path: "cfg.js"},
		{ID: "module:cfg.js", Kind: parse.KindModule, Name: "cfg", Path: "cfg.js"},
		{ID: "import:cfg.js#dotenv", Kind: parse.KindImport, Name: "dotenv", Path: "cfg.js"},
		{ID: "func:app.js#boot", Kind: parse.KindFunction, Name: "boot", Path: "app.js"},
		{ID: "func:cfg.js#load", Kind: parse.KindFunction, Name: "load", Path: "cfg.js"},
	}
	edges := []graph.Edge{
		{From: "file:app.js", To: "module:app.js", Type: parse.EdgeContains},
		{From: "module:app.js", To: "import:app.js#dotenv", Type: parse.EdgeImports},
		{From: "module:app.js", To: "func:app.js#boot", Type: parse.EdgeContains},
		{From: "func:app.js#boot", To: "func:cfg.js#load", Type: parse.EdgeCalls},
		{From: "file:cfg.js", To: "module:cfg.js", Type: parse.EdgeContains},
		{From: "module:cfg.js", To: "import:cfg.js#dotenv", Type: parse.EdgeImports},
		{From: "module:cfg.js", To: "func:cfg.js#load", Type: parse.EdgeContains},
	}
	ranks := analysis.RankCentrality(nodes, edges, 20)
	for _, r := range ranks {
		if r.Kind == parse.KindImport || strings.Contains(strings.ToLower(r.Name), "dotenv") {
			t.Fatalf("import occurrence must not rank as God Node: %+v", r)
		}
		if r.Kind == parse.KindSymbol {
			t.Fatalf("symbol must not rank as God Node: %+v", r)
		}
	}
}

func TestCoverageNotesWhenUnresolvedImports(t *testing.T) {
	nodes := []graph.Node{
		{ID: "file:main.js", Kind: parse.KindFile, Name: "main.js", Path: "main.js"},
		{ID: "module:main.js", Kind: parse.KindModule, Name: "main", Path: "main.js"},
		{ID: "import:main.js#express", Kind: parse.KindImport, Name: "express", Path: "main.js"},
		{ID: "import:main.js#./util", Kind: parse.KindImport, Name: "./util", Path: "main.js"},
		// util target missing → relative import unresolved
	}
	edges := []graph.Edge{
		{From: "file:main.js", To: "module:main.js", Type: parse.EdgeContains},
		{From: "module:main.js", To: "import:main.js#express", Type: parse.EdgeImports},
		{From: "module:main.js", To: "import:main.js#./util", Type: parse.EdgeImports},
	}
	view := analysis.BuildDependencyView(nodes, edges)
	if view.Coverage.ImportEdgesTotal != 2 {
		t.Fatalf("expected 2 import edges, got %d", view.Coverage.ImportEdgesTotal)
	}
	if view.Coverage.ImportEdgesResolved != 0 {
		t.Fatalf("expected 0 resolved imports, got %d", view.Coverage.ImportEdgesResolved)
	}
	if len(view.Coverage.Notes) == 0 {
		t.Fatal("expected coverage notes when unresolved imports exist")
	}
	joined := strings.Join(view.Coverage.Notes, " ")
	if !strings.Contains(joined, "relative") && !strings.Contains(joined, "Unresolved") {
		t.Fatalf("expected notes mentioning resolution limits, got %v", view.Coverage.Notes)
	}
}

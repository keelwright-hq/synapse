package report_test

import (
	"strings"
	"testing"

	"github.com/keelwright-hq/synapse/internal/graph"
	"github.com/keelwright-hq/synapse/internal/parse"
	"github.com/keelwright-hq/synapse/internal/report"
)

func TestImportantSymbolsExcludesGenericSymbolHubs(t *testing.T) {
	nodes := []graph.Node{
		{ID: "file:a.go", Kind: parse.KindFile, Name: "a.go", Path: "a.go"},
		{ID: "file:b.go", Kind: parse.KindFile, Name: "b.go", Path: "b.go"},
		{ID: "func:a.go#Handle", Kind: parse.KindFunction, Name: "Handle", Path: "a.go"},
		{ID: "func:b.go#Serve", Kind: parse.KindFunction, Name: "Serve", Path: "b.go"},
		{ID: "symbol:error", Kind: parse.KindSymbol, Name: "error"},
		{ID: "symbol:log", Kind: parse.KindSymbol, Name: "log"},
		{ID: "symbol:String", Kind: parse.KindSymbol, Name: "String"},
	}
	edges := []graph.Edge{
		// Generic symbols dominate raw degree.
		{From: "func:a.go#Handle", To: "symbol:error", Type: parse.EdgeCalls},
		{From: "func:b.go#Serve", To: "symbol:error", Type: parse.EdgeCalls},
		{From: "func:a.go#Handle", To: "symbol:log", Type: parse.EdgeCalls},
		{From: "func:b.go#Serve", To: "symbol:log", Type: parse.EdgeCalls},
		{From: "func:a.go#Handle", To: "symbol:String", Type: parse.EdgeCalls},
		{From: "func:b.go#Serve", To: "symbol:String", Type: parse.EdgeCalls},
		{From: "func:a.go#Handle", To: "func:b.go#Serve", Type: parse.EdgeCalls},
		{From: "file:a.go", To: "func:a.go#Handle", Type: parse.EdgeContains},
		{From: "file:b.go", To: "func:b.go#Serve", Type: parse.EdgeContains},
		{From: "file:a.go", To: "file:b.go", Type: parse.EdgeImports},
	}

	syms := report.ImportantSymbols(nodes, edges, 10)
	if len(syms) == 0 {
		t.Fatal("expected important symbols")
	}
	for _, h := range syms {
		if h.Kind == parse.KindSymbol {
			t.Fatalf("ImportantSymbols must exclude kind=symbol, got %+v", h)
		}
		if strings.HasPrefix(string(h.ID), "symbol:") {
			t.Fatalf("unexpected symbol hub %s", h.ID)
		}
	}
	if syms[0].Name != "Serve" && syms[0].Name != "Handle" {
		t.Fatalf("top symbol want Handle or Serve, got %+v", syms[0])
	}

	files := report.ImportantFiles(nodes, edges, 10)
	if len(files) == 0 {
		t.Fatal("expected important files")
	}
	for _, h := range files {
		if h.Kind != parse.KindFile {
			t.Fatalf("ImportantFiles must be files only: %+v", h)
		}
	}
	// contains-only degree must not elevate files; a.go has imports+calls path.
	if files[0].ID != "file:a.go" && files[0].ID != "file:b.go" {
		t.Fatalf("unexpected top file %+v", files[0])
	}

	imps := report.TopImports(nodes, edges, 10)
	if len(imps) != 1 || imps[0].ID != "file:b.go" {
		t.Fatalf("TopImports want file:b.go inbound, got %+v", imps)
	}
}

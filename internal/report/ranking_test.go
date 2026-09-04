package report_test

import (
	"strings"
	"testing"

	"github.com/keelwright-hq/synapse/internal/graph"
	"github.com/keelwright-hq/synapse/internal/parse"
	"github.com/keelwright-hq/synapse/internal/report"
)

// parserShapedGraph mirrors JS/TS extractor output: file → module → imports/funcs,
// with imports/calls off the file node itself.
func parserShapedGraph() (nodes []graph.Node, edges []graph.Edge) {
	nodes = []graph.Node{
		{ID: "file:src/app.js", Kind: parse.KindFile, Name: "src/app.js", Path: "src/app.js"},
		{ID: "module:src/app.js", Kind: parse.KindModule, Name: "app", Path: "src/app.js"},
		{ID: "import:src/app.js#sequelize", Kind: parse.KindImport, Name: "sequelize", Path: "src/app.js"},
		{ID: "import:src/app.js#express", Kind: parse.KindImport, Name: "express", Path: "src/app.js"},
		{ID: "import:src/app.js#./db", Kind: parse.KindImport, Name: "./db", Path: "src/app.js"},
		{ID: "func:src/app.js#boot", Kind: parse.KindFunction, Name: "boot", Path: "src/app.js"},

		{ID: "file:src/api.js", Kind: parse.KindFile, Name: "src/api.js", Path: "src/api.js"},
		{ID: "module:src/api.js", Kind: parse.KindModule, Name: "api", Path: "src/api.js"},
		{ID: "import:src/api.js#sequelize", Kind: parse.KindImport, Name: "sequelize", Path: "src/api.js"},
		{ID: "import:src/api.js#express", Kind: parse.KindImport, Name: "express", Path: "src/api.js"},
		{ID: "import:src/api.js#../lib/util", Kind: parse.KindImport, Name: "../lib/util", Path: "src/api.js"},
		{ID: "func:src/api.js#handler", Kind: parse.KindFunction, Name: "handler", Path: "src/api.js"},

		{ID: "file:lib/util.js", Kind: parse.KindFile, Name: "lib/util.js", Path: "lib/util.js"},
		{ID: "module:lib/util.js", Kind: parse.KindModule, Name: "util", Path: "lib/util.js"},
		{ID: "import:lib/util.js#sequelize", Kind: parse.KindImport, Name: "sequelize", Path: "lib/util.js"},
		{ID: "func:lib/util.js#fmt", Kind: parse.KindFunction, Name: "fmt", Path: "lib/util.js"},

		{ID: "symbol:error", Kind: parse.KindSymbol, Name: "error"},
		{ID: "symbol:log", Kind: parse.KindSymbol, Name: "log"},
	}
	edges = []graph.Edge{
		{From: "file:src/app.js", To: "module:src/app.js", Type: parse.EdgeContains},
		{From: "module:src/app.js", To: "func:src/app.js#boot", Type: parse.EdgeContains},
		{From: "module:src/app.js", To: "import:src/app.js#sequelize", Type: parse.EdgeImports},
		{From: "module:src/app.js", To: "import:src/app.js#express", Type: parse.EdgeImports},
		{From: "module:src/app.js", To: "import:src/app.js#./db", Type: parse.EdgeImports},
		{From: "func:src/app.js#boot", To: "func:src/api.js#handler", Type: parse.EdgeCalls},
		{From: "func:src/app.js#boot", To: "symbol:error", Type: parse.EdgeCalls},
		{From: "func:src/app.js#boot", To: "symbol:log", Type: parse.EdgeCalls},

		{From: "file:src/api.js", To: "module:src/api.js", Type: parse.EdgeContains},
		{From: "module:src/api.js", To: "func:src/api.js#handler", Type: parse.EdgeContains},
		{From: "module:src/api.js", To: "import:src/api.js#sequelize", Type: parse.EdgeImports},
		{From: "module:src/api.js", To: "import:src/api.js#express", Type: parse.EdgeImports},
		{From: "module:src/api.js", To: "import:src/api.js#../lib/util", Type: parse.EdgeImports},
		{From: "func:src/api.js#handler", To: "func:lib/util.js#fmt", Type: parse.EdgeCalls},
		{From: "func:src/api.js#handler", To: "symbol:error", Type: parse.EdgeCalls},
		{From: "func:src/api.js#handler", To: "symbol:log", Type: parse.EdgeCalls},

		{From: "file:lib/util.js", To: "module:lib/util.js", Type: parse.EdgeContains},
		{From: "module:lib/util.js", To: "func:lib/util.js#fmt", Type: parse.EdgeContains},
		{From: "module:lib/util.js", To: "import:lib/util.js#sequelize", Type: parse.EdgeImports},
		{From: "func:lib/util.js#fmt", To: "symbol:error", Type: parse.EdgeCalls},
	}
	return nodes, edges
}

func TestImportantFilesAggregatesModuleAndFunctionEdges(t *testing.T) {
	nodes, edges := parserShapedGraph()
	files := report.ImportantFiles(nodes, edges, 10)
	if len(files) == 0 {
		t.Fatal("ImportantFiles empty: imports/calls must roll up to owning files")
	}
	byID := map[graph.NodeID]report.Hub{}
	for _, h := range files {
		if h.Kind != parse.KindFile {
			t.Fatalf("want file hubs only, got %+v", h)
		}
		byID[h.ID] = h
	}
	// app.js: 3 imports + calls to handler/error/log (3) attributed on From,
	// and handler inbound call on To for api — app gets From-side credits.
	if _, ok := byID["file:src/app.js"]; !ok {
		t.Fatalf("expected file:src/app.js in Important files, got %+v", files)
	}
	if _, ok := byID["file:src/api.js"]; !ok {
		t.Fatalf("expected file:src/api.js in Important files, got %+v", files)
	}
	// contains-only util still has 1 import + calls; must appear.
	if files[0].Degree < 1 {
		t.Fatalf("top file degree=%d", files[0].Degree)
	}
	// Direct file-node imports/calls would leave all degrees 0 with parser shape;
	// app should outrank a contains-only file if we wrongly counted only file endpoints.
	if byID["file:src/app.js"].Degree <= 0 {
		t.Fatalf("app.js degree not aggregated: %+v", byID["file:src/app.js"])
	}
}

func TestTopImportsGroupsNormalizedTargets(t *testing.T) {
	nodes, edges := parserShapedGraph()
	imps := report.TopImports(nodes, edges, 10)
	if len(imps) == 0 {
		t.Fatal("expected Top imports")
	}
	byName := map[string]int{}
	for _, h := range imps {
		byName[h.Name] = h.Degree
		if h.Degree == 1 && (h.Name == "sequelize" || h.Name == "express") {
			t.Fatalf("package %q must be grouped across files, degree=%d hubs=%+v", h.Name, h.Degree, imps)
		}
	}
	if byName["sequelize"] != 3 {
		t.Fatalf("sequelize want 3 importing files, got %d (%+v)", byName["sequelize"], imps)
	}
	if byName["express"] != 2 {
		t.Fatalf("express want 2 importing files, got %d (%+v)", byName["express"], imps)
	}
	// ./db from src/app.js → src/db; ../lib/util from src/api.js → lib/util
	if byName["src/db"] != 1 {
		t.Fatalf("resolved ./db → src/db want degree 1, got %+v", imps)
	}
	if byName["lib/util"] != 1 {
		t.Fatalf("resolved ../lib/util → lib/util want degree 1, got %+v", imps)
	}
	// Must not list per-file import IDs as the hub name.
	for _, h := range imps {
		if strings.HasPrefix(h.Name, "import:") || strings.Contains(h.Name, "#") {
			t.Fatalf("Top imports should be normalized specs, got %+v", h)
		}
	}
	if imps[0].Name != "sequelize" {
		t.Fatalf("top import want sequelize, got %+v", imps[0])
	}
}

func TestImportantSymbolsExcludesGenericSymbolHubs(t *testing.T) {
	nodes, edges := parserShapedGraph()
	syms := report.ImportantSymbols(nodes, edges, 10)
	if len(syms) == 0 {
		t.Fatal("expected important symbols")
	}
	for _, h := range syms {
		if h.Kind == parse.KindSymbol {
			t.Fatalf("ImportantSymbols must exclude kind=symbol, got %+v", h)
		}
	}
}

func TestNormalizeImportTarget(t *testing.T) {
	cases := []struct {
		importer, spec, want string
	}{
		{"src/app.js", "sequelize", "sequelize"},
		{"src/app.js", "lodash/fp", "lodash"},
		{"src/app.js", "@scope/pkg/sub", "@scope/pkg"},
		{"src/app.js", "./db", "src/db"},
		{"src/api.js", "../lib/util", "lib/util"},
		{"main.go", "github.com/foo/bar", "github.com/foo/bar"},
		{"main.go", "fmt", "fmt"},
	}
	for _, tc := range cases {
		got := report.NormalizeImportTarget(tc.importer, tc.spec)
		if got != tc.want {
			t.Fatalf("NormalizeImportTarget(%q,%q)=%q want %q", tc.importer, tc.spec, got, tc.want)
		}
	}
}

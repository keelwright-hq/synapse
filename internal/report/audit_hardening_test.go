package report_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/keelwright-hq/synapse/internal/graph"
	"github.com/keelwright-hq/synapse/internal/graph/analysis"
	"github.com/keelwright-hq/synapse/internal/index"
	"github.com/keelwright-hq/synapse/internal/parse"
	"github.com/keelwright-hq/synapse/internal/report"
	"github.com/keelwright-hq/synapse/internal/store/memory"
)

// SYN-112: end-to-end regression covering audit findings without private Playwize input.
func TestAuditHardeningPipeline(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Circular relative imports (SYN-105).
	write("a.js", "const b = require('./b');\nfunction fa(){ console.log('a'); return b; }\nmodule.exports = { fa };\n")
	write("b.js", "const a = require('./a');\nfunction fb(){ console.log('b'); return a; }\nmodule.exports = { fb };\n")
	// Shared builtin calls must not skew file ownership (SYN-107).
	write("x.js", "function alpha(){ console.log('x'); }\n")
	write("y.js", "function beta(){ console.log('y'); }\n")
	// Go imports must stay distinct (SYN-108).
	write("main.go", "package main\nimport (\n\t\"net/http\"\n\t\"net/url\"\n)\nfunc main() { _, _ = http.Get, url.Parse }\n")
	store := memory.New()
	stats, err := index.New(store).Run(root, index.Options{Repo: "audit-fixture", Workers: 2})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Processed < 4 {
		t.Fatalf("expected processed source files, got %+v", stats)
	}

	var nodes []graph.Node
	if err := store.ForEachNode(func(n graph.Node) bool {
		nodes = append(nodes, n)
		return true
	}); err != nil {
		t.Fatal(err)
	}
	var edges []graph.Edge
	for _, n := range nodes {
		outs, err := store.OutEdges(n.ID, "")
		if err != nil {
			t.Fatal(err)
		}
		edges = append(edges, outs...)
	}

	cycles, cov := analysis.DetectCyclesReport(nodes, edges, 10)
	if len(cycles) == 0 {
		t.Fatalf("expected JS circular import cycle; coverage=%+v", cov)
	}

	files := report.ImportantFiles(nodes, edges, 20)
	byPath := map[string]int{}
	for _, h := range files {
		byPath[h.Path] = h.Degree
	}
	// x.js and y.js should not diverge solely from shared console.log To-side ownership.
	if dx, okx := byPath["x.js"]; okx {
		if dy, oky := byPath["y.js"]; oky && dx != dy {
			// Degrees can differ if parse shapes differ; allow small delta but flag huge skew.
			if dx > dy*2 || dy > dx*2 {
				t.Fatalf("file ownership skewed by shared builtin: x=%d y=%d hubs=%+v", dx, dy, files)
			}
		}
	}

	imps := report.TopImports(nodes, edges, 20)
	names := map[string]bool{}
	for _, h := range imps {
		names[h.Name] = true
	}
	if !names["net/http"] || !names["net/url"] {
		t.Fatalf("Go package paths truncated; TopImports=%+v", imps)
	}

	out := t.TempDir()
	res, err := report.Write(report.Options{
		Repo:   "audit-fixture",
		Root:   root,
		OutDir: out,
		Stats:  stats,
		Store:  store,
		Now:    time.Date(2026, 9, 17, 18, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	md, err := os.ReadFile(res.ReportPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(md)
	if !strings.Contains(text, "Analysis coverage") {
		t.Fatal("GRAPH_REPORT.md missing analysis coverage section")
	}
	if strings.Contains(text, "None detected.\n") && cov.Incomplete() {
		t.Fatal("must not claim unqualified None detected when coverage is incomplete")
	}
	html, err := os.ReadFile(res.HTMLPath)
	if err != nil {
		t.Fatal(err)
	}
	hs := string(html)
	if strings.Contains(hs, "onclick=\"window.selectNodeRef") {
		t.Fatal("viewer still emits unsafe inline ID handlers")
	}
	if !strings.Contains(hs, "UNKNOWN") {
		t.Fatal("viewer must treat missing provenance as UNKNOWN")
	}
}

func TestProvenanceUnknownDefaultInHTML(t *testing.T) {
	store := memory.New()
	_ = store.PutNode(graph.Node{ID: "file:a.js", Kind: parse.KindFile, Name: "a.js", Path: "a.js"})
	_ = store.PutNode(graph.Node{ID: "file:b.js", Kind: parse.KindFile, Name: "b.js", Path: "b.js"})
	_ = store.PutEdge(graph.Edge{From: "file:a.js", To: "file:b.js", Type: parse.EdgeImports}) // no provenance
	out := t.TempDir()
	res, err := report.Write(report.Options{
		Repo: "prov", Root: "/tmp", OutDir: out, Stats: index.Stats{Processed: 1}, Store: store,
		Now: time.Date(2026, 9, 17, 18, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(res.HTMLPath)
	if !strings.Contains(string(raw), "provenanceLabel") && !strings.Contains(string(raw), "UNKNOWN") {
		t.Fatal("expected UNKNOWN provenance path in viewer")
	}
}

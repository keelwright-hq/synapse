package report_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/keelwright-hq/synapse/internal/graph"
	"github.com/keelwright-hq/synapse/internal/index"
	"github.com/keelwright-hq/synapse/internal/parse"
	"github.com/keelwright-hq/synapse/internal/report"
	"github.com/keelwright-hq/synapse/internal/store/memory"
)

func TestWriteArtifacts(t *testing.T) {
	store := memory.New()
	nodes := []graph.Node{
		{ID: "file:main.go", Kind: parse.KindFile, Name: "main.go", Path: "main.go", Props: map[string]string{"repo_uri": "repo://demo/main.go#file:main.go"}},
		{ID: "func:main.go#Hello", Kind: parse.KindFunction, Name: "Hello", Path: "main.go", Props: map[string]string{"repo_uri": "repo://demo/main.go#func:Hello"}},
		{ID: "symbol:fmt.Println", Kind: parse.KindSymbol, Name: "Println"},
	}
	for _, n := range nodes {
		if err := store.PutNode(n); err != nil {
			t.Fatal(err)
		}
	}
	edges := []graph.Edge{
		{From: "file:main.go", To: "func:main.go#Hello", Type: parse.EdgeContains},
		{From: "func:main.go#Hello", To: "symbol:fmt.Println", Type: parse.EdgeCalls},
	}
	for _, e := range edges {
		if err := store.PutEdge(e); err != nil {
			t.Fatal(err)
		}
	}

	out := t.TempDir()
	res, err := report.Write(report.Options{
		Repo:   "demo",
		Root:   "/tmp/demo",
		OutDir: out,
		Stats:  index.Stats{Processed: 1},
		Store:  store,
		Now:    time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{res.ManifestPath, res.GraphPath, res.ReportPath} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("missing %s: %v", p, err)
		}
	}

	var g struct {
		SchemaVersion int          `json:"schema_version"`
		Nodes         []graph.Node `json:"nodes"`
		Edges         []graph.Edge `json:"edges"`
	}
	raw, err := os.ReadFile(res.GraphPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	if g.SchemaVersion != report.SchemaVersion || len(g.Nodes) == 0 || len(g.Edges) == 0 {
		t.Fatalf("unexpected graph.json: %+v", g)
	}
	foundURI := false
	for _, n := range g.Nodes {
		if n.Props != nil && n.Props["repo_uri"] != "" {
			foundURI = true
			break
		}
	}
	if !foundURI {
		t.Fatal("expected repo_uri on at least one node")
	}

	var man map[string]any
	raw, err = os.ReadFile(res.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &man); err != nil {
		t.Fatal(err)
	}
	if man["repo"] != "demo" {
		t.Fatalf("manifest repo=%v", man["repo"])
	}
	if int(man["node_count"].(float64)) != 3 {
		t.Fatalf("node_count=%v", man["node_count"])
	}

	md, err := os.ReadFile(res.ReportPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(md), "Synapse graph report") || !strings.Contains(string(md), "3 nodes") {
		t.Fatalf("unexpected report:\n%s", md)
	}

	latest := filepath.Join(t.TempDir(), "latest")
	if err := report.CopyArtifacts(out, latest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(latest, "GRAPH_REPORT.md")); err != nil {
		t.Fatal(err)
	}
}

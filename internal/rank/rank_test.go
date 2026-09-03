package rank_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keelwright-hq/synapse/internal/graph"
	"github.com/keelwright-hq/synapse/internal/parse"
	"github.com/keelwright-hq/synapse/internal/rank"
	"github.com/keelwright-hq/synapse/internal/store/memory"
)

func TestNeighborhoodGolden(t *testing.T) {
	store := memory.New()
	seed := graph.NodeID("func:a.go#Alpha")
	mustPut(t, store, graph.Node{ID: "file:a.go", Kind: parse.KindFile, Name: "a.go", Path: "a.go"})
	mustPut(t, store, graph.Node{ID: seed, Kind: parse.KindFunction, Name: "Alpha", Path: "a.go",
		Props: map[string]string{"start_line": "3", "end_line": "5"}})
	mustPut(t, store, graph.Node{ID: "func:a.go#Beta", Kind: parse.KindFunction, Name: "Beta", Path: "a.go"})
	mustPut(t, store, graph.Node{ID: "symbol:Printf", Kind: parse.KindSymbol, Name: "Printf"})
	mustEdge(t, store, graph.Edge{From: "file:a.go", To: seed, Type: parse.EdgeContains})
	mustEdge(t, store, graph.Edge{From: "file:a.go", To: "func:a.go#Beta", Type: parse.EdgeContains})
	mustEdge(t, store, graph.Edge{From: seed, To: "symbol:Printf", Type: parse.EdgeCalls})

	res, err := rank.Neighborhood(store, seed, rank.Options{Depth: 2, MaxNodes: 10})
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.MarshalIndent(stabilize(res), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')

	golden := filepath.Join("testdata", "neighborhood.golden.json")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden (run UPDATE_GOLDEN=1): %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("golden mismatch\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestPackBudgetTightTruncatesSnippet(t *testing.T) {
	hits := []rank.Hit{
		{ID: "a", Kind: "function", Name: "A", Snippet: strings.Repeat("x", 200)},
	}
	out, trunc := rank.PackBudget(hits, 40)
	if !trunc || len(out) != 1 {
		t.Fatalf("got out=%v trunc=%v", out, trunc)
	}
	if len(out[0].Snippet) >= 200 {
		t.Fatalf("expected truncated snippet, got len=%d", len(out[0].Snippet))
	}
}

func TestPackBudgetDeterministic(t *testing.T) {
	hits := []rank.Hit{
		{ID: "a", Kind: "function", Name: "A", Snippet: "aaaaaaaaaa"},
		{ID: "b", Kind: "function", Name: "B", Snippet: "bbbbbbbbbb"},
		{ID: "c", Kind: "function", Name: "C", Snippet: "cccccccccc"},
	}
	out1, trunc1 := rank.PackBudget(hits, 120)
	out2, trunc2 := rank.PackBudget(hits, 120)
	if trunc1 != trunc2 || len(out1) != len(out2) {
		t.Fatalf("nondeterministic: %v/%v vs %v/%v", out1, trunc1, out2, trunc2)
	}
	if !trunc1 || len(out1) >= len(hits) {
		// budget is tight enough to truncate at least one
		if len(out1) == 0 {
			t.Fatal("expected at least one hit")
		}
	}
	b1, _ := json.Marshal(out1)
	b2, _ := json.Marshal(out2)
	if string(b1) != string(b2) {
		t.Fatalf("pack mismatch")
	}
}

func TestSnippetUsesLineRange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.go")
	body := "package p\n\nfunc Alpha() {\n\treturn\n}\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	store := memory.New()
	seed := graph.NodeID("func:a.go#Alpha")
	mustPut(t, store, graph.Node{ID: seed, Kind: parse.KindFunction, Name: "Alpha", Path: "a.go",
		Props: map[string]string{"start_line": "3", "end_line": "5"}})
	res, err := rank.Neighborhood(store, seed, rank.Options{Depth: 1, RootDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) == 0 || res.Hits[0].Snippet == "" {
		t.Fatalf("expected snippet, got %+v", res.Hits)
	}
	if res.Hits[0].StartLine != 3 || res.Hits[0].EndLine != 5 {
		t.Fatalf("line range: %+v", res.Hits[0])
	}
}

func stabilize(r rank.Result) rank.Result {
	for i := range r.Hits {
		r.Hits[i].Snippet = "" // root-dependent
	}
	return r
}

func mustPut(t *testing.T, s *memory.Store, n graph.Node) {
	t.Helper()
	if err := s.PutNode(n); err != nil {
		t.Fatal(err)
	}
}

func mustEdge(t *testing.T, s *memory.Store, e graph.Edge) {
	t.Helper()
	if err := s.PutEdge(e); err != nil {
		t.Fatal(err)
	}
}

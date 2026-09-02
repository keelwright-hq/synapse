package badger

import (
	"path/filepath"
	"testing"

	"github.com/taricsa/synapse/internal/graph"
	"github.com/taricsa/synapse/internal/graph/storetest"
)

func TestStoreConformance(t *testing.T) {
	storetest.RunConformance(t, func() graph.Store {
		dir := t.TempDir()
		s, err := Open(dir)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		return s
	})
}

func TestDurabilityAcrossRestart(t *testing.T) {
	dir := t.TempDir()

	s1, err := Open(dir)
	if err != nil {
		t.Fatalf("Open first: %v", err)
	}

	node := graph.Node{ID: "file:main.go", Kind: "file", Path: "main.go"}
	if err := s1.PutNode(node); err != nil {
		t.Fatalf("PutNode: %v", err)
	}
	if err := s1.PutNode(graph.Node{ID: "sym:main", Kind: "function", Name: "main"}); err != nil {
		t.Fatalf("PutNode sym: %v", err)
	}
	if err := s1.PutEdge(graph.Edge{From: "file:main.go", To: "sym:main", Type: "contains"}); err != nil {
		t.Fatalf("PutEdge: %v", err)
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("Close first: %v", err)
	}

	s2, err := Open(dir)
	if err != nil {
		t.Fatalf("Open second: %v", err)
	}
	defer s2.Close()

	got, err := s2.GetNode(node.ID)
	if err != nil {
		t.Fatalf("GetNode after restart: %v", err)
	}
	if got.ID != node.ID || got.Path != node.Path {
		t.Fatalf("node mismatch after restart: %+v", got)
	}

	out, err := s2.OutEdges("file:main.go", "")
	if err != nil {
		t.Fatalf("OutEdges after restart: %v", err)
	}
	if len(out) != 1 || out[0].To != "sym:main" {
		t.Fatalf("edges after restart: %+v", out)
	}
}

func TestDefaultDataDir(t *testing.T) {
	orig := defaultDataDir
	tmp := t.TempDir()
	defaultDataDir = tmp
	t.Cleanup(func() { defaultDataDir = orig })

	s, err := Open("")
	if err != nil {
		t.Fatalf("Open default: %v", err)
	}
	defer s.Close()

	want := filepath.Join(tmp, "graph")
	got, err := filepath.EvalSymlinks(s.dir)
	if err != nil {
		got = s.dir
	}
	wantEval, err := filepath.EvalSymlinks(want)
	if err != nil {
		wantEval = want
	}
	if got != wantEval {
		t.Fatalf("graph path = %q want %q", got, wantEval)
	}
}

func TestResolveGraphPathDoesNotSuffixMatch(t *testing.T) {
	path, err := resolveGraphPath(filepath.Join(t.TempDir(), "mygraph"))
	if err != nil {
		t.Fatalf("resolveGraphPath: %v", err)
	}
	if filepath.Base(path) != "graph" {
		t.Fatalf("expected .../mygraph/graph, got %q", path)
	}
}

func TestSlashContainingNodeIDsDoNotCollide(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	parent := graph.Node{ID: "cmd/synapse", Kind: "dir"}
	child := graph.Node{ID: "cmd/synapse/main.go", Kind: "file"}
	other := graph.Node{ID: "other", Kind: "file"}
	for _, n := range []graph.Node{parent, child, other} {
		if err := s.PutNode(n); err != nil {
			t.Fatalf("PutNode %s: %v", n.ID, err)
		}
	}

	// Parent has an edge whose type contains '/', which would collide under
	// slash-delimited keys with a scan of child.
	if err := s.PutEdge(graph.Edge{From: parent.ID, To: other.ID, Type: "main.go/foo"}); err != nil {
		t.Fatalf("PutEdge: %v", err)
	}
	if err := s.PutEdge(graph.Edge{From: child.ID, To: other.ID, Type: "imports"}); err != nil {
		t.Fatalf("PutEdge child: %v", err)
	}

	out, err := s.OutEdges(child.ID, "")
	if err != nil {
		t.Fatalf("OutEdges child: %v", err)
	}
	if len(out) != 1 || out[0].Type != "imports" {
		t.Fatalf("child OutEdges leaked parent edges: %+v", out)
	}

	typed, err := s.OutEdges(parent.ID, "main.go/foo")
	if err != nil {
		t.Fatalf("OutEdges typed: %v", err)
	}
	if len(typed) != 1 || typed[0].To != other.ID {
		t.Fatalf("typed OutEdges: %+v", typed)
	}
}

package badger

import (
	"errors"
	"testing"

	"github.com/keelwright-hq/synapse/internal/graph"
	"github.com/keelwright-hq/synapse/internal/uri"
)

func TestURIIndexUniqueAndLookup(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	u := "repo://synapse/a.go#func:Alpha"
	n := graph.Node{
		ID:   "func:a.go#Alpha",
		Kind: "function",
		Name: "Alpha",
		Path: "a.go",
		Props: map[string]string{uri.PropKey: u},
	}
	if err := s.PutNode(n); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetNodeByURI(u)
	if err != nil || got.ID != n.ID {
		t.Fatalf("GetNodeByURI: %+v %v", got, err)
	}

	conflict := graph.Node{
		ID:    "func:b.go#Alpha",
		Kind:  "function",
		Name:  "Alpha",
		Path:  "b.go",
		Props: map[string]string{uri.PropKey: u},
	}
	if err := s.PutNode(conflict); !errors.Is(err, graph.ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}

	if err := s.DeleteNode(n.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetNodeByURI(u); !errors.Is(err, graph.ErrNotFound) {
		t.Fatalf("expected not found after delete, got %v", err)
	}
}

func TestMigratePhase1ToSchemaV2(t *testing.T) {
	dir := t.TempDir()
	s1, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	nodes := []graph.Node{
		{ID: "file:a.go", Kind: "file", Path: "a.go", Name: "a.go"},
		{ID: "func:a.go#Alpha", Kind: "function", Path: "a.go", Name: "Alpha"},
		{ID: "symbol:Printf", Kind: "symbol", Path: "a.go", Name: "Printf"},
	}
	for _, n := range nodes {
		if err := s1.PutNode(n); err != nil {
			t.Fatal(err)
		}
	}
	if err := s1.Close(); err != nil {
		t.Fatal(err)
	}

	s2, err := OpenWithRepo(dir, "synapse")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s2.Close() })

	ver, err := s2.schemaVersion()
	if err != nil || ver != SchemaVersionCurrent {
		t.Fatalf("schema version=%d err=%v", ver, err)
	}

	fileURI := "repo://synapse/a.go#file"
	got, err := s2.GetNodeByURI(fileURI)
	if err != nil || got.ID != "file:a.go" {
		t.Fatalf("file uri: %+v %v", got, err)
	}
	fn, err := s2.GetNode("func:a.go#Alpha")
	if err != nil {
		t.Fatal(err)
	}
	if fn.Props[uri.PropKey] != "repo://synapse/a.go#func:Alpha" {
		t.Fatalf("func props: %+v", fn.Props)
	}
	sym, err := s2.GetNode("symbol:Printf")
	if err != nil {
		t.Fatal(err)
	}
	if sym.Props != nil && sym.Props[uri.PropKey] != "" {
		t.Fatalf("global symbol should not have uri: %+v", sym.Props)
	}
}

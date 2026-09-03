package memory

import (
	"errors"
	"testing"

	"github.com/taricsa/synapse/internal/graph"
	"github.com/taricsa/synapse/internal/uri"
)

func TestMemoryURIIndexAndMigrate(t *testing.T) {
	s := New()
	s.schemaVer = SchemaVersionPhase1
	s.uriIndex = make(map[string]graph.NodeID)

	if err := s.PutNode(graph.Node{ID: "file:pkg/a.ts", Kind: "file", Path: "pkg/a.ts", Name: "pkg/a.ts"}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutNode(graph.Node{ID: "func:pkg/a.ts#Baz", Kind: "function", Path: "pkg/a.ts", Name: "Baz"}); err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureSchema("monorepo"); err != nil {
		t.Fatal(err)
	}
	if s.SchemaVersion() != SchemaVersionCurrent {
		t.Fatalf("version %d", s.SchemaVersion())
	}
	want := "repo://monorepo/pkg/a.ts#func:Baz"
	got, err := s.GetNodeByURI(want)
	if err != nil || got.ID != "func:pkg/a.ts#Baz" {
		t.Fatalf("got %+v err=%v", got, err)
	}

	conflict := graph.Node{
		ID:    "func:other.ts#Baz",
		Kind:  "function",
		Path:  "other.ts",
		Name:  "Baz",
		Props: map[string]string{uri.PropKey: want},
	}
	if err := s.PutNode(conflict); !errors.Is(err, graph.ErrConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
}

func TestPutNodeConflictPreservesOldURIIndex(t *testing.T) {
	s := New()
	u := "repo://r/a.go#func:A"
	if err := s.PutNode(graph.Node{
		ID: "func:a.go#A", Kind: "function", Path: "a.go", Name: "A",
		Props: map[string]string{uri.PropKey: u},
	}); err != nil {
		t.Fatal(err)
	}
	// Attempt to rebind the same URI to a different node ID.
	err := s.PutNode(graph.Node{
		ID: "func:b.go#A", Kind: "function", Path: "b.go", Name: "A",
		Props: map[string]string{uri.PropKey: u},
	})
	if !errors.Is(err, graph.ErrConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
	got, err := s.GetNodeByURI(u)
	if err != nil || got.ID != "func:a.go#A" {
		t.Fatalf("index corrupted after conflict: %+v %v", got, err)
	}
	// Existing node must still own its URI after a failed rebind of that URI
	// onto the same node ID with a colliding secondary URI is covered above;
	// also verify changing a node's URI to one owned by another fails cleanly.
	other := "repo://r/b.go#func:B"
	if err := s.PutNode(graph.Node{
		ID: "func:b.go#B", Kind: "function", Path: "b.go", Name: "B",
		Props: map[string]string{uri.PropKey: other},
	}); err != nil {
		t.Fatal(err)
	}
	err = s.PutNode(graph.Node{
		ID: "func:b.go#B", Kind: "function", Path: "b.go", Name: "B",
		Props: map[string]string{uri.PropKey: u},
	})
	if !errors.Is(err, graph.ErrConflict) {
		t.Fatalf("expected conflict on URI steal, got %v", err)
	}
	got, err = s.GetNodeByURI(other)
	if err != nil || got.ID != "func:b.go#B" {
		t.Fatalf("old URI for b lost after failed steal: %+v %v", got, err)
	}
	got, err = s.GetNodeByURI(u)
	if err != nil || got.ID != "func:a.go#A" {
		t.Fatalf("a URI lost after failed steal: %+v %v", got, err)
	}
}

func TestEnsureSchemaConflictIsAtomic(t *testing.T) {
	s := New()
	s.schemaVer = SchemaVersionPhase1
	shared := "repo://r/x.go#func:Shared"
	// Bypass PutNode uniqueness to simulate a Phase-1 store with conflicting props.
	s.nodes["func:a.go#A"] = graph.Node{
		ID: "func:a.go#A", Kind: "function", Path: "a.go", Name: "A",
		Props: map[string]string{uri.PropKey: shared},
	}
	s.nodes["func:b.go#B"] = graph.Node{
		ID: "func:b.go#B", Kind: "function", Path: "b.go", Name: "B",
		Props: map[string]string{uri.PropKey: shared},
	}
	s.uriIndex = make(map[string]graph.NodeID)

	if err := s.EnsureSchema("r"); !errors.Is(err, graph.ErrConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
	if s.SchemaVersion() != SchemaVersionPhase1 {
		t.Fatalf("schema version advanced on failed migrate: %d", s.SchemaVersion())
	}
	if len(s.uriIndex) != 0 {
		t.Fatalf("uri index partially applied: %+v", s.uriIndex)
	}
}

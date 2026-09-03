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

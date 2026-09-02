package memory

import (
	"testing"

	"github.com/taricsa/synapse/internal/graph"
	"github.com/taricsa/synapse/internal/graph/storetest"
)

func TestStoreConformance(t *testing.T) {
	storetest.RunConformance(t, func() graph.Store {
		return New()
	})
}

func TestPropsDeepCopy(t *testing.T) {
	s := New()
	defer s.Close()

	props := map[string]string{"lang": "go"}
	if err := s.PutNode(graph.Node{ID: "n1", Kind: "symbol", Props: props}); err != nil {
		t.Fatalf("PutNode: %v", err)
	}
	props["lang"] = "mutated"

	got, err := s.GetNode("n1")
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if got.Props["lang"] != "go" {
		t.Fatalf("store saw caller mutation: %v", got.Props)
	}

	got.Props["lang"] = "caller"
	again, err := s.GetNode("n1")
	if err != nil {
		t.Fatalf("GetNode again: %v", err)
	}
	if again.Props["lang"] != "go" {
		t.Fatalf("caller mutated store props: %v", again.Props)
	}
}

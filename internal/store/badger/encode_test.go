package badger

import (
	"testing"

	"github.com/keelwright-hq/synapse/internal/graph"
)

func TestMarshalNodeRoundTrip(t *testing.T) {
	node := graph.Node{
		ID:   "sym:main",
		Kind: "function",
		Name: "main",
		Path: "cmd/synapse/main.go",
		Props: map[string]string{"lang": "go"},
	}
	data, err := marshalNode(node)
	if err != nil {
		t.Fatalf("marshalNode: %v", err)
	}
	got, err := unmarshalNode(data)
	if err != nil {
		t.Fatalf("unmarshalNode: %v", err)
	}
	if got.ID != node.ID || got.Kind != node.Kind || got.Name != node.Name {
		t.Fatalf("round-trip mismatch: got %+v want %+v", got, node)
	}
}

func TestMarshalEdgeRoundTrip(t *testing.T) {
	edge := graph.Edge{
		From: "a",
		To:   "b",
		Type: "calls",
		Props: map[string]string{"line": "42"},
	}
	data, err := marshalEdge(edge)
	if err != nil {
		t.Fatalf("marshalEdge: %v", err)
	}
	got, err := unmarshalEdge(data)
	if err != nil {
		t.Fatalf("unmarshalEdge: %v", err)
	}
	if got.From != edge.From || got.To != edge.To || got.Type != edge.Type {
		t.Fatalf("round-trip mismatch: got %+v want %+v", got, edge)
	}
}

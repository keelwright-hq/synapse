package storetest

import (
	"errors"
	"testing"

	"github.com/taricsa/synapse/internal/graph"
)

// RunConformance exercises a Store implementation for CRUD, adjacency, and delete semantics.
func RunConformance(t *testing.T, newStore func() graph.Store) {
	t.Run("nodeCRUD", func(t *testing.T) {
		s := newStore()
		defer s.Close()

		node := graph.Node{
			ID:   "file:main.go",
			Kind: "file",
			Path: "main.go",
		}
		if err := s.PutNode(node); err != nil {
			t.Fatalf("PutNode: %v", err)
		}

		got, err := s.GetNode(node.ID)
		if err != nil {
			t.Fatalf("GetNode: %v", err)
		}
		if got.ID != node.ID || got.Kind != node.Kind || got.Path != node.Path {
			t.Fatalf("GetNode mismatch: got %+v want %+v", got, node)
		}

		if err := s.DeleteNode(node.ID); err != nil {
			t.Fatalf("DeleteNode: %v", err)
		}
		_, err = s.GetNode(node.ID)
		if !errors.Is(err, graph.ErrNotFound) {
			t.Fatalf("expected ErrNotFound after delete, got %v", err)
		}
	})

	t.Run("edgeAdjacency", func(t *testing.T) {
		s := newStore()
		defer s.Close()

		nodes := []graph.Node{
			{ID: "a", Kind: "symbol"},
			{ID: "b", Kind: "symbol"},
			{ID: "c", Kind: "symbol"},
		}
		for _, n := range nodes {
			if err := s.PutNode(n); err != nil {
				t.Fatalf("PutNode %s: %v", n.ID, err)
			}
		}

		edges := []graph.Edge{
			{From: "a", To: "b", Type: "calls"},
			{From: "a", To: "c", Type: "imports"},
			{From: "b", To: "c", Type: "calls"},
		}
		for _, e := range edges {
			if err := s.PutEdge(e); err != nil {
				t.Fatalf("PutEdge: %v", err)
			}
		}

		outAll, err := s.OutEdges("a", "")
		if err != nil {
			t.Fatalf("OutEdges all: %v", err)
		}
		if len(outAll) != 2 {
			t.Fatalf("OutEdges all: got %d want 2", len(outAll))
		}

		outCalls, err := s.OutEdges("a", "calls")
		if err != nil {
			t.Fatalf("OutEdges calls: %v", err)
		}
		if len(outCalls) != 1 || outCalls[0].To != "b" {
			t.Fatalf("OutEdges calls: got %+v", outCalls)
		}

		inAll, err := s.InEdges("c", "")
		if err != nil {
			t.Fatalf("InEdges all: %v", err)
		}
		if len(inAll) != 2 {
			t.Fatalf("InEdges all: got %d want 2", len(inAll))
		}
	})

	t.Run("deleteNodeCascadesEdges", func(t *testing.T) {
		s := newStore()
		defer s.Close()

		if err := s.PutNode(graph.Node{ID: "x", Kind: "file"}); err != nil {
			t.Fatalf("PutNode x: %v", err)
		}
		if err := s.PutNode(graph.Node{ID: "y", Kind: "file"}); err != nil {
			t.Fatalf("PutNode y: %v", err)
		}
		if err := s.PutEdge(graph.Edge{From: "x", To: "y", Type: "imports"}); err != nil {
			t.Fatalf("PutEdge: %v", err)
		}

		if err := s.DeleteNode("x"); err != nil {
			t.Fatalf("DeleteNode: %v", err)
		}

		out, err := s.OutEdges("x", "")
		if err != nil {
			t.Fatalf("OutEdges after delete: %v", err)
		}
		if len(out) != 0 {
			t.Fatalf("expected no out-edges after node delete, got %d", len(out))
		}

		in, err := s.InEdges("y", "")
		if err != nil {
			t.Fatalf("InEdges after delete: %v", err)
		}
		if len(in) != 0 {
			t.Fatalf("expected no in-edges after node delete, got %d", len(in))
		}
	})

	t.Run("deleteEdge", func(t *testing.T) {
		s := newStore()
		defer s.Close()

		if err := s.PutNode(graph.Node{ID: "p", Kind: "symbol"}); err != nil {
			t.Fatalf("PutNode p: %v", err)
		}
		if err := s.PutNode(graph.Node{ID: "q", Kind: "symbol"}); err != nil {
			t.Fatalf("PutNode q: %v", err)
		}
		if err := s.PutEdge(graph.Edge{From: "p", To: "q", Type: "refs"}); err != nil {
			t.Fatalf("PutEdge: %v", err)
		}

		if err := s.DeleteEdge("p", "q", "refs"); err != nil {
			t.Fatalf("DeleteEdge: %v", err)
		}

		out, err := s.OutEdges("p", "")
		if err != nil {
			t.Fatalf("OutEdges: %v", err)
		}
		if len(out) != 0 {
			t.Fatalf("expected edge deleted, got %d edges", len(out))
		}
	})
}

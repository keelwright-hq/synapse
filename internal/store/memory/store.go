package memory

import (
	"fmt"
	"sync"

	"github.com/taricsa/synapse/internal/graph"
)

// Store is an in-memory graph.Store for tests and fast local use.
type Store struct {
	mu    sync.RWMutex
	nodes map[graph.NodeID]graph.Node
	edges map[string]graph.Edge
}

// New returns an empty in-memory store.
func New() *Store {
	return &Store{
		nodes: make(map[graph.NodeID]graph.Node),
		edges: make(map[string]graph.Edge),
	}
}

func (s *Store) Close() error {
	return nil
}

func (s *Store) PutNode(node graph.Node) error {
	if node.ID == "" {
		return fmt.Errorf("memory: node id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nodes[node.ID] = cloneNode(node)
	return nil
}

func (s *Store) GetNode(id graph.NodeID) (graph.Node, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	node, ok := s.nodes[id]
	if !ok {
		return graph.Node{}, graph.ErrNotFound
	}
	return cloneNode(node), nil
}

func (s *Store) DeleteNode(id graph.NodeID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.nodes[id]; !ok {
		return graph.ErrNotFound
	}
	delete(s.nodes, id)
	for key, edge := range s.edges {
		if edge.From == id || edge.To == id {
			delete(s.edges, key)
		}
	}
	return nil
}

func (s *Store) PutEdge(edge graph.Edge) error {
	if edge.From == "" || edge.To == "" || edge.Type == "" {
		return fmt.Errorf("memory: edge from, to, and type are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.nodes[edge.From]; !ok {
		return graph.ErrNotFound
	}
	if _, ok := s.nodes[edge.To]; !ok {
		return graph.ErrNotFound
	}
	s.edges[edgeKey(edge)] = cloneEdge(edge)
	return nil
}

func (s *Store) DeleteEdge(from graph.NodeID, to graph.NodeID, edgeType graph.EdgeType) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := edgeKey(graph.Edge{From: from, To: to, Type: edgeType})
	if _, ok := s.edges[key]; !ok {
		return graph.ErrNotFound
	}
	delete(s.edges, key)
	return nil
}

func (s *Store) OutEdges(from graph.NodeID, edgeType graph.EdgeType) ([]graph.Edge, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []graph.Edge
	for _, edge := range s.edges {
		if edge.From != from {
			continue
		}
		if edgeType != "" && edge.Type != edgeType {
			continue
		}
		out = append(out, cloneEdge(edge))
	}
	return out, nil
}

func (s *Store) InEdges(to graph.NodeID, edgeType graph.EdgeType) ([]graph.Edge, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var in []graph.Edge
	for _, edge := range s.edges {
		if edge.To != to {
			continue
		}
		if edgeType != "" && edge.Type != edgeType {
			continue
		}
		in = append(in, cloneEdge(edge))
	}
	return in, nil
}

func edgeKey(edge graph.Edge) string {
	return string(edge.From) + "\x00" + string(edge.Type) + "\x00" + string(edge.To)
}

func cloneNode(node graph.Node) graph.Node {
	node.Props = cloneProps(node.Props)
	return node
}

func cloneEdge(edge graph.Edge) graph.Edge {
	edge.Props = cloneProps(edge.Props)
	return edge
}

func cloneProps(props map[string]string) map[string]string {
	if props == nil {
		return nil
	}
	cp := make(map[string]string, len(props))
	for k, v := range props {
		cp[k] = v
	}
	return cp
}

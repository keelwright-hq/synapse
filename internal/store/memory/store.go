package memory

import (
	"fmt"
	"sync"

	"github.com/keelwright-hq/synapse/internal/graph"
	"github.com/keelwright-hq/synapse/internal/uri"
)

// Store is an in-memory graph.Store for tests and fast local use.
type Store struct {
	mu           sync.RWMutex
	nodes        map[graph.NodeID]graph.Node
	edges        map[string]graph.Edge
	fingerprints map[string]string
	owned        map[string][]graph.NodeID
	uriIndex     map[string]graph.NodeID // canonical repo:// → Node.ID
	schemaVer    int
	repoName     string
}

// New returns an empty in-memory store.
func New() *Store {
	return &Store{
		nodes:        make(map[graph.NodeID]graph.Node),
		edges:        make(map[string]graph.Edge),
		fingerprints: make(map[string]string),
		owned:        make(map[string][]graph.NodeID),
		uriIndex:     make(map[string]graph.NodeID),
		schemaVer:    SchemaVersionCurrent,
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
	if s.uriIndex == nil {
		s.uriIndex = make(map[string]graph.NodeID)
	}

	newURI := ""
	if node.Props != nil {
		newURI = node.Props[uri.PropKey]
	}

	// Validate conflicts before mutating the URI index (no txn rollback).
	if newURI != "" {
		if existing, ok := s.uriIndex[newURI]; ok && existing != node.ID {
			return fmt.Errorf("%w: uri %q already bound to %s", graph.ErrConflict, newURI, existing)
		}
	}

	if old, ok := s.nodes[node.ID]; ok && old.Props != nil {
		if oldURI := old.Props[uri.PropKey]; oldURI != "" && oldURI != newURI {
			delete(s.uriIndex, oldURI)
		}
	}

	if newURI != "" {
		s.uriIndex[newURI] = node.ID
	}

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

func (s *Store) GetNodeByURI(repoURI string) (graph.Node, error) {
	canonical, err := uri.Normalize(repoURI)
	if err != nil {
		return graph.Node{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	id, ok := s.uriIndex[canonical]
	if !ok {
		return graph.Node{}, graph.ErrNotFound
	}
	node, ok := s.nodes[id]
	if !ok {
		return graph.Node{}, graph.ErrNotFound
	}
	return cloneNode(node), nil
}

func (s *Store) DeleteNode(id graph.NodeID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	node, ok := s.nodes[id]
	if !ok {
		return graph.ErrNotFound
	}
	if node.Props != nil {
		if u := node.Props[uri.PropKey]; u != "" {
			delete(s.uriIndex, u)
		}
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

// ForEachNode invokes fn for every node. Iteration stops early if fn returns false.
func (s *Store) ForEachNode(fn func(graph.Node) bool) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, node := range s.nodes {
		if !fn(cloneNode(node)) {
			return nil
		}
	}
	return nil
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

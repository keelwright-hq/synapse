// Package federated provides a read-only graph.Store over multiple member stores.
package federated

import (
	"fmt"
	"sync"

	"github.com/taricsa/synapse/internal/graph"
	"github.com/taricsa/synapse/internal/uri"
)

// Member is one named backend store (typically one Badger DB per repo).
type Member struct {
	Name  string
	Store graph.Store
}

// Store is a lightweight, per-query read federation over member stores.
//
// Phase-1 Node.IDs may collide across repos; routing prefers URI pins and
// unique ownership so neighborhood walks stay within the seed's repo.
// Those pins live in idOwner and are mutable query state, so a Store must
// not be shared across goroutines. Keep underlying member stores long-lived
// (to avoid re-opening Badger) and call New or Session once per query.
//
// Close does not close members; the caller owns member lifetime.
type Store struct {
	members []Member

	mu      sync.RWMutex
	idOwner map[graph.NodeID]int // pinned or uniquely owned member index
}

// New builds a federated store for a single query. members must be non-empty
// and have unique names. The returned Store is not safe for concurrent use.
func New(members []Member) (*Store, error) {
	if len(members) == 0 {
		return nil, fmt.Errorf("federated: no members")
	}
	// Copy so callers can reuse their slice without racing our Name normalization.
	copied := make([]Member, len(members))
	copy(copied, members)
	seen := make(map[string]struct{}, len(copied))
	for i, m := range copied {
		if m.Store == nil {
			return nil, fmt.Errorf("federated: member %d has nil store", i)
		}
		name, err := uri.NormalizeRepo(m.Name)
		if err != nil {
			return nil, fmt.Errorf("federated: member %d: %w", i, err)
		}
		if _, ok := seen[name]; ok {
			return nil, fmt.Errorf("federated: duplicate member name %q", name)
		}
		seen[name] = struct{}{}
		copied[i].Name = name
	}
	return &Store{
		members: copied,
		idOwner: make(map[graph.NodeID]int),
	}, nil
}

// Session returns a new Store sharing the same members with an empty pin map.
// Use one Session (or New) per concurrent query against long-lived members.
func (s *Store) Session() *Store {
	return &Store{
		members: s.members,
		idOwner: make(map[graph.NodeID]int),
	}
}

// Close clears query pins. It does not close member stores.
func (s *Store) Close() error {
	s.mu.Lock()
	s.idOwner = make(map[graph.NodeID]int)
	s.mu.Unlock()
	return nil
}

func (s *Store) pin(id graph.NodeID, idx int) {
	s.mu.Lock()
	s.idOwner[id] = idx
	s.mu.Unlock()
}

func (s *Store) owner(id graph.NodeID) (int, bool) {
	s.mu.RLock()
	idx, ok := s.idOwner[id]
	s.mu.RUnlock()
	return idx, ok
}

func (s *Store) resolveOwner(id graph.NodeID) (int, error) {
	if idx, ok := s.owner(id); ok {
		return idx, nil
	}
	var found []int
	for i, m := range s.members {
		if _, err := m.Store.GetNode(id); err == nil {
			found = append(found, i)
		} else if err != graph.ErrNotFound {
			return -1, err
		}
	}
	switch len(found) {
	case 0:
		return -1, graph.ErrNotFound
	case 1:
		s.pin(id, found[0])
		return found[0], nil
	default:
		return -1, fmt.Errorf("%w: node id %q exists in %d repos; use a repo:// URI or --repo scope",
			graph.ErrConflict, id, len(found))
	}
}

func (s *Store) PutNode(graph.Node) error {
	return fmt.Errorf("federated: PutNode is not supported (open a single-repo store to index)")
}

func (s *Store) DeleteNode(graph.NodeID) error {
	return fmt.Errorf("federated: DeleteNode is not supported")
}

func (s *Store) PutEdge(graph.Edge) error {
	return fmt.Errorf("federated: PutEdge is not supported")
}

func (s *Store) DeleteEdge(graph.NodeID, graph.NodeID, graph.EdgeType) error {
	return fmt.Errorf("federated: DeleteEdge is not supported")
}

func (s *Store) GetNode(id graph.NodeID) (graph.Node, error) {
	idx, err := s.resolveOwner(id)
	if err != nil {
		return graph.Node{}, err
	}
	return s.members[idx].Store.GetNode(id)
}

func (s *Store) GetNodeByURI(repoURI string) (graph.Node, error) {
	u, err := uri.Parse(repoURI)
	if err != nil {
		return graph.Node{}, err
	}
	for i, m := range s.members {
		if m.Name != u.Repo {
			continue
		}
		n, err := m.Store.GetNodeByURI(repoURI)
		if err != nil {
			return graph.Node{}, err
		}
		s.pin(n.ID, i)
		return n, nil
	}
	// Fallback: scan all (covers misnamed members).
	for i, m := range s.members {
		n, err := m.Store.GetNodeByURI(repoURI)
		if err == nil {
			s.pin(n.ID, i)
			return n, nil
		}
		if err != graph.ErrNotFound {
			return graph.Node{}, err
		}
	}
	return graph.Node{}, graph.ErrNotFound
}

func (s *Store) OutEdges(from graph.NodeID, edgeType graph.EdgeType) ([]graph.Edge, error) {
	idx, err := s.resolveOwner(from)
	if err != nil {
		return nil, err
	}
	edges, err := s.members[idx].Store.OutEdges(from, edgeType)
	if err != nil {
		return nil, err
	}
	for _, e := range edges {
		s.pin(e.From, idx)
		s.pin(e.To, idx)
	}
	return edges, nil
}

func (s *Store) InEdges(to graph.NodeID, edgeType graph.EdgeType) ([]graph.Edge, error) {
	idx, err := s.resolveOwner(to)
	if err != nil {
		return nil, err
	}
	edges, err := s.members[idx].Store.InEdges(to, edgeType)
	if err != nil {
		return nil, err
	}
	for _, e := range edges {
		s.pin(e.From, idx)
		s.pin(e.To, idx)
	}
	return edges, nil
}

func (s *Store) ForEachNode(fn func(graph.Node) bool) error {
	for _, m := range s.members {
		cont := true
		err := m.Store.ForEachNode(func(n graph.Node) bool {
			cont = fn(n)
			return cont
		})
		if err != nil {
			return err
		}
		if !cont {
			return nil
		}
	}
	return nil
}

// MemberNames returns logical repo names in member order.
func (s *Store) MemberNames() []string {
	out := make([]string, len(s.members))
	for i, m := range s.members {
		out[i] = m.Name
	}
	return out
}

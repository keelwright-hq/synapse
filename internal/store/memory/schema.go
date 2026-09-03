package memory

import (
	"fmt"

	"github.com/taricsa/synapse/internal/graph"
	"github.com/taricsa/synapse/internal/uri"
)

// Schema versions for the in-memory / Badger graph stores.
const (
	SchemaVersionPhase1   = 1
	SchemaVersionCurrent  = 2 // repo_uri secondary index (SYN-11)
)

// EnsureSchema migrates nodes to schema v2 (repo_uri props + index) using repo.
func (s *Store) EnsureSchema(repo string) error {
	repo, err := uri.NormalizeRepo(repo)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.schemaVer >= SchemaVersionCurrent {
		s.repoName = repo
		return nil
	}
	if s.uriIndex == nil {
		s.uriIndex = make(map[string]graph.NodeID)
	}
	for id, node := range s.nodes {
		if node.Props != nil && node.Props[uri.PropKey] != "" {
			u := node.Props[uri.PropKey]
			if existing, ok := s.uriIndex[u]; ok && existing != id {
				return fmt.Errorf("%w: uri %q for %s and %s", graph.ErrConflict, u, existing, id)
			}
			s.uriIndex[u] = id
			continue
		}
		canonical, ok, err := uri.FromLegacy(repo, string(id))
		if err != nil {
			return fmt.Errorf("migrate %s: %w", id, err)
		}
		if !ok {
			continue
		}
		if existing, ok := s.uriIndex[canonical]; ok && existing != id {
			return fmt.Errorf("%w: uri %q for %s and %s", graph.ErrConflict, canonical, existing, id)
		}
		if node.Props == nil {
			node.Props = map[string]string{}
		} else {
			node.Props = cloneProps(node.Props)
		}
		node.Props[uri.PropKey] = canonical
		s.nodes[id] = node
		s.uriIndex[canonical] = id
	}
	s.schemaVer = SchemaVersionCurrent
	s.repoName = repo
	return nil
}

// SchemaVersion returns the store schema version.
func (s *Store) SchemaVersion() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.schemaVer
}

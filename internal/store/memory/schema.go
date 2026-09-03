package memory

import (
	"fmt"

	"github.com/keelwright-hq/synapse/internal/graph"
	"github.com/keelwright-hq/synapse/internal/uri"
)

// Schema versions for the in-memory / Badger graph stores.
const (
	SchemaVersionPhase1  = 1
	SchemaVersionCurrent = 2 // repo_uri secondary index (SYN-11)
)

// EnsureSchema migrates nodes to schema v2 (repo_uri props + index) using repo.
// Migration is applied atomically: on conflict/error the store state is unchanged.
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

	tempURIIndex := make(map[string]graph.NodeID)
	if s.uriIndex != nil {
		for k, v := range s.uriIndex {
			tempURIIndex[k] = v
		}
	}
	tempNodes := make(map[graph.NodeID]graph.Node, len(s.nodes))
	for k, v := range s.nodes {
		tempNodes[k] = cloneNode(v)
	}

	for id, node := range tempNodes {
		if node.Props != nil && node.Props[uri.PropKey] != "" {
			u := node.Props[uri.PropKey]
			if existing, ok := tempURIIndex[u]; ok && existing != id {
				return fmt.Errorf("%w: uri %q for %s and %s", graph.ErrConflict, u, existing, id)
			}
			tempURIIndex[u] = id
			continue
		}
		canonical, ok, err := uri.FromLegacy(repo, string(id))
		if err != nil {
			return fmt.Errorf("migrate %s: %w", id, err)
		}
		if !ok {
			continue
		}
		if existing, ok := tempURIIndex[canonical]; ok && existing != id {
			return fmt.Errorf("%w: uri %q for %s and %s", graph.ErrConflict, canonical, existing, id)
		}
		if node.Props == nil {
			node.Props = map[string]string{}
		}
		node.Props[uri.PropKey] = canonical
		tempNodes[id] = node
		tempURIIndex[canonical] = id
	}

	s.nodes = tempNodes
	s.uriIndex = tempURIIndex
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

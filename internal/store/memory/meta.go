package memory

import (
	"fmt"

	"github.com/keelwright-hq/synapse/internal/graph"
)

// GetFingerprint returns the stored content hash for path.
func (s *Store) GetFingerprint(path string) (string, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.fingerprints == nil {
		return "", false, nil
	}
	hash, ok := s.fingerprints[path]
	return hash, ok, nil
}

// PutFingerprint stores a content hash for path.
func (s *Store) PutFingerprint(path, hash string) error {
	if path == "" {
		return fmt.Errorf("memory: fingerprint path is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fingerprints == nil {
		s.fingerprints = make(map[string]string)
	}
	s.fingerprints[path] = hash
	return nil
}

// DeleteFingerprint removes the fingerprint for path.
func (s *Store) DeleteFingerprint(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.fingerprints, path)
	return nil
}

// ListFingerprints returns all path→hash entries.
func (s *Store) ListFingerprints() (map[string]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]string, len(s.fingerprints))
	for k, v := range s.fingerprints {
		out[k] = v
	}
	return out, nil
}

// GetOwnedNodes returns node IDs owned by path.
func (s *Store) GetOwnedNodes(path string) ([]graph.NodeID, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := s.owned[path]
	if len(ids) == 0 {
		return nil, nil
	}
	out := make([]graph.NodeID, len(ids))
	copy(out, ids)
	return out, nil
}

// PutOwnedNodes stores the set of node IDs owned by path.
func (s *Store) PutOwnedNodes(path string, ids []graph.NodeID) error {
	if path == "" {
		return fmt.Errorf("memory: ownership path is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.owned == nil {
		s.owned = make(map[string][]graph.NodeID)
	}
	cp := make([]graph.NodeID, len(ids))
	copy(cp, ids)
	s.owned[path] = cp
	return nil
}

// DeleteOwnedNodes removes ownership metadata for path.
func (s *Store) DeleteOwnedNodes(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.owned, path)
	return nil
}

package badger

import (
	"encoding/json"
	"fmt"

	badgerdb "github.com/dgraph-io/badger/v4"
	"github.com/keelwright-hq/synapse/internal/graph"
)

// Fingerprint and ownership keys live alongside graph keys:
//   fp\x00{path} → content hash (utf-8)
//   fo\x00{path} → JSON []NodeID

func fingerprintKey(path string) []byte {
	return []byte("fp\x00" + path)
}

func ownershipKey(path string) []byte {
	return []byte("fo\x00" + path)
}

// GetFingerprint returns the stored content hash for path.
func (s *Store) GetFingerprint(path string) (string, bool, error) {
	var hash string
	err := s.db.View(func(txn *badgerdb.Txn) error {
		item, err := txn.Get(fingerprintKey(path))
		if err != nil {
			if err == badgerdb.ErrKeyNotFound {
				return nil
			}
			return err
		}
		return item.Value(func(val []byte) error {
			hash = string(val)
			return nil
		})
	})
	if err != nil {
		return "", false, err
	}
	if hash == "" {
		return "", false, nil
	}
	return hash, true, nil
}

// PutFingerprint stores a content hash for path.
func (s *Store) PutFingerprint(path, hash string) error {
	if path == "" {
		return fmt.Errorf("badger: fingerprint path is required")
	}
	return s.db.Update(func(txn *badgerdb.Txn) error {
		return txn.Set(fingerprintKey(path), []byte(hash))
	})
}

// DeleteFingerprint removes the fingerprint for path.
func (s *Store) DeleteFingerprint(path string) error {
	return s.db.Update(func(txn *badgerdb.Txn) error {
		return txn.Delete(fingerprintKey(path))
	})
}

// ListFingerprints returns all path→hash entries.
func (s *Store) ListFingerprints() (map[string]string, error) {
	out := make(map[string]string)
	prefix := []byte("fp\x00")
	err := s.db.View(func(txn *badgerdb.Txn) error {
		it := txn.NewIterator(badgerdb.DefaultIteratorOptions)
		defer it.Close()
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()
			key := item.Key()
			path := string(key[len(prefix):])
			if err := item.Value(func(val []byte) error {
				out[path] = string(val)
				return nil
			}); err != nil {
				return err
			}
		}
		return nil
	})
	return out, err
}

// GetOwnedNodes returns node IDs owned by path (empty if none).
func (s *Store) GetOwnedNodes(path string) ([]graph.NodeID, error) {
	var ids []graph.NodeID
	err := s.db.View(func(txn *badgerdb.Txn) error {
		item, err := txn.Get(ownershipKey(path))
		if err != nil {
			if err == badgerdb.ErrKeyNotFound {
				return nil
			}
			return err
		}
		return item.Value(func(val []byte) error {
			return json.Unmarshal(val, &ids)
		})
	})
	return ids, err
}

// PutOwnedNodes stores the set of node IDs owned by path.
func (s *Store) PutOwnedNodes(path string, ids []graph.NodeID) error {
	if path == "" {
		return fmt.Errorf("badger: ownership path is required")
	}
	if ids == nil {
		ids = []graph.NodeID{}
	}
	data, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	return s.db.Update(func(txn *badgerdb.Txn) error {
		return txn.Set(ownershipKey(path), data)
	})
}

// DeleteOwnedNodes removes ownership metadata for path.
func (s *Store) DeleteOwnedNodes(path string) error {
	return s.db.Update(func(txn *badgerdb.Txn) error {
		return txn.Delete(ownershipKey(path))
	})
}

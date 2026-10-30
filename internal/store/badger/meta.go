package badger

import (
	"encoding/json"
	"fmt"

	badgerdb "github.com/dgraph-io/badger/v4"
	"github.com/keelwright-hq/synapse/internal/graph"
	"github.com/keelwright-hq/synapse/internal/uri"
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

// ReplaceFileGraph replaces a file's owned subgraph, ownership list, and
// fingerprint in a single Badger transaction when it fits, or across multiple
// committed batches when Badger returns ErrTxnTooBig. The fingerprint is
// written last so an interrupted multi-batch replacement will be re-indexed on
// the next run.
func (s *Store) ReplaceFileGraph(relPath, hash string, nodes []graph.Node, edges []graph.Edge, owned []graph.NodeID) error {
	if relPath == "" {
		return fmt.Errorf("badger: path is required")
	}
	old, err := s.GetOwnedNodes(relPath)
	if err != nil {
		return err
	}
	if owned == nil {
		owned = []graph.NodeID{}
	}
	ownedBytes, err := json.Marshal(owned)
	if err != nil {
		return err
	}

	txn := s.db.NewTransaction(true)
	defer func() {
		if txn != nil {
			txn.Discard()
		}
	}()

	setEntry := func(k, v []byte) error {
		if err := txn.Set(k, v); err == badgerdb.ErrTxnTooBig {
			if err := txn.Commit(); err != nil {
				return err
			}
			txn = s.db.NewTransaction(true)
			return txn.Set(k, v)
		} else {
			return err
		}
	}
	delKey := func(k []byte) error {
		if err := txn.Delete(k); err == badgerdb.ErrTxnTooBig {
			if err := txn.Commit(); err != nil {
				return err
			}
			txn = s.db.NewTransaction(true)
			return txn.Delete(k)
		} else {
			return err
		}
	}

	for _, id := range old {
		if err := deleteNodeInTxn(txn, delKey, id); err != nil && err != graph.ErrNotFound {
			return err
		}
	}
	for _, node := range nodes {
		if node.ID == "" {
			return fmt.Errorf("badger: node id is required")
		}
		data, err := marshalNode(node)
		if err != nil {
			return err
		}
		if err := clearURIIndexForNodeWith(txn, delKey, node.ID); err != nil {
			return err
		}
		if node.Props != nil {
			if newURI := node.Props[uri.PropKey]; newURI != "" {
				if err := putURIIndexWith(txn, setEntry, newURI, node.ID); err != nil {
					return err
				}
			}
		}
		if err := setEntry(nodeKey(node.ID), data); err != nil {
			return err
		}
	}
	for _, edge := range edges {
		if edge.From == "" || edge.To == "" || edge.Type == "" {
			return fmt.Errorf("badger: edge from, to, and type are required")
		}
		data, err := marshalEdge(edge)
		if err != nil {
			return err
		}
		if _, err := txn.Get(nodeKey(edge.From)); err != nil {
			if err == badgerdb.ErrKeyNotFound {
				return graph.ErrNotFound
			}
			return err
		}
		if _, err := txn.Get(nodeKey(edge.To)); err != nil {
			if err == badgerdb.ErrKeyNotFound {
				return graph.ErrNotFound
			}
			return err
		}
		if err := setEntry(outEdgeKey(edge.From, edge.Type, edge.To), data); err != nil {
			return err
		}
		if err := setEntry(inEdgeKey(edge.To, edge.Type, edge.From), data); err != nil {
			return err
		}
	}
	if err := setEntry(ownershipKey(relPath), ownedBytes); err != nil {
		return err
	}
	if err := setEntry(fingerprintKey(relPath), []byte(hash)); err != nil {
		return err
	}
	err = txn.Commit()
	txn = nil
	return err
}

// DeleteFileGraph deletes a file's owned nodes, ownership entry, and fingerprint
// in a batched Badger transaction.
func (s *Store) DeleteFileGraph(relPath string) error {
	old, err := s.GetOwnedNodes(relPath)
	if err != nil {
		return err
	}
	txn := s.db.NewTransaction(true)
	defer func() {
		if txn != nil {
			txn.Discard()
		}
	}()
	delKey := func(k []byte) error {
		if err := txn.Delete(k); err == badgerdb.ErrTxnTooBig {
			if err := txn.Commit(); err != nil {
				return err
			}
			txn = s.db.NewTransaction(true)
			return txn.Delete(k)
		} else {
			return err
		}
	}
	for _, id := range old {
		if err := deleteNodeInTxn(txn, delKey, id); err != nil && err != graph.ErrNotFound {
			return err
		}
	}
	if err := delKey(ownershipKey(relPath)); err != nil {
		return err
	}
	if err := delKey(fingerprintKey(relPath)); err != nil {
		return err
	}
	err = txn.Commit()
	txn = nil
	return err
}

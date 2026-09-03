package badger

import (
	"fmt"
	"strconv"

	badgerdb "github.com/dgraph-io/badger/v4"
	"github.com/keelwright-hq/synapse/internal/graph"
	"github.com/keelwright-hq/synapse/internal/uri"
)

// Schema versions persisted under meta\x00schema_version.
const (
	SchemaVersionPhase1  = 1
	SchemaVersionCurrent = 2 // repo_uri secondary index (SYN-11)
)

func schemaVersionKey() []byte {
	return []byte("meta\x00schema_version")
}

func repoNameKey() []byte {
	return []byte("meta\x00repo")
}

// OpenWithRepo opens the store and migrates to the current schema using repo.
func OpenWithRepo(dir, repo string) (*Store, error) {
	s, err := Open(dir)
	if err != nil {
		return nil, err
	}
	if err := s.EnsureSchema(repo); err != nil {
		_ = s.Close()
		return nil, err
	}
	return s, nil
}

// EnsureSchema upgrades Phase-1 indexes to schema v2 (repo_uri props + unique index).
func (s *Store) EnsureSchema(repo string) error {
	repo, err := uri.NormalizeRepo(repo)
	if err != nil {
		return err
	}
	ver, err := s.schemaVersion()
	if err != nil {
		return err
	}
	if ver >= SchemaVersionCurrent {
		return s.setRepoName(repo)
	}
	if err := s.migrateToV2(repo); err != nil {
		return err
	}
	if err := s.setSchemaVersion(SchemaVersionCurrent); err != nil {
		return err
	}
	return s.setRepoName(repo)
}

func (s *Store) schemaVersion() (int, error) {
	var ver int
	err := s.db.View(func(txn *badgerdb.Txn) error {
		item, err := txn.Get(schemaVersionKey())
		if err != nil {
			if err == badgerdb.ErrKeyNotFound {
				ver = SchemaVersionPhase1
				return nil
			}
			return err
		}
		return item.Value(func(val []byte) error {
			v, err := strconv.Atoi(string(val))
			if err != nil {
				return err
			}
			ver = v
			return nil
		})
	})
	return ver, err
}

func (s *Store) setSchemaVersion(ver int) error {
	return s.db.Update(func(txn *badgerdb.Txn) error {
		return txn.Set(schemaVersionKey(), []byte(strconv.Itoa(ver)))
	})
}

func (s *Store) setRepoName(repo string) error {
	return s.db.Update(func(txn *badgerdb.Txn) error {
		return txn.Set(repoNameKey(), []byte(repo))
	})
}

func (s *Store) migrateToV2(repo string) error {
	type pair struct {
		id   graph.NodeID
		node graph.Node
	}
	var nodes []pair
	err := s.db.View(func(txn *badgerdb.Txn) error {
		it := txn.NewIterator(badgerdb.DefaultIteratorOptions)
		defer it.Close()
		prefix := []byte("n\x00")
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()
			var node graph.Node
			if err := item.Value(func(val []byte) error {
				var e error
				node, e = unmarshalNode(val)
				return e
			}); err != nil {
				return err
			}
			nodes = append(nodes, pair{id: node.ID, node: node})
		}
		return nil
	})
	if err != nil {
		return err
	}

	return s.db.Update(func(txn *badgerdb.Txn) error {
		seen := make(map[string]graph.NodeID)
		for _, p := range nodes {
			node := p.node
			if node.Props != nil && node.Props[uri.PropKey] != "" {
				u := node.Props[uri.PropKey]
				if existing, ok := seen[u]; ok && existing != node.ID {
					return fmt.Errorf("%w: uri %q for %s and %s", graph.ErrConflict, u, existing, node.ID)
				}
				seen[u] = node.ID
				if err := txn.Set(uriIndexKey(u), []byte(node.ID)); err != nil {
					return err
				}
				continue
			}
			canonical, ok, err := uri.FromLegacy(repo, string(node.ID))
			if err != nil {
				return fmt.Errorf("migrate %s: %w", node.ID, err)
			}
			if !ok {
				continue
			}
			if existing, ok := seen[canonical]; ok && existing != node.ID {
				return fmt.Errorf("%w: uri %q for %s and %s", graph.ErrConflict, canonical, existing, node.ID)
			}
			seen[canonical] = node.ID
			if node.Props == nil {
				node.Props = map[string]string{}
			}
			node.Props[uri.PropKey] = canonical
			data, err := marshalNode(node)
			if err != nil {
				return err
			}
			if err := txn.Set(nodeKey(node.ID), data); err != nil {
				return err
			}
			if err := txn.Set(uriIndexKey(canonical), []byte(node.ID)); err != nil {
				return err
			}
		}
		return nil
	})
}

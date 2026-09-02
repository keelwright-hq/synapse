package badger

import (
	"fmt"
	"os"
	"path/filepath"

	badgerdb "github.com/dgraph-io/badger/v4"
	"github.com/taricsa/synapse/internal/graph"
)

const defaultDataDir = ".synapse"

// Store persists a graph in BadgerDB.
type Store struct {
	dir string
	db  *badgerdb.DB
}

// Open creates or opens a Badger store under dir (default .synapse/ → .synapse/graph).
func Open(dir string) (*Store, error) {
	path, err := resolveGraphPath(dir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return nil, fmt.Errorf("badger: mkdir %s: %w", path, err)
	}

	opts := badgerdb.DefaultOptions(path)
	opts.Logger = nil
	db, err := badgerdb.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("badger: open: %w", err)
	}
	return &Store{dir: path, db: db}, nil
}

func resolveGraphPath(dir string) (string, error) {
	if dir == "" {
		dir = defaultDataDir
	}
	cleaned := filepath.Clean(dir)
	var path string
	if filepath.Base(cleaned) == "graph" {
		path = cleaned
	} else {
		path = filepath.Join(cleaned, "graph")
	}
	return filepath.Abs(path)
}

// Close flushes and releases the database.
func (s *Store) Close() error {
	if s.db == nil {
		return nil
	}
	err := s.db.Close()
	s.db = nil
	return err
}

func (s *Store) PutNode(node graph.Node) error {
	if node.ID == "" {
		return fmt.Errorf("badger: node id is required")
	}
	data, err := marshalNode(node)
	if err != nil {
		return err
	}
	return s.db.Update(func(txn *badgerdb.Txn) error {
		return txn.Set(nodeKey(node.ID), data)
	})
}

func (s *Store) GetNode(id graph.NodeID) (graph.Node, error) {
	var node graph.Node
	err := s.db.View(func(txn *badgerdb.Txn) error {
		item, err := txn.Get(nodeKey(id))
		if err != nil {
			if err == badgerdb.ErrKeyNotFound {
				return graph.ErrNotFound
			}
			return err
		}
		return item.Value(func(val []byte) error {
			node, err = unmarshalNode(val)
			return err
		})
	})
	if err != nil {
		return graph.Node{}, err
	}
	return node, nil
}

func (s *Store) DeleteNode(id graph.NodeID) error {
	return s.db.Update(func(txn *badgerdb.Txn) error {
		if _, err := txn.Get(nodeKey(id)); err != nil {
			if err == badgerdb.ErrKeyNotFound {
				return graph.ErrNotFound
			}
			return err
		}

		if err := deleteEdgesForNode(txn, id); err != nil {
			return err
		}
		return txn.Delete(nodeKey(id))
	})
}

func (s *Store) PutEdge(edge graph.Edge) error {
	if edge.From == "" || edge.To == "" || edge.Type == "" {
		return fmt.Errorf("badger: edge from, to, and type are required")
	}
	data, err := marshalEdge(edge)
	if err != nil {
		return err
	}
	return s.db.Update(func(txn *badgerdb.Txn) error {
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
		outKey := outEdgeKey(edge.From, edge.Type, edge.To)
		inKey := inEdgeKey(edge.To, edge.Type, edge.From)
		if err := txn.Set(outKey, data); err != nil {
			return err
		}
		return txn.Set(inKey, data)
	})
}

func (s *Store) DeleteEdge(from graph.NodeID, to graph.NodeID, edgeType graph.EdgeType) error {
	return s.db.Update(func(txn *badgerdb.Txn) error {
		outKey := outEdgeKey(from, edgeType, to)
		if _, err := txn.Get(outKey); err != nil {
			if err == badgerdb.ErrKeyNotFound {
				return graph.ErrNotFound
			}
			return err
		}
		inKey := inEdgeKey(to, edgeType, from)
		if err := txn.Delete(outKey); err != nil {
			return err
		}
		return txn.Delete(inKey)
	})
}

func (s *Store) OutEdges(from graph.NodeID, edgeType graph.EdgeType) ([]graph.Edge, error) {
	var edges []graph.Edge
	prefix := outEdgePrefix(from)
	if edgeType != "" {
		prefix = outEdgeTypePrefix(from, edgeType)
	}
	err := s.db.View(func(txn *badgerdb.Txn) error {
		it := txn.NewIterator(badgerdb.DefaultIteratorOptions)
		defer it.Close()
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()
			var edge graph.Edge
			if err := item.Value(func(val []byte) error {
				parsed, err := unmarshalEdge(val)
				if err != nil {
					return err
				}
				edge = parsed
				return nil
			}); err != nil {
				return err
			}
			edges = append(edges, edge)
		}
		return nil
	})
	return edges, err
}

func (s *Store) InEdges(to graph.NodeID, edgeType graph.EdgeType) ([]graph.Edge, error) {
	var edges []graph.Edge
	prefix := inEdgePrefix(to)
	if edgeType != "" {
		prefix = inEdgeTypePrefix(to, edgeType)
	}
	err := s.db.View(func(txn *badgerdb.Txn) error {
		it := txn.NewIterator(badgerdb.DefaultIteratorOptions)
		defer it.Close()
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()
			var edge graph.Edge
			if err := item.Value(func(val []byte) error {
				parsed, err := unmarshalEdge(val)
				if err != nil {
					return err
				}
				edge = parsed
				return nil
			}); err != nil {
				return err
			}
			edges = append(edges, edge)
		}
		return nil
	})
	return edges, err
}

func deleteEdgesForNode(txn *badgerdb.Txn, id graph.NodeID) error {
	outPrefix := outEdgePrefix(id)
	it := txn.NewIterator(badgerdb.DefaultIteratorOptions)
	defer it.Close()
	for it.Seek(outPrefix); it.ValidForPrefix(outPrefix); it.Next() {
		item := it.Item()
		var edge graph.Edge
		if err := item.Value(func(val []byte) error {
			parsed, err := unmarshalEdge(val)
			if err != nil {
				return err
			}
			edge = parsed
			return nil
		}); err != nil {
			return err
		}
		if err := txn.Delete(item.KeyCopy(nil)); err != nil {
			return err
		}
		if err := txn.Delete(inEdgeKey(edge.To, edge.Type, edge.From)); err != nil {
			return err
		}
	}

	inPrefix := inEdgePrefix(id)
	it2 := txn.NewIterator(badgerdb.DefaultIteratorOptions)
	defer it2.Close()
	for it2.Seek(inPrefix); it2.ValidForPrefix(inPrefix); it2.Next() {
		item := it2.Item()
		var edge graph.Edge
		if err := item.Value(func(val []byte) error {
			parsed, err := unmarshalEdge(val)
			if err != nil {
				return err
			}
			edge = parsed
			return nil
		}); err != nil {
			return err
		}
		if err := txn.Delete(item.KeyCopy(nil)); err != nil {
			return err
		}
		if err := txn.Delete(outEdgeKey(edge.From, edge.Type, edge.To)); err != nil {
			return err
		}
	}
	return nil
}

// Key segments use \x00 so NodeIDs containing '/' (file/package paths) cannot
// collide during prefix scans.
func nodeKey(id graph.NodeID) []byte {
	return []byte("n\x00" + string(id))
}

func outEdgePrefix(from graph.NodeID) []byte {
	return []byte("eo\x00" + string(from) + "\x00")
}

func inEdgePrefix(to graph.NodeID) []byte {
	return []byte("ei\x00" + string(to) + "\x00")
}

func outEdgeTypePrefix(from graph.NodeID, edgeType graph.EdgeType) []byte {
	return []byte("eo\x00" + string(from) + "\x00" + string(edgeType) + "\x00")
}

func inEdgeTypePrefix(to graph.NodeID, edgeType graph.EdgeType) []byte {
	return []byte("ei\x00" + string(to) + "\x00" + string(edgeType) + "\x00")
}

func outEdgeKey(from graph.NodeID, edgeType graph.EdgeType, to graph.NodeID) []byte {
	return []byte("eo\x00" + string(from) + "\x00" + string(edgeType) + "\x00" + string(to))
}

func inEdgeKey(to graph.NodeID, edgeType graph.EdgeType, from graph.NodeID) []byte {
	return []byte("ei\x00" + string(to) + "\x00" + string(edgeType) + "\x00" + string(from))
}

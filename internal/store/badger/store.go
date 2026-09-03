package badger

import (
	"fmt"
	"os"
	"path/filepath"

	badgerdb "github.com/dgraph-io/badger/v4"
	"github.com/keelwright-hq/synapse/internal/graph"
	"github.com/keelwright-hq/synapse/internal/uri"
)

// defaultDataDir is the relative path used when Open receives an empty dir.
// It is a var so tests can override it with t.TempDir() without writing to cwd.
var defaultDataDir = ".synapse"

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
	newURI := ""
	if node.Props != nil {
		newURI = node.Props[uri.PropKey]
	}
	return s.db.Update(func(txn *badgerdb.Txn) error {
		if err := clearURIIndexForNode(txn, node.ID); err != nil {
			return err
		}
		if newURI != "" {
			if err := putURIIndex(txn, newURI, node.ID); err != nil {
				return err
			}
		}
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

func (s *Store) GetNodeByURI(repoURI string) (graph.Node, error) {
	canonical, err := uri.Normalize(repoURI)
	if err != nil {
		return graph.Node{}, err
	}
	var node graph.Node
	err = s.db.View(func(txn *badgerdb.Txn) error {
		item, err := txn.Get(uriIndexKey(canonical))
		if err != nil {
			if err == badgerdb.ErrKeyNotFound {
				return graph.ErrNotFound
			}
			return err
		}
		var id graph.NodeID
		if err := item.Value(func(val []byte) error {
			id = graph.NodeID(val)
			return nil
		}); err != nil {
			return err
		}
		nItem, err := txn.Get(nodeKey(id))
		if err != nil {
			if err == badgerdb.ErrKeyNotFound {
				return graph.ErrNotFound
			}
			return err
		}
		return nItem.Value(func(val []byte) error {
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

		if err := clearURIIndexForNode(txn, id); err != nil {
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

// ForEachNode invokes fn for every node. Iteration stops early if fn returns false.
func (s *Store) ForEachNode(fn func(graph.Node) bool) error {
	prefix := []byte("n\x00")
	return s.db.View(func(txn *badgerdb.Txn) error {
		opts := badgerdb.DefaultIteratorOptions
		it := txn.NewIterator(opts)
		defer it.Close()
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()
			var node graph.Node
			if err := item.Value(func(val []byte) error {
				parsed, err := unmarshalNode(val)
				if err != nil {
					return err
				}
				node = parsed
				return nil
			}); err != nil {
				return err
			}
			if !fn(node) {
				return nil
			}
		}
		return nil
	})
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

func uriIndexKey(repoURI string) []byte {
	return []byte("ru\x00" + repoURI)
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

func clearURIIndexForNode(txn *badgerdb.Txn, id graph.NodeID) error {
	item, err := txn.Get(nodeKey(id))
	if err != nil {
		if err == badgerdb.ErrKeyNotFound {
			return nil
		}
		return err
	}
	var node graph.Node
	if err := item.Value(func(val []byte) error {
		var e error
		node, e = unmarshalNode(val)
		return e
	}); err != nil {
		return err
	}
	if node.Props == nil {
		return nil
	}
	u := node.Props[uri.PropKey]
	if u == "" {
		return nil
	}
	return txn.Delete(uriIndexKey(u))
}

func putURIIndex(txn *badgerdb.Txn, repoURI string, id graph.NodeID) error {
	item, err := txn.Get(uriIndexKey(repoURI))
	if err != nil && err != badgerdb.ErrKeyNotFound {
		return err
	}
	if err == nil {
		var existing graph.NodeID
		if err := item.Value(func(val []byte) error {
			existing = graph.NodeID(val)
			return nil
		}); err != nil {
			return err
		}
		if existing != id {
			return fmt.Errorf("%w: uri %q already bound to %s", graph.ErrConflict, repoURI, existing)
		}
	}
	return txn.Set(uriIndexKey(repoURI), []byte(id))
}

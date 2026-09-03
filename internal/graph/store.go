package graph

import "errors"

// ErrNotFound indicates the requested node or edge does not exist.
var ErrNotFound = errors.New("graph: not found")

// Store persists graph nodes and typed edges.
type Store interface {
	Close() error

	PutNode(node Node) error
	GetNode(id NodeID) (Node, error)
	DeleteNode(id NodeID) error

	PutEdge(edge Edge) error
	DeleteEdge(from NodeID, to NodeID, edgeType EdgeType) error

	// OutEdges returns edges leaving from. If edgeType is non-empty, results are filtered.
	OutEdges(from NodeID, edgeType EdgeType) ([]Edge, error)

	// InEdges returns edges entering to. If edgeType is non-empty, results are filtered.
	InEdges(to NodeID, edgeType EdgeType) ([]Edge, error)

	// ForEachNode invokes fn for every node. Iteration stops early if fn returns false.
	ForEachNode(fn func(Node) bool) error
}

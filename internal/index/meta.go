package index

import (
	"github.com/keelwright-hq/synapse/internal/graph"
)

// MetaStore persists file fingerprints and per-file node ownership alongside the graph.
type MetaStore interface {
	GetFingerprint(path string) (hash string, ok bool, err error)
	PutFingerprint(path, hash string) error
	DeleteFingerprint(path string) error
	ListFingerprints() (map[string]string, error)

	GetOwnedNodes(path string) ([]graph.NodeID, error)
	PutOwnedNodes(path string, ids []graph.NodeID) error
	DeleteOwnedNodes(path string) error
}

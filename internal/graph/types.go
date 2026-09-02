package graph

// NodeID uniquely identifies a node in the graph.
type NodeID string

// EdgeType categorizes relationships between nodes.
type EdgeType string

// Node is a vertex in the code graph.
type Node struct {
	ID    NodeID            `json:"id"`
	Kind  string            `json:"kind"`
	Name  string            `json:"name,omitempty"`
	Path  string            `json:"path,omitempty"`
	Props map[string]string `json:"props,omitempty"`
}

// Edge connects two nodes with a typed relationship.
type Edge struct {
	From  NodeID            `json:"from"`
	To    NodeID            `json:"to"`
	Type  EdgeType          `json:"type"`
	Props map[string]string `json:"props,omitempty"`
}

package badger

import (
	"encoding/json"
	"fmt"

	"github.com/keelwright-hq/synapse/internal/graph"
)

func marshalNode(node graph.Node) ([]byte, error) {
	data, err := json.Marshal(node)
	if err != nil {
		return nil, fmt.Errorf("badger: marshal node: %w", err)
	}
	return data, nil
}

func unmarshalNode(data []byte) (graph.Node, error) {
	var node graph.Node
	if err := json.Unmarshal(data, &node); err != nil {
		return graph.Node{}, fmt.Errorf("badger: unmarshal node: %w", err)
	}
	return node, nil
}

func marshalEdge(edge graph.Edge) ([]byte, error) {
	data, err := json.Marshal(edge)
	if err != nil {
		return nil, fmt.Errorf("badger: marshal edge: %w", err)
	}
	return data, nil
}

func unmarshalEdge(data []byte) (graph.Edge, error) {
	var edge graph.Edge
	if err := json.Unmarshal(data, &edge); err != nil {
		return graph.Edge{}, fmt.Errorf("badger: unmarshal edge: %w", err)
	}
	return edge, nil
}

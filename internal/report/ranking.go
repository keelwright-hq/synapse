package report

import (
	"sort"

	"github.com/keelwright-hq/synapse/internal/graph"
	"github.com/keelwright-hq/synapse/internal/parse"
)

// Hub is a ranked graph node for human navigation sections.
type Hub struct {
	ID     graph.NodeID
	Kind   string
	Name   string
	Path   string
	Degree int
}

// ImportantFiles ranks file nodes by undirected degree over imports and calls
// edges only (contains is ignored so large files do not win by child count).
func ImportantFiles(nodes []graph.Node, edges []graph.Edge, limit int) []Hub {
	return rankHubs(nodes, edges, limit, func(e graph.Edge) bool {
		return e.Type == parse.EdgeImports || e.Type == parse.EdgeCalls
	}, func(n graph.Node) bool {
		return n.Kind == parse.KindFile
	})
}

// ImportantSymbols ranks resolved symbols (functions, methods, types, contracts)
// by undirected degree over calls / implements / consumes. Unresolved
// kind=symbol nodes and container kinds are excluded.
func ImportantSymbols(nodes []graph.Node, edges []graph.Edge, limit int) []Hub {
	return rankHubs(nodes, edges, limit, func(e graph.Edge) bool {
		switch e.Type {
		case parse.EdgeCalls, parse.EdgeImplements, parse.EdgeConsumes:
			return true
		default:
			return false
		}
	}, isImportantSymbolKind)
}

func isImportantSymbolKind(n graph.Node) bool {
	switch n.Kind {
	case parse.KindFunction, parse.KindMethod, parse.KindType,
		parse.KindOperation, parse.KindService, parse.KindSchema, parse.KindField:
		return true
	default:
		return false
	}
}

// TopImports ranks nodes by inbound imports-edge count (most-imported targets).
func TopImports(nodes []graph.Node, edges []graph.Edge, limit int) []Hub {
	byID := map[graph.NodeID]graph.Node{}
	for _, n := range nodes {
		byID[n.ID] = n
	}
	deg := map[graph.NodeID]int{}
	for _, e := range edges {
		if e.Type != parse.EdgeImports {
			continue
		}
		deg[e.To]++
	}
	return hubsFromDegree(byID, deg, limit, nil)
}

func rankHubs(
	nodes []graph.Node,
	edges []graph.Edge,
	limit int,
	edgeOK func(graph.Edge) bool,
	nodeOK func(graph.Node) bool,
) []Hub {
	byID := map[graph.NodeID]graph.Node{}
	for _, n := range nodes {
		byID[n.ID] = n
	}
	deg := map[graph.NodeID]int{}
	for _, e := range edges {
		if !edgeOK(e) {
			continue
		}
		deg[e.From]++
		deg[e.To]++
	}
	return hubsFromDegree(byID, deg, limit, nodeOK)
}

func hubsFromDegree(
	byID map[graph.NodeID]graph.Node,
	deg map[graph.NodeID]int,
	limit int,
	nodeOK func(graph.Node) bool,
) []Hub {
	var hubs []Hub
	for id, d := range deg {
		if d == 0 {
			continue
		}
		n, ok := byID[id]
		if !ok {
			continue
		}
		if nodeOK != nil && !nodeOK(n) {
			continue
		}
		hubs = append(hubs, Hub{
			ID:     id,
			Kind:   n.Kind,
			Name:   n.Name,
			Path:   n.Path,
			Degree: d,
		})
	}
	sort.Slice(hubs, func(i, j int) bool {
		if hubs[i].Degree != hubs[j].Degree {
			return hubs[i].Degree > hubs[j].Degree
		}
		return hubs[i].ID < hubs[j].ID
	})
	if limit > 0 && len(hubs) > limit {
		hubs = hubs[:limit]
	}
	return hubs
}

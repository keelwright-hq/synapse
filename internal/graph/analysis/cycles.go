package analysis

import (
	"sort"

	"github.com/keelwright-hq/synapse/internal/graph"
	"github.com/keelwright-hq/synapse/internal/parse"
)

// Cycle represents a circular dependency loop.
type Cycle struct {
	Type   string         `json:"type"`   // e.g. "import_cycle", "call_cycle"
	Length int            `json:"length"`
	Path   []graph.NodeID `json:"path"`
}

// DetectCycles finds circular dependency cycles using Tarjan's Strongly Connected Components (SCC).
func DetectCycles(nodes []graph.Node, edges []graph.Edge, limit int) []Cycle {
	if len(nodes) == 0 {
		return nil
	}

	byID := map[graph.NodeID]graph.Node{}
	for _, n := range nodes {
		byID[n.ID] = n
	}

	adj := map[graph.NodeID][]graph.NodeID{}
	for _, e := range edges {
		if e.Type == parse.EdgeImports || e.Type == parse.EdgeCalls {
			adj[e.From] = append(adj[e.From], e.To)
		}
	}

	// Tarjan SCC State
	index := 0
	indices := map[graph.NodeID]int{}
	lowlink := map[graph.NodeID]int{}
	onStack := map[graph.NodeID]bool{}
	stack := []graph.NodeID{}
	var sccs [][]graph.NodeID

	var strongConnect func(v graph.NodeID)
	strongConnect = func(v graph.NodeID) {
		indices[v] = index
		lowlink[v] = index
		index++
		stack = append(stack, v)
		onStack[v] = true

		for _, w := range adj[v] {
			if _, visited := indices[w]; !visited {
				strongConnect(w)
				if lowlink[w] < lowlink[v] {
					lowlink[v] = lowlink[w]
				}
			} else if onStack[w] {
				if indices[w] < lowlink[v] {
					lowlink[v] = indices[w]
				}
			}
		}

		if lowlink[v] == indices[v] {
			var scc []graph.NodeID
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				scc = append(scc, w)
				if w == v {
					break
				}
			}
			if len(scc) > 1 {
				sccs = append(sccs, scc)
			}
		}
	}

	for _, n := range nodes {
		if _, visited := indices[n.ID]; !visited {
			strongConnect(n.ID)
		}
	}

	var cycles []Cycle
	for _, scc := range sccs {
		// Reverse to present cycle in forward direction
		path := make([]graph.NodeID, len(scc))
		for i, id := range scc {
			path[len(scc)-1-i] = id
		}
		cycles = append(cycles, Cycle{
			Type:   "dependency_cycle",
			Length: len(path),
			Path:   path,
		})
	}

	sort.Slice(cycles, func(i, j int) bool {
		return cycles[i].Length > cycles[j].Length
	})

	if limit > 0 && len(cycles) > limit {
		cycles = cycles[:limit]
	}
	return cycles
}

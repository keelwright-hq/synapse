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

// DetectCycles finds circular dependency cycles using Tarjan's Strongly Connected
// Components (SCC) over the resolved dependency view.
func DetectCycles(nodes []graph.Node, edges []graph.Edge, limit int) []Cycle {
	cycles, _ := DetectCyclesReport(nodes, edges, limit)
	return cycles
}

// DetectCyclesReport is like DetectCycles but also returns resolution coverage.
func DetectCyclesReport(nodes []graph.Node, edges []graph.Edge, limit int) ([]Cycle, Coverage) {
	view := BuildDependencyView(nodes, edges)
	if len(view.Nodes) == 0 {
		return nil, view.Coverage
	}

	byID := map[graph.NodeID]graph.Node{}
	for _, n := range nodes {
		byID[n.ID] = n
	}
	for _, n := range view.Nodes {
		byID[n.ID] = n
	}

	adj := map[graph.NodeID][]graph.NodeID{}
	nodeSet := map[graph.NodeID]bool{}
	for _, e := range view.Edges {
		if e.Type == parse.EdgeImports || e.Type == parse.EdgeCalls {
			adj[e.From] = append(adj[e.From], e.To)
			nodeSet[e.From] = true
			nodeSet[e.To] = true
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

	var nodeList []graph.NodeID
	for id := range nodeSet {
		nodeList = append(nodeList, id)
	}
	sort.Slice(nodeList, func(i, j int) bool { return nodeList[i] < nodeList[j] })
	for _, id := range nodeList {
		if _, visited := indices[id]; !visited {
			strongConnect(id)
		}
	}

	var cycles []Cycle
	for _, scc := range sccs {
		path := preferFileModuleCycle(scc, byID, adj)
		// Reverse to present cycle in forward direction (Tarjan pops reverse)
		rev := make([]graph.NodeID, len(path))
		for i, id := range path {
			rev[len(path)-1-i] = id
		}
		cycles = append(cycles, Cycle{
			Type:   "dependency_cycle",
			Length: len(rev),
			Path:   rev,
		})
	}

	sort.Slice(cycles, func(i, j int) bool {
		iFM := cycleIsFileModule(cycles[i], byID)
		jFM := cycleIsFileModule(cycles[j], byID)
		if iFM != jFM {
			return iFM // file/module cycles first
		}
		if cycles[i].Length != cycles[j].Length {
			return cycles[i].Length > cycles[j].Length
		}
		if len(cycles[i].Path) > 0 && len(cycles[j].Path) > 0 {
			return cycles[i].Path[0] < cycles[j].Path[0]
		}
		return false
	})

	if limit > 0 && len(cycles) > limit {
		cycles = cycles[:limit]
	}
	return cycles, view.Coverage
}

func preferFileModuleCycle(scc []graph.NodeID, byID map[graph.NodeID]graph.Node, adj map[graph.NodeID][]graph.NodeID) []graph.NodeID {
	var fm []graph.NodeID
	fmSet := map[graph.NodeID]bool{}
	for _, id := range scc {
		n, ok := byID[id]
		if !ok {
			continue
		}
		if n.Kind == parse.KindFile || n.Kind == parse.KindModule {
			fm = append(fm, id)
			fmSet[id] = true
		}
	}
	if len(fm) < 2 {
		return scc
	}
	// Keep file/module members only when they remain mutually reachable via adj.
	fmAdj := map[graph.NodeID][]graph.NodeID{}
	for _, id := range fm {
		for _, w := range adj[id] {
			if fmSet[w] {
				fmAdj[id] = append(fmAdj[id], w)
			}
		}
	}
	// Verify the induced subgraph still has an SCC of size >= 2 among fm.
	if inducedSCCSize(fm, fmAdj) >= 2 {
		return fm
	}
	return scc
}

func inducedSCCSize(nodes []graph.NodeID, adj map[graph.NodeID][]graph.NodeID) int {
	if len(nodes) == 0 {
		return 0
	}
	index := 0
	indices := map[graph.NodeID]int{}
	lowlink := map[graph.NodeID]int{}
	onStack := map[graph.NodeID]bool{}
	stack := []graph.NodeID{}
	maxSCC := 0

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
			size := 0
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				size++
				if w == v {
					break
				}
			}
			if size > maxSCC {
				maxSCC = size
			}
		}
	}
	for _, id := range nodes {
		if _, visited := indices[id]; !visited {
			strongConnect(id)
		}
	}
	return maxSCC
}

func cycleIsFileModule(c Cycle, byID map[graph.NodeID]graph.Node) bool {
	if len(c.Path) == 0 {
		return false
	}
	for _, id := range c.Path {
		n, ok := byID[id]
		if !ok {
			return false
		}
		if n.Kind != parse.KindFile && n.Kind != parse.KindModule {
			return false
		}
	}
	return true
}

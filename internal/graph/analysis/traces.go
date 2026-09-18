package analysis

import (
	"fmt"
	"sort"

	"github.com/keelwright-hq/synapse/internal/graph"
	"github.com/keelwright-hq/synapse/internal/parse"
)

// IndirectTrace represents a multi-hop call/dependency chain across namespaces or modules.
type IndirectTrace struct {
	From string         `json:"from"`
	To   string         `json:"to"`
	Hops int            `json:"hops"`
	Path []graph.NodeID `json:"path"`
}

// FindIndirectTraces discovers indirect call chains (length >= 2 hops) between
// distinct modules/files, traversing only resolved call edges from the dependency view.
func FindIndirectTraces(nodes []graph.Node, edges []graph.Edge, limit int) []IndirectTrace {
	view := BuildDependencyView(nodes, edges)
	if len(view.Nodes) == 0 {
		return nil
	}

	byID := map[graph.NodeID]graph.Node{}
	for _, n := range view.Nodes {
		byID[n.ID] = n
	}
	for _, n := range nodes {
		if _, ok := byID[n.ID]; !ok {
			byID[n.ID] = n
		}
	}

	adj := map[graph.NodeID][]graph.NodeID{}
	for _, e := range view.Edges {
		if e.Type == parse.EdgeCalls {
			adj[e.From] = append(adj[e.From], e.To)
		}
	}

	var traces []IndirectTrace

	// Run depth-bounded BFS (max depth 4) from key file/function nodes in the view.
	for _, src := range view.Nodes {
		if src.Kind != parse.KindFile && src.Kind != parse.KindFunction {
			continue
		}

		type pathNode struct {
			curr graph.NodeID
			path []graph.NodeID
		}

		queue := []pathNode{{curr: src.ID, path: []graph.NodeID{src.ID}}}
		visited := map[graph.NodeID]bool{src.ID: true}

		for len(queue) > 0 {
			pn := queue[0]
			queue = queue[1:]

			if len(pn.path) > 4 {
				continue
			}

			for _, nextID := range adj[pn.curr] {
				if visited[nextID] {
					continue
				}
				newPath := append(append([]graph.NodeID{}, pn.path...), nextID)
				if len(newPath) >= 3 { // 2+ hops
					nextN := byID[nextID]
					if nextN.Path != "" && nextN.Path != src.Path {
						traces = append(traces, IndirectTrace{
							From: fmt.Sprintf("%s (%s)", src.Name, src.Path),
							To:   fmt.Sprintf("%s (%s)", nextN.Name, nextN.Path),
							Hops: len(newPath) - 1,
							Path: newPath,
						})
					}
				}
				visited[nextID] = true
				queue = append(queue, pathNode{curr: nextID, path: newPath})
			}
		}
	}

	sort.Slice(traces, func(i, j int) bool {
		if traces[i].Hops != traces[j].Hops {
			return traces[i].Hops > traces[j].Hops
		}
		return traces[i].From < traces[j].From
	})

	if limit > 0 && len(traces) > limit {
		traces = traces[:limit]
	}

	return traces
}

// ShortestPath computes the shortest path between startID and targetID using BFS.
func ShortestPath(nodes []graph.Node, edges []graph.Edge, startID, targetID graph.NodeID) []graph.NodeID {
	if startID == targetID {
		return []graph.NodeID{startID}
	}

	view := BuildDependencyView(nodes, edges)
	adj := map[graph.NodeID][]graph.NodeID{}
	for _, e := range view.Edges {
		adj[e.From] = append(adj[e.From], e.To)
	}

	queue := [][]graph.NodeID{{startID}}
	visited := map[graph.NodeID]bool{startID: true}

	for len(queue) > 0 {
		currPath := queue[0]
		queue = queue[1:]
		last := currPath[len(currPath)-1]

		if last == targetID {
			return currPath
		}

		for _, neighbor := range adj[last] {
			if !visited[neighbor] {
				visited[neighbor] = true
				newPath := append(append([]graph.NodeID{}, currPath...), neighbor)
				queue = append(queue, newPath)
			}
		}
	}

	return nil
}

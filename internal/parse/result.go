package parse

import (
	"sort"

	"github.com/keelwright-hq/synapse/internal/graph"
)

// Result is the intermediate Node/Edge IR for one source file.
type Result struct {
	Path    string       `json:"path"`
	Lang    string       `json:"lang"`
	Nodes   []graph.Node `json:"nodes"`
	Edges   []graph.Edge `json:"edges"`
	Skipped bool         `json:"skipped,omitempty"`
}

// Normalize sorts nodes and edges for deterministic golden comparisons.
func (r *Result) Normalize() {
	sort.Slice(r.Nodes, func(i, j int) bool {
		return r.Nodes[i].ID < r.Nodes[j].ID
	})
	sort.Slice(r.Edges, func(i, j int) bool {
		a, b := r.Edges[i], r.Edges[j]
		if a.From != b.From {
			return a.From < b.From
		}
		if a.To != b.To {
			return a.To < b.To
		}
		return a.Type < b.Type
	})
	if len(r.Edges) < 2 {
		return
	}
	w := 1
	for i := 1; i < len(r.Edges); i++ {
		if sameEdgeIdentity(r.Edges[i], r.Edges[w-1]) {
			continue
		}
		r.Edges[w] = r.Edges[i]
		w++
	}
	r.Edges = r.Edges[:w]
}

func sameEdgeIdentity(a, b graph.Edge) bool {
	return a.From == b.From && a.To == b.To && a.Type == b.Type
}

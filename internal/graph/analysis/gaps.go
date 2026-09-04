package analysis

import (
	"sort"

	"github.com/keelwright-hq/synapse/internal/graph"
	"github.com/keelwright-hq/synapse/internal/parse"
)

// KnowledgeGaps holds summary of orphan nodes and thin communities.
type KnowledgeGaps struct {
	IsolatedNodes   []graph.NodeID `json:"isolated_nodes"`
	ThinCommunities []string       `json:"thin_communities"`
}

// AnalyzeKnowledgeGaps finds isolated nodes (<=1 edge) and thin clusters.
func AnalyzeKnowledgeGaps(nodes []graph.Node, edges []graph.Edge, communities []Community) KnowledgeGaps {
	deg := map[graph.NodeID]int{}
	for _, e := range edges {
		if e.Type == parse.EdgeContains {
			continue
		}
		deg[e.From]++
		deg[e.To]++
	}

	var isolated []graph.NodeID
	for _, n := range nodes {
		if n.Kind == parse.KindSymbol {
			continue
		}
		if deg[n.ID] <= 1 {
			isolated = append(isolated, n.ID)
		}
	}

	sort.Slice(isolated, func(i, j int) bool { return isolated[i] < isolated[j] })

	var thin []string
	for _, c := range communities {
		if len(c.NodeIDs) < 3 {
			thin = append(thin, c.Name)
		}
	}

	return KnowledgeGaps{
		IsolatedNodes:   isolated,
		ThinCommunities: thin,
	}
}

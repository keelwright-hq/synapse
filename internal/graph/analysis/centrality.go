package analysis

import (
	"sort"

	"github.com/keelwright-hq/synapse/internal/graph"
	"github.com/keelwright-hq/synapse/internal/parse"
)

// NodeCentrality holds centrality scores for a single node.
type NodeCentrality struct {
	ID          graph.NodeID `json:"id"`
	Kind        string       `json:"kind"`
	Name        string       `json:"name"`
	Path        string       `json:"path"`
	PageRank    float64      `json:"pagerank"`
	Betweenness float64      `json:"betweenness"`
}

// ComputePageRank computes PageRank scores over directed calls/implements/consumes/imports edges.
func ComputePageRank(nodes []graph.Node, edges []graph.Edge, damping float64, iterations int) map[graph.NodeID]float64 {
	if len(nodes) == 0 {
		return nil
	}
	if damping <= 0 || damping >= 1 {
		damping = 0.85
	}
	if iterations <= 0 {
		iterations = 20
	}

	n := len(nodes)
	initialScore := 1.0 / float64(n)
	pr := map[graph.NodeID]float64{}
	outDegree := map[graph.NodeID]int{}
	inEdgesMap := map[graph.NodeID][]graph.NodeID{}

	for _, nd := range nodes {
		pr[nd.ID] = initialScore
	}

	for _, e := range edges {
		if e.Type == parse.EdgeContains {
			continue
		}
		outDegree[e.From]++
		inEdgesMap[e.To] = append(inEdgesMap[e.To], e.From)
	}

	for iter := 0; iter < iterations; iter++ {
		nextPR := map[graph.NodeID]float64{}
		danglingSum := 0.0
		for _, nd := range nodes {
			if outDegree[nd.ID] == 0 {
				danglingSum += pr[nd.ID]
			}
		}

		danglingShare := (damping * danglingSum) / float64(n)
		baseScore := (1.0 - damping) / float64(n)

		for _, nd := range nodes {
			sumIn := 0.0
			for _, inNode := range inEdgesMap[nd.ID] {
				if outDegree[inNode] > 0 {
					sumIn += pr[inNode] / float64(outDegree[inNode])
				}
			}
			nextPR[nd.ID] = baseScore + danglingShare + (damping * sumIn)
		}
		pr = nextPR
	}

	return pr
}

// ComputeBetweenness computes approximate betweenness centrality using Brandes' Algorithm.
func ComputeBetweenness(nodes []graph.Node, edges []graph.Edge) map[graph.NodeID]float64 {
	cb := map[graph.NodeID]float64{}
	if len(nodes) == 0 {
		return cb
	}

	adj := map[graph.NodeID][]graph.NodeID{}
	for _, e := range edges {
		if e.Type == parse.EdgeContains {
			continue
		}
		adj[e.From] = append(adj[e.From], e.To)
	}

	// For medium-sized graphs run Brandes from each node s
	for _, sNode := range nodes {
		s := sNode.ID
		S := []graph.NodeID{}
		P := map[graph.NodeID][]graph.NodeID{}
		sigma := map[graph.NodeID]float64{}
		d := map[graph.NodeID]int{}

		for _, n := range nodes {
			sigma[n.ID] = 0
			d[n.ID] = -1
		}
		sigma[s] = 1.0
		d[s] = 0

		Q := []graph.NodeID{s}
		for len(Q) > 0 {
			v := Q[0]
			Q = Q[1:]
			S = append(S, v)

			for _, w := range adj[v] {
				if d[w] < 0 {
					Q = append(Q, w)
					d[w] = d[v] + 1
				}
				if d[w] == d[v]+1 {
					sigma[w] += sigma[v]
					P[w] = append(P[w], v)
				}
			}
		}

		delta := map[graph.NodeID]float64{}
		for len(S) > 0 {
			w := S[len(S)-1]
			S = S[:len(S)-1]

			for _, v := range P[w] {
				if sigma[w] > 0 {
					delta[v] += (sigma[v] / sigma[w]) * (1.0 + delta[w])
				}
			}
			if w != s {
				cb[w] += delta[w]
			}
		}
	}

	return cb
}

// RankCentrality returns top N nodes sorted by PageRank score.
func RankCentrality(nodes []graph.Node, edges []graph.Edge, limit int) []NodeCentrality {
	pr := ComputePageRank(nodes, edges, 0.85, 20)
	cb := ComputeBetweenness(nodes, edges)

	var list []NodeCentrality
	for _, n := range nodes {
		if n.Kind == parse.KindSymbol {
			continue
		}
		list = append(list, NodeCentrality{
			ID:          n.ID,
			Kind:        n.Kind,
			Name:        n.Name,
			Path:        n.Path,
			PageRank:    pr[n.ID],
			Betweenness: cb[n.ID],
		})
	}

	sort.Slice(list, func(i, j int) bool {
		if list[i].PageRank != list[j].PageRank {
			return list[i].PageRank > list[j].PageRank
		}
		return list[i].ID < list[j].ID
	})

	if limit > 0 && len(list) > limit {
		list = list[:limit]
	}
	return list
}

package analysis

import (
	"math/rand"
	"sort"
	"strconv"

	"github.com/keelwright-hq/synapse/internal/graph"
	"github.com/keelwright-hq/synapse/internal/parse"
)

// Community represents a detected structural cluster in the graph.
type Community struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`     // Label derived from dominant node hub
	Cohesion float64        `json:"cohesion"` // Edge density: 2 * E_in / (V * (V - 1))
	NodeIDs  []graph.NodeID `json:"node_ids"`
}

// DetectCommunities partitions resolved dependency-view nodes into communities
// using Label Propagation. KindSymbol and KindImport are excluded; only
// file/module/function/type/method nodes that appear in resolved edges participate.
func DetectCommunities(nodes []graph.Node, edges []graph.Edge) []Community {
	view := BuildDependencyView(nodes, edges)
	if len(view.Nodes) == 0 {
		return nil
	}

	byID := map[graph.NodeID]graph.Node{}
	for _, n := range view.Nodes {
		if !isCommunityKind(n.Kind) {
			continue
		}
		byID[n.ID] = n
	}
	if len(byID) == 0 {
		return nil
	}

	// Build undirected adjacency from resolved view edges only.
	adj := map[graph.NodeID][]graph.NodeID{}
	for _, e := range view.Edges {
		if !nodeIn(byID, e.From) || !nodeIn(byID, e.To) {
			continue
		}
		adj[e.From] = append(adj[e.From], e.To)
		adj[e.To] = append(adj[e.To], e.From)
	}

	// Initialize each participating node with its own unique label.
	label := map[graph.NodeID]string{}
	nodeList := make([]graph.NodeID, 0, len(byID))
	for id := range byID {
		label[id] = string(id)
		nodeList = append(nodeList, id)
	}
	sort.Slice(nodeList, func(i, j int) bool { return nodeList[i] < nodeList[j] })

	// Run Label Propagation iterations (max 15 iterations)
	rng := rand.New(rand.NewSource(42))
	for iter := 0; iter < 15; iter++ {
		changed := false
		rng.Shuffle(len(nodeList), func(i, j int) {
			nodeList[i], nodeList[j] = nodeList[j], nodeList[i]
		})

		for _, id := range nodeList {
			neighbors := adj[id]
			if len(neighbors) == 0 {
				continue
			}
			counts := map[string]int{}
			maxCount := 0
			for _, neighbor := range neighbors {
				if _, ok := byID[neighbor]; !ok {
					continue
				}
				l := label[neighbor]
				counts[l]++
				if counts[l] > maxCount {
					maxCount = counts[l]
				}
			}
			if maxCount == 0 {
				continue
			}
			var candidates []string
			for l, c := range counts {
				if c == maxCount {
					candidates = append(candidates, l)
				}
			}
			sort.Strings(candidates)
			bestLabel := candidates[0]
			if label[id] != bestLabel {
				label[id] = bestLabel
				changed = true
			}
		}
		if !changed {
			break
		}
	}

	groups := map[string][]graph.NodeID{}
	for _, id := range nodeList {
		l := label[id]
		groups[l] = append(groups[l], id)
	}

	var result []Community
	commIndex := 0

	edgeSet := map[string]bool{}
	for _, e := range view.Edges {
		edgeSet[string(e.From)+"->"+string(e.To)] = true
		edgeSet[string(e.To)+"->"+string(e.From)] = true
	}

	for _, memberIDs := range groups {
		if len(memberIDs) == 0 {
			continue
		}
		sort.Slice(memberIDs, func(i, j int) bool { return memberIDs[i] < memberIDs[j] })

		name := deriveCommunityName(memberIDs, byID, adj)
		cohesion := computeCohesion(memberIDs, edgeSet)

		result = append(result, Community{
			ID:       strconv.Itoa(commIndex),
			Name:     name,
			Cohesion: cohesion,
			NodeIDs:  memberIDs,
		})
		commIndex++
	}

	sort.Slice(result, func(i, j int) bool {
		if len(result[i].NodeIDs) != len(result[j].NodeIDs) {
			return len(result[i].NodeIDs) > len(result[j].NodeIDs)
		}
		return result[i].Name < result[j].Name
	})

	// Reassign stable IDs after sort.
	for i := range result {
		result[i].ID = strconv.Itoa(i)
	}

	return result
}

func isCommunityKind(kind string) bool {
	switch kind {
	case parse.KindFile, parse.KindModule, parse.KindFunction, parse.KindType, parse.KindMethod:
		return true
	default:
		return false
	}
}

func nodeIn(byID map[graph.NodeID]graph.Node, id graph.NodeID) bool {
	_, ok := byID[id]
	return ok
}

func deriveCommunityName(memberIDs []graph.NodeID, byID map[graph.NodeID]graph.Node, adj map[graph.NodeID][]graph.NodeID) string {
	bestID := memberIDs[0]
	maxDeg := -1
	for _, id := range memberIDs {
		n, ok := byID[id]
		if !ok {
			continue
		}
		deg := len(adj[id])
		// Prefer file/module nodes
		if n.Kind == parse.KindFile || n.Kind == parse.KindModule {
			deg += 100
		}
		if deg > maxDeg {
			maxDeg = deg
			bestID = id
		}
	}
	n := byID[bestID]
	if n.Name != "" {
		return n.Name
	}
	if n.Path != "" {
		return n.Path
	}
	return string(bestID)
}

func computeCohesion(memberIDs []graph.NodeID, edgeSet map[string]bool) float64 {
	v := len(memberIDs)
	if v <= 1 {
		return 1.0
	}
	internalEdges := 0
	for i := 0; i < v; i++ {
		for j := i + 1; j < v; j++ {
			if edgeSet[string(memberIDs[i])+"->"+string(memberIDs[j])] {
				internalEdges++
			}
		}
	}
	maxEdges := (v * (v - 1)) / 2
	return float64(internalEdges) / float64(maxEdges)
}

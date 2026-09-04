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

// DetectCommunities partitions graph nodes into communities using Label Propagation.
// It returns a list of communities sorted by size (descending).
func DetectCommunities(nodes []graph.Node, edges []graph.Edge) []Community {
	if len(nodes) == 0 {
		return nil
	}

	byID := map[graph.NodeID]graph.Node{}
	for _, n := range nodes {
		byID[n.ID] = n
	}

	// Build undirected adjacency list
	adj := map[graph.NodeID][]graph.NodeID{}
	for _, e := range edges {
		if e.Type == parse.EdgeContains {
			continue
		}
		adj[e.From] = append(adj[e.From], e.To)
		adj[e.To] = append(adj[e.To], e.From)
	}

	// Initialize each node with its own unique label
	label := map[graph.NodeID]string{}
	nodeList := make([]graph.NodeID, 0, len(nodes))
	for _, n := range nodes {
		label[n.ID] = string(n.ID)
		nodeList = append(nodeList, n.ID)
	}

	// Run Label Propagation iterations (max 15 iterations)
	rng := rand.New(rand.NewSource(42))
	for iter := 0; iter < 15; iter++ {
		changed := false
		// Shuffle node order for asynchronous update
		rng.Shuffle(len(nodeList), func(i, j int) {
			nodeList[i], nodeList[j] = nodeList[j], nodeList[i]
		})

		for _, id := range nodeList {
			neighbors := adj[id]
			if len(neighbors) == 0 {
				continue
			}
			// Count neighbor labels
			counts := map[string]int{}
			maxCount := 0
			for _, neighbor := range neighbors {
				l := label[neighbor]
				counts[l]++
				if counts[l] > maxCount {
					maxCount = counts[l]
				}
			}
			// Collect top candidate labels
			var candidates []string
			for l, c := range counts {
				if c == maxCount {
					candidates = append(candidates, l)
				}
			}
			sort.Strings(candidates) // Deterministic tie-breaking
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

	// Group node IDs by label
	groups := map[string][]graph.NodeID{}
	for _, n := range nodes {
		l := label[n.ID]
		groups[l] = append(groups[l], n.ID)
	}

	// Build result community structures
	var result []Community
	commIndex := 0

	// Pre-build internal edge maps for cohesion scoring
	edgeSet := map[string]bool{}
	for _, e := range edges {
		edgeSet[string(e.From)+"->"+string(e.To)] = true
		edgeSet[string(e.To)+"->"+string(e.From)] = true
	}

	for _, memberIDs := range groups {
		if len(memberIDs) == 0 {
			continue
		}

		// Find dominant node for community naming
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

	// Sort communities by size descending
	sort.Slice(result, func(i, j int) bool {
		if len(result[i].NodeIDs) != len(result[j].NodeIDs) {
			return len(result[i].NodeIDs) > len(result[j].NodeIDs)
		}
		return result[i].Name < result[j].Name
	})

	return result
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

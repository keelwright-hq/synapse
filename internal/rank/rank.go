package rank

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/taricsa/synapse/internal/graph"
	"github.com/taricsa/synapse/internal/parse"
)

// DefaultEdgeWeights score adjacency by edge type (higher = closer / more relevant).
var DefaultEdgeWeights = map[graph.EdgeType]float64{
	parse.EdgeContains: 1.0,
	parse.EdgeCalls:    0.8,
	parse.EdgeImports:  0.5,
}

// Options configure neighborhood ranking.
type Options struct {
	Depth     int
	MaxNodes  int
	Budget    int // max response characters; 0 = unlimited
	RootDir   string
	Weights   map[graph.EdgeType]float64
}

// Hit is one ranked neighborhood entry.
type Hit struct {
	ID         graph.NodeID `json:"id"`
	Kind       string       `json:"kind"`
	Name       string       `json:"name,omitempty"`
	Path       string       `json:"path,omitempty"`
	Score      float64      `json:"score"`
	Depth      int          `json:"depth"`
	EdgeReason string       `json:"edge_reason,omitempty"`
	StartLine  int          `json:"start_line,omitempty"`
	EndLine    int          `json:"end_line,omitempty"`
	Snippet    string       `json:"snippet,omitempty"`
}

// Result is a ranked neighborhood pack.
type Result struct {
	Seed      graph.NodeID `json:"seed"`
	Hits      []Hit        `json:"hits"`
	Truncated bool         `json:"truncated,omitempty"`
}

// Neighborhood BFS-ranks nodes around seed with edge weights and optional budget.
func Neighborhood(store graph.Store, seed graph.NodeID, opts Options) (Result, error) {
	if opts.Depth <= 0 {
		opts.Depth = 2
	}
	if opts.MaxNodes <= 0 {
		opts.MaxNodes = 32
	}
	weights := opts.Weights
	if weights == nil {
		weights = DefaultEdgeWeights
	}

	seedNode, err := store.GetNode(seed)
	if err != nil {
		return Result{}, err
	}

	type item struct {
		id     graph.NodeID
		depth  int
		score  float64
		reason string
	}
	queue := []item{{id: seed, depth: 0, score: 1.0, reason: "seed"}}
	best := map[graph.NodeID]item{seed: queue[0]}

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur.depth >= opts.Depth {
			continue
		}
		edges := make([]graph.Edge, 0)
		out, err := store.OutEdges(cur.id, "")
		if err != nil {
			return Result{}, err
		}
		edges = append(edges, out...)
		in, err := store.InEdges(cur.id, "")
		if err != nil {
			return Result{}, err
		}
		edges = append(edges, in...)

		for _, e := range edges {
			neighbor := e.To
			dir := "out"
			if e.To == cur.id {
				neighbor = e.From
				dir = "in"
			}
			w := weights[e.Type]
			if w == 0 {
				w = 0.3
			}
			score := cur.score * w
			reason := fmt.Sprintf("%s:%s", dir, e.Type)
			next := item{id: neighbor, depth: cur.depth + 1, score: score, reason: reason}
			if prev, ok := best[neighbor]; ok {
				if next.score <= prev.score {
					continue
				}
			}
			best[neighbor] = next
			queue = append(queue, next)
		}
	}

	hits := make([]Hit, 0, len(best))
	for _, it := range best {
		node, err := store.GetNode(it.id)
		if err != nil {
			if err == graph.ErrNotFound {
				continue
			}
			return Result{}, err
		}
		h := Hit{
			ID:         node.ID,
			Kind:       node.Kind,
			Name:       node.Name,
			Path:       node.Path,
			Score:      it.score,
			Depth:      it.depth,
			EdgeReason: it.reason,
			StartLine:  propInt(node.Props, "start_line"),
			EndLine:    propInt(node.Props, "end_line"),
		}
		h.Snippet = extractSnippet(opts.RootDir, node, h.StartLine, h.EndLine)
		hits = append(hits, h)
	}

	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		if hits[i].Depth != hits[j].Depth {
			return hits[i].Depth < hits[j].Depth
		}
		return hits[i].ID < hits[j].ID
	})

	out := Result{Seed: seedNode.ID, Hits: hits}
	if len(out.Hits) > opts.MaxNodes {
		out.Hits = out.Hits[:opts.MaxNodes]
		out.Truncated = true
	}
	if opts.Budget > 0 {
		out.Hits, out.Truncated = PackBudget(out.Hits, opts.Budget)
	}
	return out, nil
}

// PackBudget keeps hits in order until estimated character budget is exhausted.
func PackBudget(hits []Hit, budget int) ([]Hit, bool) {
	if budget <= 0 || len(hits) == 0 {
		return hits, false
	}
	var (
		used  int
		out   []Hit
		trunc bool
	)
	for _, h := range hits {
		cost := estimateChars(h)
		if used+cost > budget && len(out) > 0 {
			trunc = true
			break
		}
		if used+cost > budget && len(out) == 0 {
			// Always keep at least the first hit, truncated snippet.
			h.Snippet = truncate(h.Snippet, max(0, budget-80))
			out = append(out, h)
			trunc = true
			break
		}
		out = append(out, h)
		used += cost
	}
	if len(out) < len(hits) {
		trunc = true
	}
	return out, trunc
}

func estimateChars(h Hit) int {
	n := len(h.ID) + len(h.Kind) + len(h.Name) + len(h.Path) + len(h.EdgeReason) + len(h.Snippet) + 48
	return n
}

func truncate(s string, n int) string {
	if n < 0 {
		n = 0
	}
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func propInt(props map[string]string, key string) int {
	if props == nil {
		return 0
	}
	v, err := strconv.Atoi(props[key])
	if err != nil {
		return 0
	}
	return v
}

func extractSnippet(root string, node graph.Node, start, end int) string {
	if node.Path == "" {
		return ""
	}
	path := node.Path
	if root != "" && !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	if start > 0 && end >= start && start <= len(lines) {
		if end > len(lines) {
			end = len(lines)
		}
		return strings.Join(lines[start-1:end], "\n")
	}
	// Fallback: first line containing the symbol name.
	if node.Name == "" {
		return ""
	}
	for i, line := range lines {
		if strings.Contains(line, node.Name) {
			from := i
			to := i + 1
			if to > len(lines) {
				to = len(lines)
			}
			return strings.Join(lines[from:to], "\n")
		}
	}
	return ""
}

// ResolveSeed finds a node by exact ID or unique name.
func ResolveSeed(store graph.Store, query string) (graph.NodeID, error) {
	if query == "" {
		return "", fmt.Errorf("rank: empty seed")
	}
	if n, err := store.GetNode(graph.NodeID(query)); err == nil {
		return n.ID, nil
	}
	var matches []graph.NodeID
	// Name lookup scans nodes until we know uniqueness (0/1) or ambiguity (≥2).
	// A secondary name index can replace this for very large graphs.
	err := store.ForEachNode(func(n graph.Node) bool {
		if n.Name == query || string(n.ID) == query {
			matches = append(matches, n.ID)
			if len(matches) >= 2 {
				return false
			}
		}
		return true
	})
	if err != nil {
		return "", err
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("%w: symbol %q", graph.ErrNotFound, query)
	}
	return "", fmt.Errorf("rank: ambiguous symbol %q (%d matches)", query, len(matches))
}

// Search returns nodes whose name or id contains query (case-sensitive), capped.
func Search(store graph.Store, query string, limit int) ([]graph.Node, error) {
	if limit <= 0 {
		limit = 20
	}
	var out []graph.Node
	err := store.ForEachNode(func(n graph.Node) bool {
		if query == "" || strings.Contains(string(n.ID), query) || strings.Contains(n.Name, query) {
			out = append(out, n)
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// FindReferences returns nodes with an incoming edge of the given type (default calls).
func FindReferences(store graph.Store, target graph.NodeID, edgeType graph.EdgeType) ([]graph.Edge, error) {
	if edgeType == "" {
		edgeType = parse.EdgeCalls
	}
	return store.InEdges(target, edgeType)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

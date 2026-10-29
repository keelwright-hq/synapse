package rank

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/keelwright-hq/synapse/internal/graph"
	"github.com/keelwright-hq/synapse/internal/parse"
	"github.com/keelwright-hq/synapse/internal/uri"
)

// DefaultEdgeWeights score adjacency by edge type (higher = closer / more relevant).
// Phase 3 families (git / runtime / docs) are explicit so they do not fall back to 0.3.
var DefaultEdgeWeights = map[graph.EdgeType]float64{
	parse.EdgeContains:      0.75,
	parse.EdgeCalls:         0.8,
	parse.EdgeImplements:    0.9,
	parse.EdgeConsumes:      0.9,
	parse.EdgeImports:       0.5,
	parse.EdgeCoCommitted:   0.7,  // HISTORICAL co-change
	parse.EdgeObservedCalls: 0.85, // OBSERVED runtime
	parse.EdgeDocuments:     0.6,  // doc ↔ symbol
}

// Options configure neighborhood ranking.
type Options struct {
	Depth    int
	MaxNodes int
	Budget   int // max response characters; 0 = unlimited
	RootDir  string
	// RepoRoots maps logical repo:// names to filesystem roots (workspace mode).
	// When set, snippet extraction prefers the root for the node's repo_uri.
	RepoRoots map[string]string
	Weights   map[graph.EdgeType]float64
	// FamilyWeights optionally scale DefaultEdgeWeights by family (SYN-86).
	FamilyWeights map[EdgeFamily]float64
	// Explain attaches per-family score contributions on each hit (SYN-88).
	Explain bool
}

// ScorePart is one family's contribution to a hit score.
type ScorePart struct {
	Family EdgeFamily `json:"family"`
	Type   string     `json:"edge_type"`
	Weight float64    `json:"weight"`
	Dir    string     `json:"dir"`
}

// Hit is one ranked neighborhood entry.
type Hit struct {
	ID         graph.NodeID `json:"id"`
	RepoURI    string       `json:"repo_uri,omitempty"`
	Kind       string       `json:"kind"`
	Name       string       `json:"name,omitempty"`
	Path       string       `json:"path,omitempty"`
	Score      float64      `json:"score"`
	Depth      int          `json:"depth"`
	EdgeReason string       `json:"edge_reason,omitempty"`
	Explain    []ScorePart  `json:"explain,omitempty"`
	StartLine  int          `json:"start_line,omitempty"`
	EndLine    int          `json:"end_line,omitempty"`
	Snippet    string       `json:"snippet,omitempty"`
}

type repoScopedStore interface {
	PinnedRepo(id graph.NodeID) string
	GetNodeInRepo(repo string, id graph.NodeID) (graph.Node, error)
	OutEdgesInRepo(repo string, from graph.NodeID, edgeType graph.EdgeType) ([]graph.Edge, error)
	InEdgesInRepo(repo string, to graph.NodeID, edgeType graph.EdgeType) ([]graph.Edge, error)
}

// Result is a ranked neighborhood pack.
type Result struct {
	Seed      graph.NodeID `json:"seed"`
	Hits      []Hit        `json:"hits"`
	Truncated bool         `json:"truncated,omitempty"`
	Warnings  []string     `json:"warnings,omitempty"`
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
	if weights == nil && opts.FamilyWeights != nil {
		weights = WeightsFromFamilies(opts.FamilyWeights)
	}
	if weights == nil {
		weights = DefaultEdgeWeights
	}

	seedNode, err := store.GetNode(seed)
	if err != nil {
		return Result{}, err
	}

	rss, _ := store.(repoScopedStore)
	seedRepo := ""
	if rss != nil {
		seedRepo = rss.PinnedRepo(seed)
		if seedRepo == "" && seedNode.Props != nil {
			if u, perr := uri.Parse(seedNode.Props[uri.PropKey]); perr == nil {
				seedRepo = u.Repo
			}
		}
	}

	type nodeKey struct {
		repo string
		id   graph.NodeID
	}
	type item struct {
		repo    string
		id      graph.NodeID
		depth   int
		score   float64
		reason  string
		explain []ScorePart
	}
	seedItem := item{repo: seedRepo, id: seed, depth: 0, score: 1.0, reason: "seed"}
	queue := []item{seedItem}
	best := map[nodeKey]item{{repo: seedRepo, id: seed}: seedItem}

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur.depth >= opts.Depth {
			continue
		}
		var (
			out []graph.Edge
			in  []graph.Edge
		)
		if rss != nil && cur.repo != "" {
			out, err = rss.OutEdgesInRepo(cur.repo, cur.id, "")
			if err != nil {
				return Result{}, err
			}
			in, err = rss.InEdgesInRepo(cur.repo, cur.id, "")
			if err != nil {
				return Result{}, err
			}
		} else {
			out, err = store.OutEdges(cur.id, "")
			if err != nil {
				return Result{}, err
			}
			in, err = store.InEdges(cur.id, "")
			if err != nil {
				return Result{}, err
			}
		}

		visit := func(e graph.Edge, dir string, neighbor graph.NodeID, neighborRepo string) {
			if neighborRepo == "" {
				neighborRepo = cur.repo
			}
			w := weights[e.Type]
			if w == 0 {
				w = 0.3
			}
			w *= edgeSignalMultiplier(e.Props)
			score := cur.score * w
			reason := fmt.Sprintf("%s:%s", dir, e.Type)
			var explain []ScorePart
			if opts.Explain {
				explain = append(append([]ScorePart{}, cur.explain...), ScorePart{
					Family: FamilyOf(e.Type),
					Type:   string(e.Type),
					Weight: w,
					Dir:    dir,
				})
			}
			nKey := nodeKey{repo: neighborRepo, id: neighbor}
			next := item{repo: neighborRepo, id: neighbor, depth: cur.depth + 1, score: score, reason: reason, explain: explain}
			if prev, ok := best[nKey]; ok {
				if next.score <= prev.score {
					return
				}
			}
			best[nKey] = next
			queue = append(queue, next)
		}

		for _, e := range out {
			toRepo := ""
			if e.Props != nil {
				toRepo = e.Props["to_repo"]
			}
			visit(e, "out", e.To, toRepo)
		}
		for _, e := range in {
			fromRepo := ""
			if e.Props != nil {
				fromRepo = e.Props["from_repo"]
			}
			visit(e, "in", e.From, fromRepo)
		}
	}

	hits := make([]Hit, 0, len(best))
	for _, it := range best {
		var node graph.Node
		if rss != nil && it.repo != "" {
			node, err = rss.GetNodeInRepo(it.repo, it.id)
		} else {
			node, err = store.GetNode(it.id)
		}
		if err != nil {
			if err == graph.ErrNotFound {
				continue
			}
			return Result{}, err
		}
		h := Hit{
			ID:         node.ID,
			RepoURI:    propURI(node),
			Kind:       node.Kind,
			Name:       node.Name,
			Path:       node.Path,
			Score:      it.score,
			Depth:      it.depth,
			EdgeReason: it.reason,
			Explain:    it.explain,
			StartLine:  propInt(node.Props, "start_line"),
			EndLine:    propInt(node.Props, "end_line"),
		}
		h.Snippet = extractSnippet(rootForNode(opts, node), node, h.StartLine, h.EndLine)
		hits = append(hits, h)
	}

	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		if hits[i].Depth != hits[j].Depth {
			return hits[i].Depth < hits[j].Depth
		}
		if hits[i].ID != hits[j].ID {
			return hits[i].ID < hits[j].ID
		}
		return hits[i].RepoURI < hits[j].RepoURI
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

// edgeSignalMultiplier returns a bounded multiplier in (0.95, 1.05) based on
// per-edge weight or count properties (1.0 when absent or equal to 1).
func edgeSignalMultiplier(props map[string]string) float64 {
	if props == nil {
		return 1.0
	}
	var signal float64
	if raw := props[parse.PropWeight]; raw != "" {
		if v, err := strconv.ParseFloat(raw, 64); err == nil && v > 0 {
			signal = v
		}
	}
	if raw := props[parse.PropCount]; raw != "" {
		if v, err := strconv.ParseFloat(raw, 64); err == nil && v > signal {
			signal = v
		}
	}
	if signal <= 0 || signal == 1.0 {
		return 1.0
	}
	return 0.95 + 0.1*(signal/(signal+1.0))
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

// ResolveSeed finds a node by exact Phase-1 ID, unique name, or canonical repo:// URI.
func ResolveSeed(store graph.Store, query string) (graph.NodeID, error) {
	if query == "" {
		return "", fmt.Errorf("rank: empty seed")
	}
	if strings.HasPrefix(query, uri.Scheme+"://") {
		n, err := store.GetNodeByURI(query)
		if err != nil {
			return "", err
		}
		return n.ID, nil
	}
	if n, err := store.GetNode(graph.NodeID(query)); err == nil {
		return n.ID, nil
	}
	var (
		codeMatches []graph.Node
		defMatches  []graph.Node
		symMatches  []graph.Node
	)
	// Name lookup scans nodes until we know uniqueness (0/1) or ambiguity (≥2).
	// Real definition nodes take precedence over unresolved symbol/import placeholders,
	// and callable/type definitions take precedence over package/file containers.
	err := store.ForEachNode(func(n graph.Node) bool {
		if n.Name == query || string(n.ID) == query {
			switch n.Kind {
			case parse.KindSymbol, parse.KindImport:
				symMatches = append(symMatches, n)
			case parse.KindFunction, parse.KindMethod, parse.KindType, parse.KindOperation, parse.KindSchema:
				codeMatches = append(codeMatches, n)
				defMatches = append(defMatches, n)
			default:
				defMatches = append(defMatches, n)
			}
		}
		return true
	})
	if err != nil {
		return "", err
	}
	matches := defMatches
	if len(matches) > 1 && len(codeMatches) == 1 {
		matches = codeMatches
	}
	if len(matches) == 0 {
		matches = symMatches
	}
	if len(matches) == 1 {
		if u := propURI(matches[0]); u != "" {
			_, _ = store.GetNodeByURI(u)
		}
		return matches[0].ID, nil
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("%w: symbol %q", graph.ErrNotFound, query)
	}
	return "", fmt.Errorf("rank: ambiguous symbol %q (%d matches); use a repo:// URI or --repo to scope", query, len(matches))
}

// rootForNode picks the filesystem root for snippet extraction.
func rootForNode(opts Options, node graph.Node) string {
	if opts.RepoRoots != nil && node.Props != nil {
		if raw := node.Props[uri.PropKey]; raw != "" {
			if u, err := uri.Parse(raw); err == nil {
				if root, ok := opts.RepoRoots[u.Repo]; ok && root != "" {
					return root
				}
			}
		}
	}
	return opts.RootDir
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
	edges, err := store.InEdges(target, edgeType)
	if err != nil || len(edges) > 0 || edgeType != parse.EdgeCalls {
		return edges, err
	}
	// Fallback for unlinked graphs (e.g. raw ParseFile fixtures): if target is the
	// unique function/method definition for its name, check incoming calls to symbol:<Name>.
	tn, err := store.GetNode(target)
	if err != nil || (tn.Kind != parse.KindFunction && tn.Kind != parse.KindMethod) || tn.Name == "" {
		return edges, nil
	}
	defCount := 0
	_ = store.ForEachNode(func(n graph.Node) bool {
		if (n.Kind == parse.KindFunction || n.Kind == parse.KindMethod) && n.Name == tn.Name {
			defCount++
			return defCount < 2
		}
		return true
	})
	if defCount != 1 {
		return edges, nil
	}
	symID := graph.NodeID("symbol:" + tn.Name)
	symEdges, err := store.InEdges(symID, edgeType)
	if err != nil {
		return edges, nil
	}
	return symEdges, nil
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

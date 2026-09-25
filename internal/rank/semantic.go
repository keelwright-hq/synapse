package rank

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/keelwright-hq/synapse/internal/graph"
	"github.com/keelwright-hq/synapse/internal/parse"
	"github.com/keelwright-hq/synapse/internal/uri"
)

// CoChangeHit is one co_committed neighbor (SYN-21).
type CoChangeHit struct {
	ID       graph.NodeID `json:"id"`
	RepoURI  string       `json:"repo_uri,omitempty"`
	Path     string       `json:"path,omitempty"`
	Weight   float64      `json:"weight"`
	Support  int          `json:"support,omitempty"`
	EdgeFrom graph.NodeID `json:"from"`
	EdgeTo   graph.NodeID `json:"to"`
}

// CoChanges returns top co_committed neighbors for a seed path/symbol.
func CoChanges(store graph.Store, seed string, limit int) ([]CoChangeHit, error) {
	if limit <= 0 {
		limit = 20
	}
	id, err := ResolveSeed(store, seed)
	if err != nil {
		// Allow bare relative paths as file seeds.
		id = graph.NodeID("file:" + strings.TrimPrefix(seed, "file:"))
		if _, err2 := store.GetNode(id); err2 != nil {
			return nil, err
		}
	}
	node, _ := store.GetNode(id)
	fileID := id
	if node.Kind != parse.KindFile && node.Path != "" {
		fileID = graph.NodeID("file:" + node.Path)
	}
	out, err := store.OutEdges(fileID, parse.EdgeCoCommitted)
	if err != nil {
		return nil, err
	}
	in, err := store.InEdges(fileID, parse.EdgeCoCommitted)
	if err != nil {
		return nil, err
	}
	all := append(out, in...)
	hits := make([]CoChangeHit, 0, len(all))
	for _, e := range all {
		other := e.To
		if other == fileID {
			other = e.From
		}
		on, err := store.GetNode(other)
		if err != nil {
			continue
		}
		w, _ := strconv.ParseFloat(e.Props[parse.PropWeight], 64)
		sup, _ := strconv.Atoi(e.Props[parse.PropSupport])
		hits = append(hits, CoChangeHit{
			ID: other, Path: on.Path, Weight: w, Support: sup,
			RepoURI: propURI(on), EdgeFrom: e.From, EdgeTo: e.To,
		})
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].Weight > hits[j].Weight })
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, nil
}

// HotPathHit is one observed_calls neighbor (SYN-21).
type HotPathHit struct {
	ID      graph.NodeID `json:"id"`
	RepoURI string       `json:"repo_uri,omitempty"`
	Name    string       `json:"name,omitempty"`
	Count   int          `json:"count"`
	LastSeen string      `json:"last_seen,omitempty"`
	Dir     string       `json:"dir"` // out|in
}

// HotPaths returns observed_calls neighbors sorted by frequency.
func HotPaths(store graph.Store, seed string, limit int) ([]HotPathHit, error) {
	if limit <= 0 {
		limit = 20
	}
	id, err := ResolveSeed(store, seed)
	if err != nil {
		return nil, err
	}
	out, err := store.OutEdges(id, parse.EdgeObservedCalls)
	if err != nil {
		return nil, err
	}
	in, err := store.InEdges(id, parse.EdgeObservedCalls)
	if err != nil {
		return nil, err
	}
	var hits []HotPathHit
	for _, e := range out {
		n, err := store.GetNode(e.To)
		if err != nil {
			continue
		}
		c, _ := strconv.Atoi(e.Props[parse.PropCount])
		hits = append(hits, HotPathHit{
			ID: n.ID, Name: n.Name, RepoURI: propURI(n), Count: c,
			LastSeen: e.Props[parse.PropLastSeen], Dir: "out",
		})
	}
	for _, e := range in {
		n, err := store.GetNode(e.From)
		if err != nil {
			continue
		}
		c, _ := strconv.Atoi(e.Props[parse.PropCount])
		hits = append(hits, HotPathHit{
			ID: n.ID, Name: n.Name, RepoURI: propURI(n), Count: c,
			LastSeen: e.Props[parse.PropLastSeen], Dir: "in",
		})
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].Count > hits[j].Count })
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, nil
}

// DocHit is documentation tied to a symbol (SYN-21).
type DocHit struct {
	ID      graph.NodeID `json:"id"`
	RepoURI string       `json:"repo_uri,omitempty"`
	Kind    string       `json:"kind"`
	Name    string       `json:"name,omitempty"`
	Path    string       `json:"path,omitempty"`
	Snippet string       `json:"snippet,omitempty"`
	Source  string       `json:"source"` // edge|prop
}

// DocsForSymbol returns documenting nodes and/or PropDoc text for a symbol.
func DocsForSymbol(store graph.Store, seed string, root string, limit int) ([]DocHit, error) {
	if limit <= 0 {
		limit = 20
	}
	id, err := ResolveSeed(store, seed)
	if err != nil {
		return nil, err
	}
	node, err := store.GetNode(id)
	if err != nil {
		return nil, err
	}
	var hits []DocHit
	if doc := node.Props[parse.PropDoc]; doc != "" {
		hits = append(hits, DocHit{
			ID: id, Kind: node.Kind, Name: node.Name, Path: node.Path,
			RepoURI: propURI(node), Snippet: doc, Source: "prop",
		})
	}
	in, err := store.InEdges(id, parse.EdgeDocuments)
	if err != nil {
		return nil, err
	}
	for _, e := range in {
		n, err := store.GetNode(e.From)
		if err != nil {
			continue
		}
		snippet := extractSnippet(root, n, propInt(n.Props, "start_line"), propInt(n.Props, "end_line"))
		hits = append(hits, DocHit{
			ID: n.ID, Kind: n.Kind, Name: n.Name, Path: n.Path,
			RepoURI: propURI(n), Snippet: snippet, Source: "edge",
		})
	}
	if len(hits) > limit {
		hits = hits[:limit]
	}
	if len(hits) == 0 {
		return nil, fmt.Errorf("%w: no docs for %s", graph.ErrNotFound, seed)
	}
	return hits, nil
}

func propURI(n graph.Node) string {
	if n.Props == nil {
		return ""
	}
	return n.Props[uri.PropKey]
}

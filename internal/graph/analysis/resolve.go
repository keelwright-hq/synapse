package analysis

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/keelwright-hq/synapse/internal/graph"
	"github.com/keelwright-hq/synapse/internal/parse"
)

// Coverage describes what dependency resolution could and could not establish.
type Coverage struct {
	ImportEdgesTotal    int      `json:"import_edges_total"`
	ImportEdgesResolved int      `json:"import_edges_resolved"`
	CallEdgesTotal      int      `json:"call_edges_total"`
	CallEdgesResolved   int      `json:"call_edges_resolved"`
	Notes               []string `json:"notes,omitempty"`
}

// Incomplete reports whether any import or call edges remain unresolved.
func (c Coverage) Incomplete() bool {
	return c.ImportEdgesResolved < c.ImportEdgesTotal || c.CallEdgesResolved < c.CallEdgesTotal
}

// DependencyView is a filtered graph of resolved file/module relationships.
type DependencyView struct {
	Nodes    []graph.Node
	Edges    []graph.Edge // only resolved deps between files/modules (and resolved calls between defs)
	Coverage Coverage
}

// BuildDependencyView constructs a single-repo analysis view of resolvable local
// imports and resolved call targets. Raw graph exports are left unchanged.
func BuildDependencyView(nodes []graph.Node, edges []graph.Edge) DependencyView {
	byID := map[graph.NodeID]graph.Node{}
	for _, n := range nodes {
		byID[n.ID] = n
	}

	owner := fileOwnerMap(nodes, edges)
	filesByPath, modulesByPath := indexFileModulePaths(nodes)

	cov := Coverage{
		Notes: []string{
			"JS/TS relative imports supported; Go package imports not module-resolved; unresolved calls excluded",
		},
	}

	seenEdge := map[string]bool{}
	var outEdges []graph.Edge
	participant := map[graph.NodeID]bool{}

	addEdge := func(from, to graph.NodeID, typ graph.EdgeType) {
		if from == "" || to == "" || from == to {
			return
		}
		key := string(from) + "\x00" + string(to) + "\x00" + string(typ)
		if seenEdge[key] {
			return
		}
		seenEdge[key] = true
		outEdges = append(outEdges, graph.Edge{From: from, To: to, Type: typ})
		participant[from] = true
		participant[to] = true
	}

	for _, e := range edges {
		switch e.Type {
		case parse.EdgeImports:
			cov.ImportEdgesTotal++
			to, ok := byID[e.To]
			if !ok || to.Kind != parse.KindImport {
				continue
			}
			spec := strings.TrimSpace(to.Name)
			if !strings.HasPrefix(spec, ".") {
				// Absolute/package imports: count as unresolved; do not invent edges.
				continue
			}
			fromPath := importerPath(byID, owner, e.From, to)
			target, ok := resolveRelativeTarget(spec, fromPath, byID, filesByPath, modulesByPath)
			if !ok {
				continue
			}
			src := analysisEndpoint(byID, owner, e.From)
			if src == "" {
				continue
			}
			dst := target.ID
			addEdge(src, dst, parse.EdgeImports)
			cov.ImportEdgesResolved++

		case parse.EdgeCalls:
			cov.CallEdgesTotal++
			to, ok := byID[e.To]
			if !ok {
				continue
			}
			if to.Kind == parse.KindSymbol || to.Kind == parse.KindImport {
				continue
			}
			from, ok := byID[e.From]
			if !ok {
				continue
			}
			// Keep resolved definition-level calls.
			addEdge(from.ID, to.ID, parse.EdgeCalls)
			cov.CallEdgesResolved++

			// Also emit file-level analysis edges when both ends own files.
			fromFile, hasFrom := owner[e.From]
			toFile, hasTo := owner[e.To]
			if hasFrom && hasTo && fromFile != toFile {
				addEdge(fromFile, toFile, parse.EdgeCalls)
			}
		}
	}

	if cov.ImportEdgesTotal > cov.ImportEdgesResolved {
		cov.Notes = append(cov.Notes,
			"Unresolved or unsupported imports are excluded from cycle, community, and centrality analysis")
	}
	if cov.CallEdgesTotal > cov.CallEdgesResolved {
		cov.Notes = append(cov.Notes,
			"Unresolved call targets (kind=symbol) are excluded from traces and architectural metrics")
	}

	var outNodes []graph.Node
	for _, n := range nodes {
		if !participant[n.ID] {
			continue
		}
		outNodes = append(outNodes, n)
	}
	sort.Slice(outNodes, func(i, j int) bool { return outNodes[i].ID < outNodes[j].ID })
	sort.Slice(outEdges, func(i, j int) bool {
		if outEdges[i].From != outEdges[j].From {
			return outEdges[i].From < outEdges[j].From
		}
		if outEdges[i].To != outEdges[j].To {
			return outEdges[i].To < outEdges[j].To
		}
		return outEdges[i].Type < outEdges[j].Type
	})

	return DependencyView{Nodes: outNodes, Edges: outEdges, Coverage: cov}
}

// fileOwnerMap maps each node ID to its owning file node ID via contains walk
// and path index. Path-based ownership is skipped for KindSymbol and KindImport
// so occurrence/unresolved nodes are never attributed to a source file.
func fileOwnerMap(nodes []graph.Node, edges []graph.Edge) map[graph.NodeID]graph.NodeID {
	byID := map[graph.NodeID]graph.Node{}
	filesByPath := map[string]graph.NodeID{}
	owner := map[graph.NodeID]graph.NodeID{}
	for _, n := range nodes {
		byID[n.ID] = n
		if n.Kind == parse.KindFile {
			owner[n.ID] = n.ID
			if n.Path != "" {
				filesByPath[n.Path] = n.ID
			}
			if strings.HasPrefix(string(n.ID), "file:") {
				filesByPath[strings.TrimPrefix(string(n.ID), "file:")] = n.ID
			}
		}
	}

	parent := map[graph.NodeID]graph.NodeID{}
	for _, e := range edges {
		if e.Type == parse.EdgeContains {
			parent[e.To] = e.From
		}
	}

	visited := map[graph.NodeID]bool{}
	var resolve func(graph.NodeID) (graph.NodeID, bool)
	resolve = func(id graph.NodeID) (graph.NodeID, bool) {
		if fid, ok := owner[id]; ok {
			return fid, true
		}
		if visited[id] {
			return "", false // cyclic contains: terminate
		}
		visited[id] = true
		defer func() { visited[id] = false }()

		n, ok := byID[id]
		if !ok {
			return "", false
		}

		// Skip Path-based ownership for symbols and import occurrences.
		if n.Kind != parse.KindSymbol && n.Kind != parse.KindImport && n.Path != "" {
			if fid, ok := filesByPath[n.Path]; ok {
				owner[id] = fid
				return fid, true
			}
			cand := graph.NodeID("file:" + n.Path)
			if _, ok := byID[cand]; ok {
				owner[id] = cand
				return cand, true
			}
		}

		if p, ok := parent[id]; ok {
			if fid, ok := resolve(p); ok {
				owner[id] = fid
				return fid, true
			}
		}
		return "", false
	}

	for _, n := range nodes {
		_, _ = resolve(n.ID)
	}
	return owner
}

func indexFileModulePaths(nodes []graph.Node) (filesByPath, modulesByPath map[string]graph.NodeID) {
	filesByPath = map[string]graph.NodeID{}
	modulesByPath = map[string]graph.NodeID{}
	for _, n := range nodes {
		path := n.Path
		if path == "" && strings.HasPrefix(string(n.ID), "file:") {
			path = strings.TrimPrefix(string(n.ID), "file:")
		}
		if path == "" && strings.HasPrefix(string(n.ID), "module:") {
			path = strings.TrimPrefix(string(n.ID), "module:")
		}
		if path == "" {
			continue
		}
		switch n.Kind {
		case parse.KindFile:
			filesByPath[path] = n.ID
		case parse.KindModule:
			modulesByPath[path] = n.ID
		}
	}
	return filesByPath, modulesByPath
}

func importerPath(byID map[graph.NodeID]graph.Node, owner map[graph.NodeID]graph.NodeID, fromID graph.NodeID, importNode graph.Node) string {
	if from, ok := byID[fromID]; ok && from.Path != "" {
		return from.Path
	}
	if fid, ok := owner[fromID]; ok {
		if f, ok := byID[fid]; ok && f.Path != "" {
			return f.Path
		}
	}
	if importNode.Path != "" {
		return importNode.Path
	}
	return ""
}

func analysisEndpoint(byID map[graph.NodeID]graph.Node, owner map[graph.NodeID]graph.NodeID, id graph.NodeID) graph.NodeID {
	// Prefer owning file so import cycles close across file nodes.
	if fid, ok := owner[id]; ok {
		return fid
	}
	if n, ok := byID[id]; ok {
		if n.Kind == parse.KindFile || n.Kind == parse.KindModule {
			return id
		}
	}
	return ""
}

func resolveRelativeTarget(
	spec, fromPath string,
	byID map[graph.NodeID]graph.Node,
	filesByPath, modulesByPath map[string]graph.NodeID,
) (graph.Node, bool) {
	base := "."
	if fromPath != "" {
		base = filepath.Dir(fromPath)
	}
	cleaned := filepath.ToSlash(filepath.Clean(filepath.Join(base, spec)))

	candidates := []string{
		cleaned,
		cleaned + ".js",
		cleaned + ".jsx",
		cleaned + ".ts",
		cleaned + ".tsx",
		cleaned + ".mjs",
		cleaned + ".cjs",
		cleaned + "/index.js",
		cleaned + "/index.jsx",
		cleaned + "/index.ts",
		cleaned + "/index.tsx",
		cleaned + ".go",
	}

	for _, cand := range candidates {
		if id, ok := filesByPath[cand]; ok {
			return byID[id], true
		}
		if id, ok := modulesByPath[cand]; ok {
			return byID[id], true
		}
		if n, ok := byID[graph.NodeID("file:"+cand)]; ok && (n.Kind == parse.KindFile || n.Kind == parse.KindModule) {
			return n, true
		}
		if n, ok := byID[graph.NodeID("module:"+cand)]; ok && (n.Kind == parse.KindFile || n.Kind == parse.KindModule) {
			return n, true
		}
	}
	return graph.Node{}, false
}

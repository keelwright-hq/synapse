package report

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/keelwright-hq/synapse/internal/graph"
	"github.com/keelwright-hq/synapse/internal/parse"
)

// Hub is a ranked graph node for human navigation sections.
type Hub struct {
	ID     graph.NodeID
	Kind   string
	Name   string
	Path   string
	Degree int
}

// ImportantFiles ranks file nodes by imports/calls activity attributed to them.
// Parsers attach imports to modules/packages and calls to functions/methods, so
// those edges are rolled up to the owning file via Path / contains ownership.
func ImportantFiles(nodes []graph.Node, edges []graph.Edge, limit int) []Hub {
	byID := map[graph.NodeID]graph.Node{}
	for _, n := range nodes {
		byID[n.ID] = n
	}
	owner := fileOwners(nodes, edges)
	deg := map[graph.NodeID]int{}
	for _, e := range edges {
		if e.Type != parse.EdgeImports && e.Type != parse.EdgeCalls {
			continue
		}
		fromFile, hasFrom := owner[e.From]
		toFile, hasTo := owner[e.To]
		if hasFrom {
			deg[fromFile]++
		}
		if hasTo && (!hasFrom || fromFile != toFile) {
			deg[toFile]++
		}
	}
	return hubsFromDegree(byID, deg, limit, func(n graph.Node) bool {
		return n.Kind == parse.KindFile
	})
}

// ImportantSymbols ranks resolved symbols (functions, methods, types, contracts)
// by undirected degree over calls / implements / consumes. Unresolved
// kind=symbol nodes and container kinds are excluded.
func ImportantSymbols(nodes []graph.Node, edges []graph.Edge, limit int) []Hub {
	return rankHubs(nodes, edges, limit, func(e graph.Edge) bool {
		switch e.Type {
		case parse.EdgeCalls, parse.EdgeImplements, parse.EdgeConsumes:
			return true
		default:
			return false
		}
	}, isImportantSymbolKind)
}

func isImportantSymbolKind(n graph.Node) bool {
	switch n.Kind {
	case parse.KindFunction, parse.KindMethod, parse.KindType,
		parse.KindOperation, parse.KindService, parse.KindSchema, parse.KindField:
		return true
	default:
		return false
	}
}

// TopImports ranks external/local dependency specs by how many distinct files
// import them. Per-file import nodes (import:path#spec) are grouped by a
// normalized target: package names stay as-is, relative specs are resolved
// against the importing file directory.
func TopImports(nodes []graph.Node, edges []graph.Edge, limit int) []Hub {
	byID := map[graph.NodeID]graph.Node{}
	for _, n := range nodes {
		byID[n.ID] = n
	}
	owner := fileOwners(nodes, edges)
	// target -> set of importing file paths
	importers := map[string]map[string]struct{}{}
	for _, e := range edges {
		if e.Type != parse.EdgeImports {
			continue
		}
		to, ok := byID[e.To]
		if !ok {
			continue
		}
		spec := to.Name
		if spec == "" {
			spec = string(to.ID)
		}
		importerPath := ""
		if from, ok := byID[e.From]; ok && from.Path != "" {
			importerPath = from.Path
		} else if fid, ok := owner[e.From]; ok {
			if f, ok := byID[fid]; ok {
				importerPath = f.Path
			}
		}
		if importerPath == "" && to.Path != "" {
			importerPath = to.Path
		}
		key := NormalizeImportTarget(importerPath, spec)
		if key == "" {
			continue
		}
		fileKey := importerPath
		if fid, ok := owner[e.From]; ok {
			if f, ok := byID[fid]; ok && f.Path != "" {
				fileKey = f.Path
			}
		} else if to.Path != "" {
			fileKey = to.Path
		}
		if fileKey == "" {
			fileKey = string(e.From)
		}
		set, ok := importers[key]
		if !ok {
			set = map[string]struct{}{}
			importers[key] = set
		}
		set[fileKey] = struct{}{}
	}

	var hubs []Hub
	for key, files := range importers {
		hubs = append(hubs, Hub{
			ID:     graph.NodeID("dep:" + key),
			Kind:   parse.KindImport,
			Name:   key,
			Degree: len(files),
		})
	}
	sort.Slice(hubs, func(i, j int) bool {
		if hubs[i].Degree != hubs[j].Degree {
			return hubs[i].Degree > hubs[j].Degree
		}
		return hubs[i].Name < hubs[j].Name
	})
	if limit > 0 && len(hubs) > limit {
		hubs = hubs[:limit]
	}
	return hubs
}

// NormalizeImportTarget groups import specs for ranking.
// Relative paths are resolved against the importing file; bare package names
// are kept (scoped npm packages reduced to @scope/name).
func NormalizeImportTarget(importerPath, spec string) string {
	spec = strings.TrimSpace(spec)
	spec = strings.Trim(spec, `"'`)
	if spec == "" {
		return ""
	}
	if strings.HasPrefix(spec, ".") {
		base := "."
		if importerPath != "" {
			base = filepath.Dir(importerPath)
		}
		return filepath.ToSlash(filepath.Clean(filepath.Join(base, spec)))
	}
	if strings.HasPrefix(spec, "/") {
		return filepath.ToSlash(filepath.Clean(spec))
	}
	// Scoped package: @org/pkg[/...]
	if strings.HasPrefix(spec, "@") {
		parts := strings.Split(spec, "/")
		if len(parts) >= 2 {
			return parts[0] + "/" + parts[1]
		}
		return spec
	}
	// Go-style module paths keep the full import path.
	if strings.Contains(spec, ".") && strings.Contains(spec, "/") {
		return spec
	}
	// npm-style subpath: lodash/fp → lodash
	if i := strings.IndexByte(spec, '/'); i > 0 {
		return spec[:i]
	}
	return spec
}

// fileOwners maps each node ID to its owning file node ID.
func fileOwners(nodes []graph.Node, edges []graph.Edge) map[graph.NodeID]graph.NodeID {
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
			// Also index bare file:path IDs.
			if strings.HasPrefix(string(n.ID), "file:") {
				filesByPath[strings.TrimPrefix(string(n.ID), "file:")] = n.ID
			}
		}
	}
	// Parent via contains: child <- parent
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
			return "", false
		}
		visited[id] = true
		defer func() { visited[id] = false }()
		n, ok := byID[id]
		if !ok {
			return "", false
		}
		if n.Path != "" {
			if fid, ok := filesByPath[n.Path]; ok {
				owner[id] = fid
				return fid, true
			}
			// Synthesize lookup for file:path even if missing from map.
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

func rankHubs(
	nodes []graph.Node,
	edges []graph.Edge,
	limit int,
	edgeOK func(graph.Edge) bool,
	nodeOK func(graph.Node) bool,
) []Hub {
	byID := map[graph.NodeID]graph.Node{}
	for _, n := range nodes {
		byID[n.ID] = n
	}
	deg := map[graph.NodeID]int{}
	for _, e := range edges {
		if !edgeOK(e) {
			continue
		}
		deg[e.From]++
		deg[e.To]++
	}
	return hubsFromDegree(byID, deg, limit, nodeOK)
}

func hubsFromDegree(
	byID map[graph.NodeID]graph.Node,
	deg map[graph.NodeID]int,
	limit int,
	nodeOK func(graph.Node) bool,
) []Hub {
	var hubs []Hub
	for id, d := range deg {
		if d == 0 {
			continue
		}
		n, ok := byID[id]
		if !ok {
			continue
		}
		if nodeOK != nil && !nodeOK(n) {
			continue
		}
		hubs = append(hubs, Hub{
			ID:     id,
			Kind:   n.Kind,
			Name:   n.Name,
			Path:   n.Path,
			Degree: d,
		})
	}
	sort.Slice(hubs, func(i, j int) bool {
		if hubs[i].Degree != hubs[j].Degree {
			return hubs[i].Degree > hubs[j].Degree
		}
		return hubs[i].ID < hubs[j].ID
	})
	if limit > 0 && len(hubs) > limit {
		hubs = hubs[:limit]
	}
	return hubs
}

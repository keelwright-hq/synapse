package rank

import (
	"fmt"
	"sort"

	"github.com/keelwright-hq/synapse/internal/graph"
	"github.com/keelwright-hq/synapse/internal/parse"
)

// dependencyEdges are the static and runtime edges that mean "uses": a change
// to the target can affect the source.
var dependencyEdges = []graph.EdgeType{
	parse.EdgeCalls,
	parse.EdgeReferences,
	parse.EdgeInherits,
	parse.EdgeImplements,
	parse.EdgeConsumes,
	parse.EdgeObservedCalls,
}

// FileGroup lists the symbols in one file reached by an impact or dependency query.
type FileGroup struct {
	Path    string      `json:"path"`
	Depth   int         `json:"depth"`
	Symbols []SymbolRef `json:"symbols"`
}

// SymbolRef is one symbol and the edge that connected it.
type SymbolRef struct {
	ID    graph.NodeID `json:"id"`
	Name  string       `json:"name"`
	Kind  string       `json:"kind"`
	Edge  string       `json:"edge"`
	Line  int          `json:"line,omitempty"`
	Depth int          `json:"depth"`
}

// ImpactResult is the file-grouped answer to an affected or dependencies query.
type ImpactResult struct {
	Seed       graph.NodeID `json:"seed"`
	Direction  string       `json:"direction"`
	Files      []FileGroup  `json:"files"`
	Symbols    int          `json:"symbols"`     // symbols in the returned files
	TotalFiles int          `json:"total_files"` // files before --limit
	Truncated  bool         `json:"truncated,omitempty"`
}

// Affected walks dependency edges backwards from seed: everything that calls,
// references, inherits from or implements it, up to depth hops. Every type on
// the walk includes its members (also those declared in Swift extensions in
// other files), so callers of a type's methods count as affected.
func Affected(store graph.Store, seed graph.NodeID, depth, limit int) (ImpactResult, error) {
	return walkDependencies(store, seed, depth, limit, "in")
}

// Dependencies walks dependency edges forwards from seed (and a type's
// members): everything it calls, references, inherits from or implements.
func Dependencies(store graph.Store, seed graph.NodeID, depth, limit int) (ImpactResult, error) {
	return walkDependencies(store, seed, depth, limit, "out")
}

func walkDependencies(store graph.Store, seed graph.NodeID, depth, limit int, dir string) (ImpactResult, error) {
	if depth <= 0 {
		depth = 1
	}
	seedNode, err := store.GetNode(seed)
	if err != nil {
		return ImpactResult{}, err
	}
	mx, err := newMemberIndex(store)
	if err != nil {
		return ImpactResult{}, err
	}
	own, err := mx.withMembers(seedNode)
	if err != nil {
		return ImpactResult{}, err
	}
	isOwn := map[graph.NodeID]bool{}
	for _, n := range own {
		isOwn[n.ID] = true
	}

	reached := map[graph.NodeID]SymbolRef{}
	frontier := own
	for d := 1; d <= depth && len(frontier) > 0; d++ {
		var next []graph.Node
		var sources []graph.Node
		for _, cur := range frontier {
			// A type reached mid-walk also stands for its members.
			expanded, err := mx.withMembers(cur)
			if err != nil {
				return ImpactResult{}, err
			}
			sources = append(sources, expanded...)
		}
		for _, cur := range sources {
			for _, et := range dependencyEdges {
				var edges []graph.Edge
				if dir == "in" {
					edges, err = store.InEdges(cur.ID, et)
				} else {
					edges, err = store.OutEdges(cur.ID, et)
				}
				if err != nil {
					return ImpactResult{}, err
				}
				for _, e := range edges {
					other := e.From
					if dir == "out" {
						other = e.To
					}
					if isOwn[other] {
						continue
					}
					if _, seen := reached[other]; seen {
						continue
					}
					n, err := store.GetNode(other)
					if err != nil {
						if err == graph.ErrNotFound {
							continue
						}
						return ImpactResult{}, err
					}
					// Unresolved placeholders have no file to report.
					if n.Kind == parse.KindSymbol || n.Kind == parse.KindImport || n.Path == "" {
						continue
					}
					reached[other] = SymbolRef{
						ID: n.ID, Name: symbolLabel(n), Kind: n.Kind, Edge: string(et),
						Line: propInt(n.Props, "start_line"), Depth: d,
					}
					next = append(next, n)
				}
			}
		}
		frontier = next
	}

	byPath := map[string]*FileGroup{}
	for _, ref := range reached {
		n, _ := store.GetNode(ref.ID)
		g := byPath[n.Path]
		if g == nil {
			g = &FileGroup{Path: n.Path, Depth: ref.Depth}
			byPath[n.Path] = g
		}
		if ref.Depth < g.Depth {
			g.Depth = ref.Depth
		}
		g.Symbols = append(g.Symbols, ref)
	}
	res := ImpactResult{Seed: seed, Direction: dir}
	for _, g := range byPath {
		sort.Slice(g.Symbols, func(i, j int) bool {
			if g.Symbols[i].Line != g.Symbols[j].Line {
				return g.Symbols[i].Line < g.Symbols[j].Line
			}
			return g.Symbols[i].ID < g.Symbols[j].ID
		})
		res.Files = append(res.Files, *g)
	}
	sort.Slice(res.Files, func(i, j int) bool {
		if res.Files[i].Depth != res.Files[j].Depth {
			return res.Files[i].Depth < res.Files[j].Depth
		}
		return res.Files[i].Path < res.Files[j].Path
	})
	res.TotalFiles = len(res.Files)
	if limit > 0 && len(res.Files) > limit {
		res.Files = res.Files[:limit]
		res.Truncated = true
	}
	for _, g := range res.Files {
		res.Symbols += len(g.Symbols)
	}
	return res, nil
}

// memberIndex finds a type's members: what it contains, plus, when the name
// has exactly one declaration, the Swift extensions of that name and what
// they contain (extension methods hang off the extension file's type node).
type memberIndex struct {
	store      graph.Store
	decls      map[string][]graph.NodeID
	extensions map[string][]graph.NodeID
}

func newMemberIndex(store graph.Store) (*memberIndex, error) {
	mx := &memberIndex{store: store, decls: map[string][]graph.NodeID{}, extensions: map[string][]graph.NodeID{}}
	err := store.ForEachNode(func(n graph.Node) bool {
		if n.Kind != parse.KindType || n.Name == "" {
			return true
		}
		if n.Props[parse.PropExtension] != "" {
			mx.extensions[n.Name] = append(mx.extensions[n.Name], n.ID)
		} else {
			mx.decls[n.Name] = append(mx.decls[n.Name], n.ID)
		}
		return true
	})
	return mx, err
}

// withMembers returns n plus, for a type, its members and its extensions.
func (mx *memberIndex) withMembers(n graph.Node) ([]graph.Node, error) {
	out := []graph.Node{n}
	if n.Kind != parse.KindType {
		return out, nil
	}
	containers := []graph.NodeID{n.ID}
	if len(mx.decls[n.Name]) == 1 {
		for _, id := range append(append([]graph.NodeID{}, mx.decls[n.Name]...), mx.extensions[n.Name]...) {
			if id == n.ID {
				continue
			}
			if m, err := mx.store.GetNode(id); err == nil {
				out = append(out, m)
				containers = append(containers, id)
			}
		}
	}
	for _, c := range containers {
		edges, err := mx.store.OutEdges(c, parse.EdgeContains)
		if err != nil {
			return nil, err
		}
		for _, e := range edges {
			if m, err := mx.store.GetNode(e.To); err == nil {
				out = append(out, m)
			}
		}
	}
	return out, nil
}

// PathStep is one hop of a path result.
type PathStep struct {
	ID   graph.NodeID `json:"id"`
	Name string       `json:"name"`
	Kind string       `json:"kind"`
	Path string       `json:"path,omitempty"`
	Line int          `json:"line,omitempty"`
	Edge string       `json:"edge,omitempty"` // edge taken to reach this step
}

// PathResult is the shortest directed dependency path between two symbols.
type PathResult struct {
	From  graph.NodeID `json:"from"`
	To    graph.NodeID `json:"to"`
	Steps []PathStep   `json:"steps"`
}

// ShortestPath finds the fewest-hop path from `from` to `to` along dependency
// edges in their own direction. A type may step into its members (a contains
// hop, counted like any other), and the search also ends on any member of a
// type target, so "A reaches Repository" means A reaches one of Repository's
// methods. It never climbs from a member up to its owner type, which would
// turn "lives in the same type" into a path.
func ShortestPath(store graph.Store, from, to graph.NodeID, maxDepth int) (PathResult, error) {
	if maxDepth <= 0 {
		maxDepth = 8
	}
	if _, err := store.GetNode(from); err != nil {
		return PathResult{}, err
	}
	target, err := store.GetNode(to)
	if err != nil {
		return PathResult{}, err
	}
	mx, err := newMemberIndex(store)
	if err != nil {
		return PathResult{}, err
	}
	goal := map[graph.NodeID]bool{to: true}
	members, err := mx.withMembers(target)
	if err != nil {
		return PathResult{}, err
	}
	for _, m := range members {
		goal[m.ID] = true
	}

	type hop struct {
		prev graph.NodeID
		edge string
	}
	prev := map[graph.NodeID]hop{from: {}}
	frontier := []graph.NodeID{from}
	var found graph.NodeID
	if goal[from] {
		// Already there: from is the target or one of its members.
		found = from
	}
	for d := 0; d < maxDepth && found == "" && len(frontier) > 0; d++ {
		var next []graph.NodeID
		for _, cur := range frontier {
			n, err := store.GetNode(cur)
			if err != nil {
				continue
			}
			var edges []graph.Edge
			for _, et := range dependencyEdges {
				es, err := store.OutEdges(cur, et)
				if err != nil {
					return PathResult{}, err
				}
				edges = append(edges, es...)
			}
			if n.Kind == parse.KindType {
				// Step into members, including those declared in extensions.
				members, err := mx.withMembers(n)
				if err != nil {
					return PathResult{}, err
				}
				for _, m := range members[1:] {
					edges = append(edges, graph.Edge{From: cur, To: m.ID, Type: parse.EdgeContains})
				}
			}
			sort.Slice(edges, func(i, j int) bool {
				if edges[i].To != edges[j].To {
					return edges[i].To < edges[j].To
				}
				return edges[i].Type < edges[j].Type
			})
			for _, e := range edges {
				if _, seen := prev[e.To]; seen {
					continue
				}
				prev[e.To] = hop{prev: cur, edge: string(e.Type)}
				if goal[e.To] && e.To != from {
					found = e.To
					break
				}
				next = append(next, e.To)
			}
			if found != "" {
				break
			}
		}
		frontier = next
	}
	if found == "" {
		return PathResult{}, fmt.Errorf("%w: no dependency path from %s to %s within %d hops", graph.ErrNotFound, from, to, maxDepth)
	}
	var chain []graph.NodeID
	for id := found; ; id = prev[id].prev {
		chain = append([]graph.NodeID{id}, chain...)
		if id == from {
			break
		}
	}
	res := PathResult{From: from, To: to}
	for _, id := range chain {
		n, err := store.GetNode(id)
		if err != nil {
			return PathResult{}, err
		}
		res.Steps = append(res.Steps, PathStep{
			ID: id, Name: symbolLabel(n), Kind: n.Kind, Path: n.Path,
			Line: propInt(n.Props, "start_line"), Edge: prev[id].edge,
		})
	}
	return res, nil
}

// symbolLabel names a method as Type.method when its ID carries the receiver.
func symbolLabel(n graph.Node) string {
	if n.Kind != parse.KindMethod {
		return n.Name
	}
	id := string(n.ID)
	for i := len(id) - 1; i >= 0; i-- {
		if id[i] == '#' {
			sym := id[i+1:]
			for j := 0; j < len(sym); j++ {
				if sym[j] == '(' {
					sym = sym[:j]
					break
				}
			}
			return sym
		}
	}
	return n.Name
}

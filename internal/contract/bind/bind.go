// Package bind links code symbols to contract operations (OpenAPI / GraphQL / gRPC)
// via best-effort heuristics.
package bind

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/keelwright-hq/synapse/internal/graph"
	"github.com/keelwright-hq/synapse/internal/parse"
	"github.com/keelwright-hq/synapse/internal/uri"
)

// Member is one indexed workspace repo available to the binder.
type Member struct {
	Name  string
	Root  string // filesystem root for re-reading sources
	Store graph.Store
}

// Options configure Bind.
type Options struct {
	Members []Member
	// Overlay holds cross-repo implements/consumes edges (IDs = repo:// URIs).
	// Nil is fine for single-repo same-store binding only.
	Overlay graph.Store
}

type operationRef struct {
	node    graph.Node
	repo    string
	store   graph.Store
	opID    string // props.operation_id
	path    string // props.path or props.grpc_path
	method  string
	gqlRoot string // props.gql_root (Query|Mutation|Subscription)
	service string // props.service (gRPC)
	repoURI string
}

type symbolRef struct {
	node    graph.Node
	repo    string
	store   graph.Store
	root    string
	repoURI string
	fold    string // folded name for matching
}

var quotedStringRe = regexp.MustCompile(`["'\x60]([^"'\x60\\]|\\.)*["'\x60]`)

// Match reasons stored on implements/consumes edge props["match"] (SYN-17).
const (
	MatchOperationID = "operation_id"
	MatchPathLiteral = "path_literal"
)

// Bind clears the overlay (if any), then writes implements/consumes edges.
func Bind(opts Options) error {
	if opts.Overlay != nil {
		if err := clearStore(opts.Overlay); err != nil {
			return err
		}
	}

	var ops []operationRef
	var syms []symbolRef
	for _, m := range opts.Members {
		err := m.Store.ForEachNode(func(n graph.Node) bool {
			switch n.Kind {
			case parse.KindOperation:
				op := operationRef{
					node:    n,
					repo:    m.Name,
					store:   m.Store,
					repoURI: n.Props[uri.PropKey],
				}
				if n.Props != nil {
					op.opID = n.Props["operation_id"]
					op.path = n.Props["path"]
					if op.path == "" {
						op.path = n.Props["grpc_path"]
					}
					op.method = n.Props["method"]
					op.gqlRoot = n.Props["gql_root"]
					op.service = n.Props["service"]
				}
				ops = append(ops, op)
			case parse.KindFunction, parse.KindMethod:
				syms = append(syms, symbolRef{
					node:    n,
					repo:    m.Name,
					store:   m.Store,
					root:    m.Root,
					repoURI: n.Props[uri.PropKey],
					fold:    foldName(symbolMatchName(n)),
				})
			}
			return true
		})
		if err != nil {
			return err
		}
	}

	type edgeKey struct {
		from, to graph.NodeID
		typ      graph.EdgeType
	}
	seen := map[edgeKey]struct{}{}

	put := func(fromRepo string, fromStore graph.Store, from graph.Node, toRepo string, to graph.Node, typ graph.EdgeType, match string) error {
		key := edgeKey{from: from.ID, to: to.ID, typ: typ}
		if _, ok := seen[key]; ok {
			return nil
		}
		seen[key] = struct{}{}

		var edgeProps map[string]string
		if match != "" {
			edgeProps = map[string]string{"match": match}
		}

		if fromRepo == toRepo {
			return fromStore.PutEdge(graph.Edge{From: from.ID, To: to.ID, Type: typ, Props: edgeProps})
		}
		if opts.Overlay == nil {
			return nil
		}
		fromURI := from.Props[uri.PropKey]
		toURI := to.Props[uri.PropKey]
		if fromURI == "" || toURI == "" {
			return nil
		}
		if err := opts.Overlay.PutNode(graph.Node{
			ID:    graph.NodeID(fromURI),
			Kind:  from.Kind,
			Name:  from.Name,
			Path:  from.Path,
			Props: map[string]string{uri.PropKey: fromURI},
		}); err != nil {
			return err
		}
		if err := opts.Overlay.PutNode(graph.Node{
			ID:    graph.NodeID(toURI),
			Kind:  to.Kind,
			Name:  to.Name,
			Path:  to.Path,
			Props: map[string]string{uri.PropKey: toURI},
		}); err != nil {
			return err
		}
		return opts.Overlay.PutEdge(graph.Edge{
			From:  graph.NodeID(fromURI),
			To:    graph.NodeID(toURI),
			Type:  typ,
			Props: edgeProps,
		})
	}

	// 1) operationId ↔ symbol name (plus GraphQL resolver name variants):
	// same repo → implements; other repo → consumes.
	for _, op := range ops {
		if op.opID == "" {
			continue
		}
		wants := operationNameFolds(op)
		for _, sym := range syms {
			if !foldMatches(sym.fold, wants) {
				continue
			}
			if sym.repo == op.repo {
				if err := put(sym.repo, sym.store, sym.node, op.repo, op.node, parse.EdgeImplements, MatchOperationID); err != nil {
					return err
				}
			} else {
				if err := put(sym.repo, sym.store, sym.node, op.repo, op.node, parse.EdgeConsumes, MatchOperationID); err != nil {
					return err
				}
			}
		}
	}

	// 2) Path string literals in source → consumes from enclosing function/method.
	pathOps := make(map[string][]operationRef)
	for _, op := range ops {
		if op.path == "" {
			continue
		}
		pathOps[normalizeAPIPath(op.path)] = append(pathOps[normalizeAPIPath(op.path)], op)
	}
	if len(pathOps) == 0 {
		return nil
	}

	// Group symbols by file for one read.
	type fileKey struct {
		repo, path, root string
		store            graph.Store
	}
	byFile := map[fileKey][]symbolRef{}
	for _, sym := range syms {
		if sym.node.Path == "" || sym.root == "" {
			continue
		}
		k := fileKey{repo: sym.repo, path: sym.node.Path, root: sym.root, store: sym.store}
		byFile[k] = append(byFile[k], sym)
	}

	for fk, fileSyms := range byFile {
		abs := filepath.Join(fk.root, filepath.FromSlash(fk.path))
		data, err := os.ReadFile(abs)
		if err != nil {
			continue
		}
		type hit struct {
			path string
			line int
		}
		var hits []hit
		for _, m := range quotedStringRe.FindAllStringSubmatchIndex(string(data), -1) {
			if len(m) < 2 {
				continue
			}
			raw := string(data[m[0]:m[1]])
			lit := unquoteLiteral(raw)
			norm := normalizeAPIPath(lit)
			if _, ok := pathOps[norm]; !ok {
				// Also try with leading slash if missing.
				if !strings.HasPrefix(norm, "/") {
					norm = "/" + strings.TrimPrefix(norm, "/")
				}
				if _, ok := pathOps[norm]; !ok {
					continue
				}
			}
			line := 1 + bytes.Count(data[:m[0]], []byte("\n"))
			hits = append(hits, hit{path: norm, line: line})
		}
		for _, h := range hits {
			opsForPath := pathOps[h.path]
			for _, sym := range fileSyms {
				if !lineInSpan(sym.node, h.line) {
					continue
				}
				for _, op := range opsForPath {
					if err := put(sym.repo, sym.store, sym.node, op.repo, op.node, parse.EdgeConsumes, MatchPathLiteral); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

func clearStore(s graph.Store) error {
	var ids []graph.NodeID
	err := s.ForEachNode(func(n graph.Node) bool {
		ids = append(ids, n.ID)
		return true
	})
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := s.DeleteNode(id); err != nil && err != graph.ErrNotFound {
			return err
		}
	}
	return nil
}

func symbolMatchName(n graph.Node) string {
	if n.Kind == parse.KindMethod {
		// Prefer short method name after receiver.
		if i := strings.LastIndex(n.Name, "."); i >= 0 && i+1 < len(n.Name) {
			return n.Name[i+1:]
		}
	}
	return n.Name
}

// operationNameFolds returns folded candidate names that may implement/consume op.
func operationNameFolds(op operationRef) []string {
	field := op.opID
	seen := map[string]struct{}{}
	var out []string
	add := func(s string) {
		f := foldName(s)
		if f == "" {
			return
		}
		if _, ok := seen[f]; ok {
			return
		}
		seen[f] = struct{}{}
		out = append(out, f)
	}
	add(field)
	if op.gqlRoot != "" {
		add("Resolve" + field)
		add("Get" + field)
		add(op.gqlRoot + "_" + field)
	}
	if op.service != "" {
		add(op.service + "_" + field)
		add(op.service + field)
	}
	return out
}

func foldMatches(symFold string, wants []string) bool {
	for _, w := range wants {
		if symFold == w {
			return true
		}
	}
	return false
}

func foldName(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

func normalizeAPIPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	// Strip query/fragment if present in literals.
	if i := strings.IndexAny(p, "?#"); i >= 0 {
		p = p[:i]
	}
	return p
}

func unquoteLiteral(s string) string {
	if len(s) < 2 {
		return s
	}
	return s[1 : len(s)-1]
}

func lineInSpan(n graph.Node, line int) bool {
	if n.Props == nil {
		return true // no span → attribute to whole-file symbols
	}
	start, ok1 := parseIntProp(n.Props["start_line"])
	end, ok2 := parseIntProp(n.Props["end_line"])
	if !ok1 || !ok2 {
		return true
	}
	return line >= start && line <= end
}

func parseIntProp(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return n, true
}

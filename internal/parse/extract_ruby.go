package parse

import (
	"path/filepath"
	"strings"

	"github.com/keelwright-hq/synapse/internal/graph"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func extractRuby(path string, src []byte, root *tree_sitter.Node) Result {
	b := newBuilder(path, src)
	fid := fileID(path)
	b.put(graph.Node{ID: fid, Kind: KindFile, Name: path, Path: path})

	modName := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	mid := moduleID(path)
	b.put(graph.Node{ID: mid, Kind: KindModule, Name: modName, Path: path})
	b.edge(fid, mid, EdgeContains)

	walkRuby(b, root, mid, "")
	return b.result("ruby")
}

func walkRuby(b *builder, n *tree_sitter.Node, module, current graph.NodeID) {
	if n == nil {
		return
	}

	switch n.Kind() {
	case "module", "class":
		name := rubyName(b, n)
		if name != "" {
			id := typeID(b.path, name)
			b.putSpan(n, graph.Node{ID: id, Kind: KindType, Name: name, Path: b.path})
			b.edge(module, id, EdgeContains)
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			walkRuby(b, n.NamedChild(i), module, current)
		}
		return

	case "method", "singleton_method":
		name := rubyName(b, n)
		if name != "" {
			recv := containingRubyType(b, n)
			var id graph.NodeID
			kind := KindFunction
			if recv != "" {
				id = methodID(b.path, recv, name)
				kind = KindMethod
			} else {
				id = funcID(b.path, name)
			}
			b.putSpan(n, graph.Node{ID: id, Kind: kind, Name: name, Path: b.path})
			b.edge(module, id, EdgeContains)
			walkRuby(b, field(n, "body"), module, id)
			return
		}

	case "call":
		method := field(n, "method")
		methodName := ""
		if method != nil {
			methodName = b.text(method)
		}
		if isRubyRequire(methodName) {
			if spec := rubyRequireSpec(b, n); spec != "" {
				iid := importID(b.path, spec)
				b.put(graph.Node{ID: iid, Kind: KindImport, Name: spec, Path: b.path})
				b.edge(module, iid, EdgeImports)
			}
		} else if current != "" && methodName != "" {
			sid := symbolID(methodName)
			b.put(graph.Node{ID: sid, Kind: KindSymbol, Name: methodName})
			b.edge(current, sid, EdgeCalls)
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			walkRuby(b, n.NamedChild(i), module, current)
		}
		return
	}

	for i := uint(0); i < n.NamedChildCount(); i++ {
		walkRuby(b, n.NamedChild(i), module, current)
	}
}

func rubyName(b *builder, n *tree_sitter.Node) string {
	if nameNode := field(n, "name"); nameNode != nil {
		return b.text(nameNode)
	}
	return ""
}

func containingRubyType(b *builder, n *tree_sitter.Node) string {
	for p := n.Parent(); p != nil; p = p.Parent() {
		if p.Kind() == "method" || p.Kind() == "singleton_method" {
			return ""
		}
		if p.Kind() == "class" || p.Kind() == "module" {
			if name := rubyName(b, p); name != "" {
				return name
			}
			return ""
		}
	}
	return ""
}

func isRubyRequire(name string) bool {
	switch name {
	case "require", "require_relative", "load", "autoload":
		return true
	default:
		return false
	}
}

func rubyRequireSpec(b *builder, call *tree_sitter.Node) string {
	args := field(call, "arguments")
	if args == nil {
		return ""
	}
	for i := uint(0); i < args.NamedChildCount(); i++ {
		c := args.NamedChild(i)
		if c != nil && c.Kind() == "string" {
			return unquote(b.text(c))
		}
	}
	return ""
}

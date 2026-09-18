package parse

import (
	"path/filepath"
	"strings"

	"github.com/keelwright-hq/synapse/internal/graph"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func extractPHP(path string, src []byte, root *tree_sitter.Node) Result {
	b := newBuilder(path, src)
	fid := fileID(path)
	b.put(graph.Node{ID: fid, Kind: KindFile, Name: path, Path: path})

	modName := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	mid := moduleID(path)
	b.put(graph.Node{ID: mid, Kind: KindModule, Name: modName, Path: path})
	b.edge(fid, mid, EdgeContains)

	walkPHP(b, root, mid, "")
	return b.result("php")
}

func walkPHP(b *builder, n *tree_sitter.Node, module, current graph.NodeID) {
	if n == nil {
		return
	}

	switch n.Kind() {
	case "namespace_definition":
		if name := field(n, "name"); name != nil {
			spec := b.text(name)
			id := packageID(b.path, spec)
			b.put(graph.Node{ID: id, Kind: KindPackage, Name: spec, Path: b.path})
			b.edge(module, id, EdgeContains)
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			walkPHP(b, n.NamedChild(i), module, current)
		}
		return

	case "namespace_use_declaration":
		extractPHPUse(b, n, module)
		return

	case "class_declaration", "interface_declaration", "trait_declaration", "enum_declaration":
		if nameNode := field(n, "name"); nameNode != nil {
			name := b.text(nameNode)
			id := typeID(b.path, name)
			b.putSpan(n, graph.Node{ID: id, Kind: KindType, Name: name, Path: b.path})
			b.edge(module, id, EdgeContains)
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			walkPHP(b, n.NamedChild(i), module, current)
		}
		return

	case "method_declaration":
		if nameNode := field(n, "name"); nameNode != nil {
			name := b.text(nameNode)
			recv := containingPHPType(b, n)
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
			walkPHP(b, field(n, "body"), module, id)
			return
		}

	case "function_definition":
		if nameNode := field(n, "name"); nameNode != nil {
			name := b.text(nameNode)
			id := funcID(b.path, name)
			b.putSpan(n, graph.Node{ID: id, Kind: KindFunction, Name: name, Path: b.path})
			b.edge(module, id, EdgeContains)
			walkPHP(b, field(n, "body"), module, id)
			return
		}

	case "function_call_expression":
		if current != "" {
			fn := field(n, "function")
			name := ""
			if fn != nil {
				name = b.text(fn)
			}
			if name != "" {
				sid := symbolID(name)
				b.put(graph.Node{ID: sid, Kind: KindSymbol, Name: name})
				b.edge(current, sid, EdgeCalls)
			}
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			walkPHP(b, n.NamedChild(i), module, current)
		}
		return
	}

	for i := uint(0); i < n.NamedChildCount(); i++ {
		walkPHP(b, n.NamedChild(i), module, current)
	}
}

func containingPHPType(b *builder, n *tree_sitter.Node) string {
	for p := n.Parent(); p != nil; p = p.Parent() {
		if p.Kind() == "method_declaration" || p.Kind() == "function_definition" {
			return ""
		}
		switch p.Kind() {
		case "class_declaration", "interface_declaration", "trait_declaration", "enum_declaration":
			if nameNode := field(p, "name"); nameNode != nil {
				return b.text(nameNode)
			}
			return ""
		}
	}
	return ""
}

func extractPHPUse(b *builder, n *tree_sitter.Node, parent graph.NodeID) {
	seen := map[string]struct{}{}
	walkNamed(n, func(c *tree_sitter.Node) {
		if c.Kind() != "qualified_name" {
			return
		}
		spec := strings.TrimSpace(b.text(c))
		if spec == "" {
			return
		}
		if _, ok := seen[spec]; ok {
			return
		}
		seen[spec] = struct{}{}
		iid := importID(b.path, spec)
		b.put(graph.Node{ID: iid, Kind: KindImport, Name: spec, Path: b.path})
		b.edge(parent, iid, EdgeImports)
	})
}

package parse

import (
	"path/filepath"
	"strings"

	"github.com/taricsa/synapse/internal/graph"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func extractTypeScript(path string, src []byte, root *tree_sitter.Node) Result {
	b := newBuilder(path, src)
	fid := fileID(path)
	b.put(graph.Node{ID: fid, Kind: KindFile, Name: path, Path: path})

	modName := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	mid := moduleID(path)
	b.put(graph.Node{ID: mid, Kind: KindModule, Name: modName, Path: path})
	b.edge(fid, mid, EdgeContains)

	walkTS(b, root, mid, "")
	return b.result("typescript")
}

func walkTS(b *builder, n *tree_sitter.Node, module, current graph.NodeID) {
	if n == nil {
		return
	}

	switch n.Kind() {
	case "import_statement":
		extractTSImport(b, n, module)
		return

	case "function_declaration":
		nameNode := field(n, "name")
		if nameNode != nil {
			name := b.text(nameNode)
			id := funcID(b.path, name)
			b.putSpan(n, graph.Node{ID: id, Kind: KindFunction, Name: name, Path: b.path})
			b.edge(module, id, EdgeContains)
			walkTS(b, field(n, "body"), module, id)
			return
		}

	case "method_definition":
		nameNode := field(n, "name")
		if nameNode != nil {
			name := b.text(nameNode)
			id := methodID(b.path, containingTSClassName(b, n), name)
			b.putSpan(n, graph.Node{ID: id, Kind: KindMethod, Name: name, Path: b.path})
			b.edge(module, id, EdgeContains)
			walkTS(b, field(n, "body"), module, id)
			return
		}

	case "class_declaration", "interface_declaration", "type_alias_declaration", "enum_declaration":
		nameNode := field(n, "name")
		if nameNode != nil {
			name := b.text(nameNode)
			id := typeID(b.path, name)
			b.putSpan(n, graph.Node{ID: id, Kind: KindType, Name: name, Path: b.path})
			b.edge(module, id, EdgeContains)
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			walkTS(b, n.NamedChild(i), module, current)
		}
		return

	case "lexical_declaration", "variable_declaration":
		for i := uint(0); i < n.NamedChildCount(); i++ {
			decl := n.NamedChild(i)
			if decl == nil || decl.Kind() != "variable_declarator" {
				walkTS(b, decl, module, current)
				continue
			}
			nameNode := field(decl, "name")
			value := field(decl, "value")
			if nameNode != nil && value != nil &&
				(value.Kind() == "arrow_function" || value.Kind() == "function_expression") {
				name := b.text(nameNode)
				id := funcID(b.path, name)
				b.putSpan(value, graph.Node{ID: id, Kind: KindFunction, Name: name, Path: b.path})
				b.edge(module, id, EdgeContains)
				walkTS(b, field(value, "body"), module, id)
				continue
			}
			walkTS(b, decl, module, current)
		}
		return

	case "call_expression":
		if current != "" {
			fn := field(n, "function")
			name := calleeName(b, fn)
			if name != "" {
				sid := symbolID(name)
				b.put(graph.Node{ID: sid, Kind: KindSymbol, Name: name})
				b.edge(current, sid, EdgeCalls)
			}
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			walkTS(b, n.NamedChild(i), module, current)
		}
		return
	}

	for i := uint(0); i < n.NamedChildCount(); i++ {
		walkTS(b, n.NamedChild(i), module, current)
	}
}

func containingTSClassName(b *builder, n *tree_sitter.Node) string {
	for p := n.Parent(); p != nil; p = p.Parent() {
		if p.Kind() != "class_declaration" {
			continue
		}
		if nameNode := field(p, "name"); nameNode != nil {
			return b.text(nameNode)
		}
		break
	}
	return "class"
}

func extractTSImport(b *builder, n *tree_sitter.Node, parent graph.NodeID) {
	var spec string
	walkNamed(n, func(c *tree_sitter.Node) {
		if c.Kind() == "string" {
			spec = unquote(b.text(c))
		}
	})
	if spec == "" {
		return
	}
	iid := importID(b.path, spec)
	b.put(graph.Node{ID: iid, Kind: KindImport, Name: spec, Path: b.path})
	b.edge(parent, iid, EdgeImports)
}

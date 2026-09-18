package parse

import (
	"path/filepath"
	"strings"

	"github.com/keelwright-hq/synapse/internal/graph"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func extractC(path string, src []byte, root *tree_sitter.Node) Result {
	return extractCFamily(path, src, root, "c")
}

func extractCpp(path string, src []byte, root *tree_sitter.Node) Result {
	return extractCFamily(path, src, root, "cpp")
}

func extractCFamily(path string, src []byte, root *tree_sitter.Node, lang string) Result {
	b := newBuilder(path, src)
	fid := fileID(path)
	b.put(graph.Node{ID: fid, Kind: KindFile, Name: path, Path: path})

	modName := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	mid := moduleID(path)
	b.put(graph.Node{ID: mid, Kind: KindModule, Name: modName, Path: path})
	b.edge(fid, mid, EdgeContains)

	walkCFamily(b, root, mid, "")
	return b.result(lang)
}

func walkCFamily(b *builder, n *tree_sitter.Node, module, current graph.NodeID) {
	if n == nil {
		return
	}

	switch n.Kind() {
	case "preproc_include":
		if pathNode := field(n, "path"); pathNode != nil {
			spec := strings.Trim(b.text(pathNode), `<>"`)
			if spec != "" {
				iid := importID(b.path, spec)
				b.put(graph.Node{ID: iid, Kind: KindImport, Name: spec, Path: b.path})
				b.edge(module, iid, EdgeImports)
			}
		}
		return

	case "namespace_definition":
		if nameNode := field(n, "name"); nameNode != nil {
			spec := b.text(nameNode)
			id := packageID(b.path, spec)
			b.put(graph.Node{ID: id, Kind: KindPackage, Name: spec, Path: b.path})
			b.edge(module, id, EdgeContains)
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			walkCFamily(b, n.NamedChild(i), module, current)
		}
		return

	case "struct_specifier", "union_specifier", "enum_specifier", "class_specifier":
		if nameNode := field(n, "name"); nameNode != nil {
			name := b.text(nameNode)
			id := typeID(b.path, name)
			b.putSpan(n, graph.Node{ID: id, Kind: KindType, Name: name, Path: b.path})
			b.edge(module, id, EdgeContains)
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			walkCFamily(b, n.NamedChild(i), module, current)
		}
		return

	case "function_definition":
		name, recv := cFunctionName(b, n)
		if name != "" {
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
			walkCFamily(b, field(n, "body"), module, id)
			return
		}

	case "call_expression":
		if current != "" {
			fn := field(n, "function")
			name := calleeName(b, fn)
			if name == "" && fn != nil {
				name = b.text(fn)
			}
			if name != "" {
				sid := symbolID(name)
				b.put(graph.Node{ID: sid, Kind: KindSymbol, Name: name})
				b.edge(current, sid, EdgeCalls)
			}
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			walkCFamily(b, n.NamedChild(i), module, current)
		}
		return
	}

	for i := uint(0); i < n.NamedChildCount(); i++ {
		walkCFamily(b, n.NamedChild(i), module, current)
	}
}

func cFunctionName(b *builder, def *tree_sitter.Node) (name, recv string) {
	decl := field(def, "declarator")
	fnDecl := findFunctionDeclarator(decl)
	if fnDecl == nil {
		return "", ""
	}
	inner := field(fnDecl, "declarator")
	if inner == nil {
		return "", ""
	}
	switch inner.Kind() {
	case "identifier", "field_identifier", "destructor_name", "operator_name":
		name = b.text(inner)
	default:
		// pointer/reference wrappers — dig for identifier
		walkNamed(inner, func(c *tree_sitter.Node) {
			if name != "" {
				return
			}
			switch c.Kind() {
			case "identifier", "field_identifier":
				name = b.text(c)
			}
		})
	}
	if name == "" {
		return "", ""
	}
	recv = containingCType(b, def)
	return name, recv
}

func findFunctionDeclarator(n *tree_sitter.Node) *tree_sitter.Node {
	if n == nil {
		return nil
	}
	if n.Kind() == "function_declarator" {
		return n
	}
	for i := uint(0); i < n.NamedChildCount(); i++ {
		if found := findFunctionDeclarator(n.NamedChild(i)); found != nil {
			return found
		}
	}
	return nil
}

func containingCType(b *builder, n *tree_sitter.Node) string {
	for p := n.Parent(); p != nil; p = p.Parent() {
		if p.Kind() == "function_definition" {
			return ""
		}
		switch p.Kind() {
		case "class_specifier", "struct_specifier", "union_specifier":
			if nameNode := field(p, "name"); nameNode != nil {
				return b.text(nameNode)
			}
			return ""
		}
	}
	return ""
}

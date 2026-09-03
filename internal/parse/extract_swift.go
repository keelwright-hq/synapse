package parse

import (
	"path/filepath"
	"strings"

	"github.com/taricsa/synapse/internal/graph"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func extractSwift(path string, src []byte, root *tree_sitter.Node) Result {
	b := newBuilder(path, src)
	fid := fileID(path)
	b.put(graph.Node{ID: fid, Kind: KindFile, Name: path, Path: path})

	modName := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	mid := moduleID(path)
	b.put(graph.Node{ID: mid, Kind: KindModule, Name: modName, Path: path})
	b.edge(fid, mid, EdgeContains)

	walkSwift(b, root, mid, "")
	return b.result("swift")
}

func walkSwift(b *builder, n *tree_sitter.Node, module, current graph.NodeID) {
	if n == nil {
		return
	}

	switch n.Kind() {
	case "import_declaration":
		extractSwiftImport(b, n, module)
		return

	case "function_declaration", "init_declaration":
		name := swiftDeclName(b, n)
		if name == "" && n.Kind() == "init_declaration" {
			name = "init"
		}
		if name != "" {
			recv := containingSwiftType(b, n)
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
			walkSwift(b, field(n, "body"), module, id)
			return
		}

	case "class_declaration", "struct_declaration", "enum_declaration", "protocol_declaration", "actor_declaration":
		name := swiftDeclName(b, n)
		if name != "" {
			id := typeID(b.path, name)
			b.putSpan(n, graph.Node{ID: id, Kind: KindType, Name: name, Path: b.path})
			b.edge(module, id, EdgeContains)
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			walkSwift(b, n.NamedChild(i), module, current)
		}
		return

	case "call_expression":
		if current != "" {
			fn := field(n, "function")
			if fn == nil && n.NamedChildCount() > 0 {
				fn = n.NamedChild(0)
			}
			name := calleeName(b, fn)
			if name != "" {
				sid := symbolID(name)
				b.put(graph.Node{ID: sid, Kind: KindSymbol, Name: name})
				b.edge(current, sid, EdgeCalls)
			}
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			walkSwift(b, n.NamedChild(i), module, current)
		}
		return
	}

	for i := uint(0); i < n.NamedChildCount(); i++ {
		walkSwift(b, n.NamedChild(i), module, current)
	}
}

func swiftDeclName(b *builder, n *tree_sitter.Node) string {
	if nameNode := field(n, "name"); nameNode != nil {
		return b.text(nameNode)
	}
	for i := uint(0); i < n.NamedChildCount(); i++ {
		c := n.NamedChild(i)
		if c == nil {
			continue
		}
		switch c.Kind() {
		case "simple_identifier", "type_identifier", "identifier":
			return b.text(c)
		}
	}
	return ""
}

func containingSwiftType(b *builder, n *tree_sitter.Node) string {
	for p := n.Parent(); p != nil; p = p.Parent() {
		if p.Kind() == "function_declaration" || p.Kind() == "init_declaration" {
			// Nested local function, not a type method.
			return ""
		}
		switch p.Kind() {
		case "class_declaration", "struct_declaration", "enum_declaration", "protocol_declaration", "actor_declaration", "extension_declaration":
			if name := swiftDeclName(b, p); name != "" {
				return name
			}
			return "type"
		}
	}
	return ""
}

func extractSwiftImport(b *builder, n *tree_sitter.Node, parent graph.NodeID) {
	var spec string
	walkNamed(n, func(c *tree_sitter.Node) {
		if spec != "" {
			return
		}
		switch c.Kind() {
		case "identifier", "simple_identifier", "type_identifier":
			spec = b.text(c)
		}
	})
	if spec == "" {
		return
	}
	iid := importID(b.path, spec)
	b.put(graph.Node{ID: iid, Kind: KindImport, Name: spec, Path: b.path})
	b.edge(parent, iid, EdgeImports)
}

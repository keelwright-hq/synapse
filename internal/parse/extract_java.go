package parse

import (
	"path/filepath"
	"strings"

	"github.com/keelwright-hq/synapse/internal/graph"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func extractJava(path string, src []byte, root *tree_sitter.Node) Result {
	b := newBuilder(path, src)
	fid := fileID(path)
	b.put(graph.Node{ID: fid, Kind: KindFile, Name: path, Path: path})

	modName := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	mid := moduleID(path)
	b.put(graph.Node{ID: mid, Kind: KindModule, Name: modName, Path: path})
	b.edge(fid, mid, EdgeContains)

	walkJava(b, root, mid, "")
	return b.result("java")
}

func walkJava(b *builder, n *tree_sitter.Node, module, current graph.NodeID) {
	if n == nil {
		return
	}

	switch n.Kind() {
	case "package_declaration":
		extractJavaPackageImport(b, n, module, true)
		return

	case "import_declaration":
		extractJavaPackageImport(b, n, module, false)
		return

	case "class_declaration", "interface_declaration", "enum_declaration", "record_declaration", "annotation_type_declaration":
		nameNode := field(n, "name")
		if nameNode != nil {
			name := b.text(nameNode)
			id := typeID(b.path, name)
			b.putSpan(n, graph.Node{ID: id, Kind: KindType, Name: name, Path: b.path})
			b.edge(module, id, EdgeContains)
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			walkJava(b, n.NamedChild(i), module, current)
		}
		return

	case "method_declaration", "constructor_declaration":
		nameNode := field(n, "name")
		name := ""
		if nameNode != nil {
			name = b.text(nameNode)
		}
		if name == "" && n.Kind() == "constructor_declaration" {
			name = containingJavaType(b, n)
		}
		if name != "" {
			recv := containingJavaType(b, n)
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
			walkJava(b, field(n, "body"), module, id)
			return
		}

	case "method_invocation":
		if current != "" {
			name := javaCalleeName(b, n)
			if name != "" {
				sid := symbolID(name)
				b.put(graph.Node{ID: sid, Kind: KindSymbol, Name: name})
				b.edge(current, sid, EdgeCalls)
			}
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			walkJava(b, n.NamedChild(i), module, current)
		}
		return

	case "object_creation_expression":
		if current != "" {
			if typ := field(n, "type"); typ != nil {
				name := b.text(typ)
				if name != "" {
					sid := symbolID(name)
					b.put(graph.Node{ID: sid, Kind: KindSymbol, Name: name})
					b.edge(current, sid, EdgeCalls)
				}
			}
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			walkJava(b, n.NamedChild(i), module, current)
		}
		return
	}

	for i := uint(0); i < n.NamedChildCount(); i++ {
		walkJava(b, n.NamedChild(i), module, current)
	}
}

func javaCalleeName(b *builder, invocation *tree_sitter.Node) string {
	if nameNode := field(invocation, "name"); nameNode != nil {
		return b.text(nameNode)
	}
	return ""
}

func containingJavaType(b *builder, n *tree_sitter.Node) string {
	for p := n.Parent(); p != nil; p = p.Parent() {
		switch p.Kind() {
		case "class_declaration", "interface_declaration", "enum_declaration", "record_declaration", "annotation_type_declaration":
			if nameNode := field(p, "name"); nameNode != nil {
				return b.text(nameNode)
			}
			return ""
		}
	}
	return ""
}

func extractJavaPackageImport(b *builder, n *tree_sitter.Node, parent graph.NodeID, isPackage bool) {
	var spec string
	for i := uint(0); i < n.NamedChildCount(); i++ {
		c := n.NamedChild(i)
		if c == nil {
			continue
		}
		switch c.Kind() {
		case "scoped_identifier", "identifier":
			spec = b.text(c)
		}
	}
	if spec == "" {
		raw := strings.TrimSpace(b.text(n))
		raw = strings.TrimSuffix(raw, ";")
		if isPackage {
			spec = strings.TrimSpace(strings.TrimPrefix(raw, "package"))
		} else {
			raw = strings.TrimSpace(strings.TrimPrefix(raw, "import"))
			raw = strings.TrimSpace(strings.TrimPrefix(raw, "static"))
			spec = strings.TrimSuffix(strings.TrimSpace(raw), ".*")
		}
	}
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return
	}
	if isPackage {
		id := packageID(b.path, spec)
		b.put(graph.Node{ID: id, Kind: KindPackage, Name: spec, Path: b.path})
		b.edge(parent, id, EdgeContains)
		return
	}
	iid := importID(b.path, spec)
	b.put(graph.Node{ID: iid, Kind: KindImport, Name: spec, Path: b.path})
	b.edge(parent, iid, EdgeImports)
}

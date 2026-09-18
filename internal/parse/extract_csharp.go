package parse

import (
	"path/filepath"
	"strings"

	"github.com/keelwright-hq/synapse/internal/graph"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func extractCSharp(path string, src []byte, root *tree_sitter.Node) Result {
	b := newBuilder(path, src)
	fid := fileID(path)
	b.put(graph.Node{ID: fid, Kind: KindFile, Name: path, Path: path})

	modName := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	mid := moduleID(path)
	b.put(graph.Node{ID: mid, Kind: KindModule, Name: modName, Path: path})
	b.edge(fid, mid, EdgeContains)

	walkCSharp(b, root, mid, "")
	return b.result("csharp")
}

func walkCSharp(b *builder, n *tree_sitter.Node, module, current graph.NodeID) {
	if n == nil {
		return
	}

	switch n.Kind() {
	case "using_directive":
		extractCSharpUsing(b, n, module)
		return

	case "namespace_declaration", "file_scoped_namespace_declaration":
		if nameNode := field(n, "name"); nameNode != nil {
			spec := b.text(nameNode)
			id := packageID(b.path, spec)
			b.put(graph.Node{ID: id, Kind: KindPackage, Name: spec, Path: b.path})
			b.edge(module, id, EdgeContains)
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			walkCSharp(b, n.NamedChild(i), module, current)
		}
		return

	case "class_declaration", "interface_declaration", "struct_declaration", "enum_declaration", "record_declaration":
		if nameNode := field(n, "name"); nameNode != nil {
			name := b.text(nameNode)
			id := typeID(b.path, name)
			b.putSpan(n, graph.Node{ID: id, Kind: KindType, Name: name, Path: b.path})
			b.edge(module, id, EdgeContains)
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			walkCSharp(b, n.NamedChild(i), module, current)
		}
		return

	case "method_declaration", "constructor_declaration":
		nameNode := field(n, "name")
		name := ""
		if nameNode != nil {
			name = b.text(nameNode)
		}
		if name == "" && n.Kind() == "constructor_declaration" {
			name = containingCSharpType(b, n)
		}
		if name != "" {
			recv := containingCSharpType(b, n)
			sig := csharpParamSig(b, n)
			var id graph.NodeID
			kind := KindFunction
			if recv != "" {
				id = methodIDWithParams(b.path, recv, name, sig)
				kind = KindMethod
			} else {
				id = funcIDWithParams(b.path, name, sig)
			}
			b.putSpan(n, graph.Node{ID: id, Kind: kind, Name: name, Path: b.path})
			b.edge(module, id, EdgeContains)
			walkCSharp(b, field(n, "body"), module, id)
			return
		}

	case "invocation_expression":
		if current != "" {
			fn := field(n, "function")
			name := calleeName(b, fn)
			if name == "" && fn != nil {
				if fn.Kind() == "member_access_expression" {
					if nameNode := field(fn, "name"); nameNode != nil {
						name = b.text(nameNode)
					}
				} else {
					name = b.text(fn)
				}
			}
			if name != "" {
				sid := symbolID(name)
				b.put(graph.Node{ID: sid, Kind: KindSymbol, Name: name})
				b.edge(current, sid, EdgeCalls)
			}
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			walkCSharp(b, n.NamedChild(i), module, current)
		}
		return
	}

	for i := uint(0); i < n.NamedChildCount(); i++ {
		walkCSharp(b, n.NamedChild(i), module, current)
	}
}

func containingCSharpType(b *builder, n *tree_sitter.Node) string {
	for p := n.Parent(); p != nil; p = p.Parent() {
		if p.Kind() == "method_declaration" || p.Kind() == "constructor_declaration" {
			return ""
		}
		switch p.Kind() {
		case "class_declaration", "interface_declaration", "struct_declaration", "enum_declaration", "record_declaration":
			if nameNode := field(p, "name"); nameNode != nil {
				return b.text(nameNode)
			}
			return ""
		}
	}
	return ""
}

func extractCSharpUsing(b *builder, n *tree_sitter.Node, parent graph.NodeID) {
	var spec string
	for i := uint(0); i < n.NamedChildCount(); i++ {
		c := n.NamedChild(i)
		if c == nil {
			continue
		}
		switch c.Kind() {
		case "identifier", "qualified_name", "name", "alias_qualified_name":
			spec = b.text(c)
		}
	}
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return
	}
	iid := importID(b.path, spec)
	b.put(graph.Node{ID: iid, Kind: KindImport, Name: spec, Path: b.path})
	b.edge(parent, iid, EdgeImports)
}

func csharpParamSig(b *builder, decl *tree_sitter.Node) string {
	params := field(decl, "parameters")
	if params == nil {
		return ""
	}
	var parts []string
	for i := uint(0); i < params.NamedChildCount(); i++ {
		c := params.NamedChild(i)
		if c == nil || c.Kind() != "parameter" {
			continue
		}
		if typ := field(c, "type"); typ != nil {
			parts = append(parts, compactType(b.text(typ)))
		}
	}
	return strings.Join(parts, ",")
}

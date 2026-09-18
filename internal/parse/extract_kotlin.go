package parse

import (
	"path/filepath"
	"strings"

	"github.com/keelwright-hq/synapse/internal/graph"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func extractKotlin(path string, src []byte, root *tree_sitter.Node) Result {
	b := newBuilder(path, src)
	fid := fileID(path)
	b.put(graph.Node{ID: fid, Kind: KindFile, Name: path, Path: path})

	modName := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	mid := moduleID(path)
	b.put(graph.Node{ID: mid, Kind: KindModule, Name: modName, Path: path})
	b.edge(fid, mid, EdgeContains)

	walkKotlin(b, root, mid, "")
	return b.result("kotlin")
}

func walkKotlin(b *builder, n *tree_sitter.Node, module, current graph.NodeID) {
	if n == nil {
		return
	}

	switch n.Kind() {
	case "package_header":
		extractKotlinPackage(b, n, module)
		return

	case "import_header":
		extractKotlinImport(b, n, module)
		return

	case "class_declaration", "object_declaration", "companion_object":
		name := kotlinDeclName(b, n)
		if name == "" && n.Kind() == "companion_object" {
			name = "Companion"
		}
		if name != "" {
			id := typeID(b.path, name)
			b.putSpan(n, graph.Node{ID: id, Kind: KindType, Name: name, Path: b.path})
			b.edge(module, id, EdgeContains)
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			walkKotlin(b, n.NamedChild(i), module, current)
		}
		return

	case "function_declaration", "secondary_constructor":
		name := kotlinDeclName(b, n)
		if name == "" && n.Kind() == "secondary_constructor" {
			name = "constructor"
		}
		if name != "" {
			recv := containingKotlinType(b, n)
			sig := kotlinParamSig(b, n)
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
			body := field(n, "body")
			if body == nil {
				for i := uint(0); i < n.NamedChildCount(); i++ {
					c := n.NamedChild(i)
					if c != nil && c.Kind() == "function_body" {
						body = c
						break
					}
				}
			}
			walkKotlin(b, body, module, id)
			return
		}

	case "call_expression":
		if current != "" {
			fn := field(n, "function")
			if fn == nil && n.NamedChildCount() > 0 {
				fn = n.NamedChild(0)
			}
			name := calleeName(b, fn)
			if name == "" {
				name = kotlinCalleeName(b, fn)
			}
			if name != "" {
				sid := symbolID(name)
				b.put(graph.Node{ID: sid, Kind: KindSymbol, Name: name})
				b.edge(current, sid, EdgeCalls)
			}
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			walkKotlin(b, n.NamedChild(i), module, current)
		}
		return
	}

	for i := uint(0); i < n.NamedChildCount(); i++ {
		walkKotlin(b, n.NamedChild(i), module, current)
	}
}

func kotlinDeclName(b *builder, n *tree_sitter.Node) string {
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

func containingKotlinType(b *builder, n *tree_sitter.Node) string {
	for p := n.Parent(); p != nil; p = p.Parent() {
		if p.Kind() == "function_declaration" || p.Kind() == "secondary_constructor" {
			// Nested local function, not a type method.
			return ""
		}
		switch p.Kind() {
		case "class_declaration", "object_declaration", "companion_object":
			name := kotlinDeclName(b, p)
			if name == "" && p.Kind() == "companion_object" {
				name = "Companion"
			}
			if name != "" {
				return name
			}
			return "type"
		}
	}
	return ""
}

func kotlinCalleeName(b *builder, fn *tree_sitter.Node) string {
	if fn == nil {
		return ""
	}
	switch fn.Kind() {
	case "simple_identifier", "type_identifier", "identifier":
		return b.text(fn)
	case "navigation_expression":
		// Greeter().greet → last navigation_suffix identifier
		var name string
		walkNamed(fn, func(c *tree_sitter.Node) {
			if c.Kind() == "navigation_suffix" {
				for i := uint(0); i < c.NamedChildCount(); i++ {
					ch := c.NamedChild(i)
					if ch != nil && (ch.Kind() == "simple_identifier" || ch.Kind() == "identifier") {
						name = b.text(ch)
					}
				}
			}
		})
		return name
	default:
		return ""
	}
}

func extractKotlinPackage(b *builder, n *tree_sitter.Node, parent graph.NodeID) {
	var spec string
	for i := uint(0); i < n.NamedChildCount(); i++ {
		c := n.NamedChild(i)
		if c != nil && c.Kind() == "identifier" {
			spec = b.text(c)
			break
		}
	}
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return
	}
	id := packageID(b.path, spec)
	b.put(graph.Node{ID: id, Kind: KindPackage, Name: spec, Path: b.path})
	b.edge(parent, id, EdgeContains)
}

func extractKotlinImport(b *builder, n *tree_sitter.Node, parent graph.NodeID) {
	var spec string
	for i := uint(0); i < n.NamedChildCount(); i++ {
		c := n.NamedChild(i)
		if c != nil && c.Kind() == "identifier" {
			spec = b.text(c)
			break
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

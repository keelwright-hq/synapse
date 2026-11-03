package parse

import (
	"path/filepath"
	"strings"

	"github.com/keelwright-hq/synapse/internal/graph"
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
			if recv != "" {
				// Members hang off their type so a type's neighborhood reaches them.
				b.edge(typeID(b.path, recv), id, EdgeContains)
			} else {
				b.edge(module, id, EdgeContains)
			}
			body := field(n, "body")
			for i := uint(0); i < n.NamedChildCount(); i++ {
				if c := n.NamedChild(i); c != nil && !sameNode(c, body) {
					// Parameter and return types are references from the declaration.
					collectSwiftTypeRefs(b, c, id)
				}
			}
			walkSwift(b, body, module, id)
			return
		}

	case "class_declaration", "struct_declaration", "enum_declaration", "protocol_declaration", "actor_declaration":
		name := swiftDeclName(b, n)
		if name != "" {
			id := typeID(b.path, name)
			// tree-sitter-swift parses `extension Foo` as a class_declaration.
			if isSwiftExtension(b, n) {
				// Never let an extension replace the type's own declaration in this file.
				if _, declared := b.nodes[id]; !declared {
					b.putSpan(n, graph.Node{ID: id, Kind: KindType, Name: name, Path: b.path,
						Props: map[string]string{PropExtension: "true"}})
				}
			} else {
				b.putSpan(n, graph.Node{ID: id, Kind: KindType, Name: name, Path: b.path})
			}
			b.edge(module, id, EdgeContains)
			current = id
		}
		nameNode := field(n, "name")
		for i := uint(0); i < n.NamedChildCount(); i++ {
			c := n.NamedChild(i)
			if c == nil || sameNode(c, nameNode) {
				continue
			}
			if c.Kind() == "inheritance_specifier" {
				if current != "" {
					for _, t := range swiftUserTypeNames(b, c) {
						addSwiftListProp(b, current, PropSupertypes, t)
					}
				}
				continue
			}
			// Property types and initializers in the body belong to the type.
			walkSwift(b, c, module, current)
		}
		return

	case "navigation_expression":
		// `WeatherNetworkError.serverError`, `Foo.shared`: a capitalized target is
		// a type used through a static member or enum case.
		if t := field(n, "target"); current != "" && t != nil && t.Kind() == "simple_identifier" {
			if name := b.text(t); name != "" && name[0] >= 'A' && name[0] <= 'Z' {
				addSwiftListProp(b, current, PropTypeRefs, name)
			}
		}

	case "user_type":
		if current != "" {
			collectSwiftTypeRefs(b, n, current)
		}
		return

	case "call_expression":
		if current != "" {
			fn := field(n, "function")
			if fn == nil && n.NamedChildCount() > 0 {
				fn = n.NamedChild(0)
			}
			name := calleeName(b, fn)
			// tree-sitter-swift parses `defer { ... }` as a call with a trailing
			// closure; the statement is not a call, but its body still is walked.
			if name != "" && name != "defer" {
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

// collectSwiftTypeRefs records every type name used under n (annotations,
// generic arguments, casts) as a reference from owner. The index resolves the
// names to type nodes; names that do not resolve never become nodes.
func collectSwiftTypeRefs(b *builder, n *tree_sitter.Node, owner graph.NodeID) {
	for _, t := range swiftUserTypeNames(b, n) {
		addSwiftListProp(b, owner, PropTypeRefs, t)
	}
}

// swiftUserTypeNames returns the type identifiers of every user_type under n,
// including nested generic arguments (`[Foo]`, `Result<Bar, Error>`).
func swiftUserTypeNames(b *builder, n *tree_sitter.Node) []string {
	var out []string
	var walk func(*tree_sitter.Node)
	walk = func(c *tree_sitter.Node) {
		if c == nil {
			return
		}
		if c.Kind() == "user_type" {
			for i := uint(0); i < c.NamedChildCount(); i++ {
				if id := c.NamedChild(i); id != nil && id.Kind() == "type_identifier" {
					out = append(out, b.text(id))
				}
			}
		}
		for i := uint(0); i < c.NamedChildCount(); i++ {
			walk(c.NamedChild(i))
		}
	}
	walk(n)
	return out
}

// addSwiftListProp appends value to a comma-separated, deduplicated prop.
func addSwiftListProp(b *builder, id graph.NodeID, key, value string) {
	node, ok := b.nodes[id]
	if !ok || value == "" {
		return
	}
	if node.Props == nil {
		node.Props = map[string]string{}
	}
	for _, v := range strings.Split(node.Props[key], ",") {
		if v == value {
			return
		}
	}
	if node.Props[key] == "" {
		node.Props[key] = value
	} else {
		node.Props[key] += "," + value
	}
	b.nodes[id] = node
}

func sameNode(a, b *tree_sitter.Node) bool {
	return a != nil && b != nil && a.Kind() == b.Kind() && a.StartByte() == b.StartByte() && a.EndByte() == b.EndByte()
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

func isSwiftExtension(b *builder, n *tree_sitter.Node) bool {
	k := field(n, "declaration_kind")
	return k != nil && b.text(k) == "extension"
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

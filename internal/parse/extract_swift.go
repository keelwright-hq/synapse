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
			b.nest(current, id)
			body := field(n, "body")
			for i := uint(0); i < n.NamedChildCount(); i++ {
				if c := n.NamedChild(i); c != nil && !sameNode(c, body) {
					// Parameter and return types are references from the declaration.
					collectSwiftTypeRefs(b, c, id)
					if c.Kind() == "parameter" {
						recordSwiftVarType(b, id, swiftParamName(b, c), swiftParamType(c))
					}
				}
			}
			walkSwift(b, body, module, id)
			return
		}

	case "protocol_function_declaration":
		// A requirement has no body, but it is what a call through a
		// protocol-typed value (`store.save()`) statically targets.
		name := swiftDeclName(b, n)
		if recv := containingSwiftType(b, n); name != "" && recv != "" {
			id := methodID(b.path, recv, name)
			if _, exists := b.nodes[id]; !exists {
				b.putSpan(n, graph.Node{ID: id, Kind: KindMethod, Name: name, Path: b.path,
					Props: map[string]string{PropRequirement: "true"}})
				b.edge(typeID(b.path, recv), id, EdgeContains)
			}
			collectSwiftTypeRefs(b, n, id)
			return
		}

	case "property_declaration":
		// `let store: any StoreProtocol`, `let cache = Cache()`: remember the
		// type so calls through the name can be resolved. Children are still
		// walked below.
		if owner := b.nodes[current]; owner.Kind == KindType || owner.Kind == KindFunction || owner.Kind == KindMethod {
			name, typ := swiftPropertyNameAndType(b, n)
			recordSwiftVarType(b, current, name, typ)
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
						b.addListProp(current, PropSupertypes, t)
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
				b.addListProp(current, PropTypeRefs, name)
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

// recordSwiftVarType notes on owner that name has the declared type typ.
func recordSwiftVarType(b *builder, owner graph.NodeID, name string, typ *tree_sitter.Node) {
	if name == "" || name == "_" || typ == nil {
		return
	}
	if t := swiftSingleTypeName(b, typ); t != "" {
		b.addListProp(owner, PropVarTypes, name+":"+t)
	}
}

// swiftParamName returns a parameter's internal name (`for id: Int` is `id`).
func swiftParamName(b *builder, p *tree_sitter.Node) string {
	name := ""
	for i := uint(0); i < p.NamedChildCount(); i++ {
		if c := p.NamedChild(i); c != nil && c.Kind() == "simple_identifier" {
			name = b.text(c)
		} else {
			break
		}
	}
	return name
}

// swiftParamType returns a parameter's type node: the first child after its names.
func swiftParamType(p *tree_sitter.Node) *tree_sitter.Node {
	for i := uint(0); i < p.NamedChildCount(); i++ {
		if c := p.NamedChild(i); c != nil && c.Kind() != "simple_identifier" {
			return c
		}
	}
	return nil
}

// swiftPropertyNameAndType returns the single name a property declaration
// binds and where its type comes from: its annotation, or the initializer
// call `Foo(...)` when there is none.
func swiftPropertyNameAndType(b *builder, n *tree_sitter.Node) (string, *tree_sitter.Node) {
	var name string
	var annotation, value *tree_sitter.Node
	for i := uint(0); i < n.NamedChildCount(); i++ {
		c := n.NamedChild(i)
		if c == nil {
			continue
		}
		switch c.Kind() {
		case "pattern":
			if name != "" {
				return "", nil // `let a = 1, b = 2`: not a single binding
			}
			if c.NamedChildCount() == 1 && c.NamedChild(0).Kind() == "simple_identifier" {
				name = b.text(c.NamedChild(0))
			}
		case "type_annotation":
			annotation = c
		case "call_expression":
			value = c
		}
	}
	if annotation != nil {
		return name, annotation
	}
	if value != nil && value.NamedChildCount() > 0 {
		// `Foo()` constructs a Foo; `foo()` and `Foo.make()` say nothing certain.
		if callee := value.NamedChild(0); callee.Kind() == "simple_identifier" {
			if t := b.text(callee); t != "" && t[0] >= 'A' && t[0] <= 'Z' {
				return name, callee
			}
		}
	}
	return name, nil
}

// swiftSingleTypeName returns the one named type a type node stands for,
// seeing through optionals, `any`/`some` and parentheses (`(any Foo)?` is
// Foo). Arrays, dictionaries, closures and tuples of several types give "".
func swiftSingleTypeName(b *builder, n *tree_sitter.Node) string {
	for n != nil {
		switch n.Kind() {
		case "simple_identifier", "type_identifier":
			return b.text(n)
		case "user_type":
			last := ""
			for i := uint(0); i < n.NamedChildCount(); i++ {
				if c := n.NamedChild(i); c != nil && c.Kind() == "type_identifier" {
					last = b.text(c)
				}
			}
			return last
		case "type_annotation", "optional_type", "existential_type", "opaque_type",
			"tuple_type", "tuple_type_item", "implicitly_unwrapped_type":
			if n.NamedChildCount() != 1 {
				return ""
			}
			n = n.NamedChild(0)
		default:
			return ""
		}
	}
	return ""
}

// collectSwiftTypeRefs records every type name used under n (annotations,
// generic arguments, casts) as a reference from owner. The index resolves the
// names to type nodes; names that do not resolve never become nodes.
func collectSwiftTypeRefs(b *builder, n *tree_sitter.Node, owner graph.NodeID) {
	for _, t := range swiftUserTypeNames(b, n) {
		b.addListProp(owner, PropTypeRefs, t)
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

package parse

import (
	"path/filepath"
	"strings"

	"github.com/keelwright-hq/synapse/internal/graph"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func extractJavaScript(path string, src []byte, root *tree_sitter.Node) Result {
	b := newBuilder(path, src)
	fid := fileID(path)
	b.put(graph.Node{ID: fid, Kind: KindFile, Name: path, Path: path})

	modName := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	mid := moduleID(path)
	b.put(graph.Node{ID: mid, Kind: KindModule, Name: modName, Path: path})
	b.edge(fid, mid, EdgeContains)

	walkJS(b, root, mid, "")
	return b.result("javascript")
}

func walkJS(b *builder, n *tree_sitter.Node, module, current graph.NodeID) {
	if n == nil {
		return
	}

	switch n.Kind() {
	case "import_statement", "export_statement":
		if n.Kind() == "import_statement" {
			extractTSImport(b, n, module)
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			walkJS(b, n.NamedChild(i), module, current)
		}
		return

	case "function_declaration", "generator_function_declaration":
		nameNode := field(n, "name")
		if nameNode != nil {
			name := b.text(nameNode)
			id := funcID(b.path, name)
			b.putSpan(n, graph.Node{ID: id, Kind: KindFunction, Name: name, Path: b.path})
			b.edge(module, id, EdgeContains)
			walkJS(b, field(n, "body"), module, id)
			return
		}

	case "method_definition":
		nameNode := field(n, "name")
		if nameNode != nil {
			name := b.text(nameNode)
			id := methodID(b.path, containingTSClassName(b, n), name)
			b.putSpan(n, graph.Node{ID: id, Kind: KindMethod, Name: name, Path: b.path})
			b.edge(module, id, EdgeContains)
			walkJS(b, field(n, "body"), module, id)
			return
		}

	case "class_declaration":
		nameNode := field(n, "name")
		if nameNode != nil {
			name := b.text(nameNode)
			id := typeID(b.path, name)
			b.putSpan(n, graph.Node{ID: id, Kind: KindType, Name: name, Path: b.path})
			b.edge(module, id, EdgeContains)
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			walkJS(b, n.NamedChild(i), module, current)
		}
		return

	case "lexical_declaration", "variable_declaration":
		for i := uint(0); i < n.NamedChildCount(); i++ {
			decl := n.NamedChild(i)
			if decl == nil || decl.Kind() != "variable_declarator" {
				walkJS(b, decl, module, current)
				continue
			}
			nameNode := field(decl, "name")
			value := field(decl, "value")
			if nameNode != nil && value != nil &&
				(value.Kind() == "arrow_function" || value.Kind() == "function_expression" ||
					value.Kind() == "generator_function") {
				name := b.text(nameNode)
				id := funcID(b.path, name)
				b.putSpan(value, graph.Node{ID: id, Kind: KindFunction, Name: name, Path: b.path})
				b.edge(module, id, EdgeContains)
				// Walk the whole function node so default params (e.g. x = bar()) are captured.
				walkJS(b, value, module, id)
				continue
			}
			walkJS(b, decl, module, current)
		}
		return

	case "call_expression":
		fn := field(n, "function")
		if isRequireCall(b, fn) || isImportCall(b, fn) {
			if spec := firstStringArg(b, n); spec != "" {
				iid := importID(b.path, spec)
				b.put(graph.Node{ID: iid, Kind: KindImport, Name: spec, Path: b.path})
				b.edge(module, iid, EdgeImports)
			}
		}
		if current != "" {
			name := calleeName(b, fn)
			if name != "" && name != "require" && name != "import" {
				sid := symbolID(name)
				b.put(graph.Node{ID: sid, Kind: KindSymbol, Name: name})
				b.edge(current, sid, EdgeCalls)
			}
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			walkJS(b, n.NamedChild(i), module, current)
		}
		return
	}

	for i := uint(0); i < n.NamedChildCount(); i++ {
		walkJS(b, n.NamedChild(i), module, current)
	}
}

func isRequireCall(b *builder, fn *tree_sitter.Node) bool {
	if fn == nil {
		return false
	}
	return fn.Kind() == "identifier" && b.text(fn) == "require"
}

func isImportCall(b *builder, fn *tree_sitter.Node) bool {
	if fn == nil {
		return false
	}
	return fn.Kind() == "import" || (fn.Kind() == "identifier" && b.text(fn) == "import")
}

func firstStringArg(b *builder, call *tree_sitter.Node) string {
	args := field(call, "arguments")
	if args == nil {
		return ""
	}
	var spec string
	walkNamed(args, func(c *tree_sitter.Node) {
		if spec == "" && (c.Kind() == "string" || c.Kind() == "string_fragment") {
			spec = unquote(b.text(c))
		}
	})
	return spec
}

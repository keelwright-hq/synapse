package parse

import (
	"path/filepath"
	"strings"

	"github.com/taricsa/synapse/internal/graph"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func extractPython(path string, src []byte, root *tree_sitter.Node) Result {
	b := newBuilder(path, src)
	fid := fileID(path)
	b.put(graph.Node{ID: fid, Kind: KindFile, Name: path, Path: path})

	modName := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	mid := moduleID(path)
	b.put(graph.Node{ID: mid, Kind: KindModule, Name: modName, Path: path})
	b.edge(fid, mid, EdgeContains)

	walkPython(b, root, mid, "")
	return b.result("python")
}

func walkPython(b *builder, n *tree_sitter.Node, module, current graph.NodeID) {
	if n == nil {
		return
	}

	switch n.Kind() {
	case "import_statement", "import_from_statement":
		extractPythonImport(b, n, module)
		return

	case "function_definition":
		nameNode := field(n, "name")
		if nameNode != nil {
			name := b.text(nameNode)
			recv := containingPythonClass(b, n)
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
			walkPython(b, field(n, "body"), module, id)
			return
		}

	case "class_definition":
		nameNode := field(n, "name")
		if nameNode != nil {
			name := b.text(nameNode)
			id := typeID(b.path, name)
			b.putSpan(n, graph.Node{ID: id, Kind: KindType, Name: name, Path: b.path})
			b.edge(module, id, EdgeContains)
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			walkPython(b, n.NamedChild(i), module, current)
		}
		return

	case "call":
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
			walkPython(b, n.NamedChild(i), module, current)
		}
		return
	}

	for i := uint(0); i < n.NamedChildCount(); i++ {
		walkPython(b, n.NamedChild(i), module, current)
	}
}

func containingPythonClass(b *builder, n *tree_sitter.Node) string {
	for p := n.Parent(); p != nil; p = p.Parent() {
		if p.Kind() == "function_definition" {
			// Nested local function, not a class method.
			return ""
		}
		if p.Kind() == "class_definition" {
			if nameNode := field(p, "name"); nameNode != nil {
				return b.text(nameNode)
			}
			return ""
		}
	}
	return ""
}

func extractPythonImport(b *builder, n *tree_sitter.Node, parent graph.NodeID) {
	var specs []string
	if n.Kind() == "import_from_statement" {
		if mod := field(n, "module_name"); mod != nil {
			specs = append(specs, b.text(mod))
		}
	}
	walkNamed(n, func(c *tree_sitter.Node) {
		switch c.Kind() {
		case "dotted_name", "relative_import":
			if n.Kind() == "import_statement" {
				specs = append(specs, b.text(c))
			}
		}
	})
	seen := map[string]struct{}{}
	for _, spec := range specs {
		spec = strings.TrimSpace(spec)
		if spec == "" {
			continue
		}
		if _, ok := seen[spec]; ok {
			continue
		}
		seen[spec] = struct{}{}
		iid := importID(b.path, spec)
		b.put(graph.Node{ID: iid, Kind: KindImport, Name: spec, Path: b.path})
		b.edge(parent, iid, EdgeImports)
	}
}

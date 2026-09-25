package parse

import (
	"strings"

	"github.com/keelwright-hq/synapse/internal/graph"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func extractGo(path string, src []byte, root *tree_sitter.Node) Result {
	b := newBuilder(path, src)
	fid := fileID(path)
	b.put(graph.Node{ID: fid, Kind: KindFile, Name: path, Path: path})

	var pkgID graph.NodeID
	walkGo(b, root, fid, &pkgID, "")
	return b.result("go")
}

// precedingGoDoc returns the contiguous // comment block immediately before n.
func precedingGoDoc(b *builder, n *tree_sitter.Node) string {
	if n == nil {
		return ""
	}
	var lines []string
	prev := n.PrevSibling()
	for prev != nil && prev.Kind() == "comment" {
		t := strings.TrimSpace(b.text(prev))
		if strings.HasPrefix(t, "//") {
			lines = append([]string{strings.TrimSpace(strings.TrimPrefix(t, "//"))}, lines...)
		} else {
			break
		}
		prev = prev.PrevSibling()
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func walkGo(b *builder, n *tree_sitter.Node, file graph.NodeID, pkgID *graph.NodeID, current graph.NodeID) {
	if n == nil {
		return
	}

	switch n.Kind() {
	case "package_clause":
		nameNode := n.NamedChild(0)
		if nameNode != nil {
			name := b.text(nameNode)
			id := packageID(b.path, name)
			*pkgID = id
			b.put(graph.Node{ID: id, Kind: KindPackage, Name: name, Path: b.path})
			b.edge(file, id, EdgeContains)
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			walkGo(b, n.NamedChild(i), file, pkgID, current)
		}
		return

	case "import_spec":
		pathNode := field(n, "path")
		if pathNode == nil && n.NamedChildCount() > 0 {
			pathNode = n.NamedChild(n.NamedChildCount() - 1)
		}
		if pathNode != nil {
			spec := unquote(b.text(pathNode))
			iid := importID(b.path, spec)
			b.put(graph.Node{ID: iid, Kind: KindImport, Name: spec, Path: b.path})
			parent := file
			if *pkgID != "" {
				parent = *pkgID
			}
			b.edge(parent, iid, EdgeImports)
		}
		return

	case "function_declaration":
		nameNode := field(n, "name")
		if nameNode != nil {
			name := b.text(nameNode)
			id := funcID(b.path, name)
			node := graph.Node{ID: id, Kind: KindFunction, Name: name, Path: b.path}
			if doc := precedingGoDoc(b, n); doc != "" {
				node.Props = map[string]string{PropDoc: doc}
			}
			b.putSpan(n, node)
			parent := file
			if *pkgID != "" {
				parent = *pkgID
			}
			b.edge(parent, id, EdgeContains)
			body := field(n, "body")
			walkGo(b, body, file, pkgID, id)
			return
		}

	case "method_declaration":
		nameNode := field(n, "name")
		if nameNode != nil {
			name := b.text(nameNode)
			recv := receiverTypeName(b, field(n, "receiver"))
			id := methodID(b.path, recv, name)
			props := map[string]string{"receiver": recv}
			if doc := precedingGoDoc(b, n); doc != "" {
				props[PropDoc] = doc
			}
			b.putSpan(n, graph.Node{
				ID: id, Kind: KindMethod, Name: name, Path: b.path,
				Props: props,
			})
			parent := file
			if *pkgID != "" {
				parent = *pkgID
			}
			b.edge(parent, id, EdgeContains)
			body := field(n, "body")
			walkGo(b, body, file, pkgID, id)
			return
		}

	case "type_spec":
		nameNode := field(n, "name")
		if nameNode != nil {
			name := b.text(nameNode)
			id := typeID(b.path, name)
			node := graph.Node{ID: id, Kind: KindType, Name: name, Path: b.path}
			// type_spec sits inside type_declaration; look at the declaration for docs.
			decl := n.Parent()
			docNode := n
			if decl != nil && decl.Kind() == "type_declaration" {
				docNode = decl
			}
			if doc := precedingGoDoc(b, docNode); doc != "" {
				node.Props = map[string]string{PropDoc: doc}
			}
			b.putSpan(n, node)
			parent := file
			if *pkgID != "" {
				parent = *pkgID
			}
			b.edge(parent, id, EdgeContains)
		}
		// still walk children for nested types if any
		for i := uint(0); i < n.NamedChildCount(); i++ {
			walkGo(b, n.NamedChild(i), file, pkgID, current)
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
		// walk args for nested calls
		for i := uint(0); i < n.NamedChildCount(); i++ {
			walkGo(b, n.NamedChild(i), file, pkgID, current)
		}
		return
	}

	for i := uint(0); i < n.NamedChildCount(); i++ {
		walkGo(b, n.NamedChild(i), file, pkgID, current)
	}
}

func receiverTypeName(b *builder, recv *tree_sitter.Node) string {
	if recv == nil {
		return "_"
	}
	var typ string
	walkNamed(recv, func(n *tree_sitter.Node) {
		switch n.Kind() {
		case "type_identifier":
			if typ == "" {
				typ = b.text(n)
			}
		}
	})
	if typ == "" {
		return "_"
	}
	return typ
}

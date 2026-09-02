package parse

import (
	"fmt"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	"github.com/taricsa/synapse/internal/graph"
)

type builder struct {
	path  string
	src   []byte
	nodes map[graph.NodeID]graph.Node
	edges []graph.Edge
}

func newBuilder(path string, src []byte) *builder {
	return &builder{
		path:  path,
		src:   src,
		nodes: make(map[graph.NodeID]graph.Node),
	}
}

func (b *builder) text(n *tree_sitter.Node) string {
	if n == nil {
		return ""
	}
	return n.Utf8Text(b.src)
}

func (b *builder) put(n graph.Node) {
	if n.ID == "" {
		return
	}
	if n.Path == "" {
		n.Path = b.path
	}
	b.nodes[n.ID] = n
}

func (b *builder) edge(from, to graph.NodeID, typ graph.EdgeType) {
	if from == "" || to == "" || from == to {
		return
	}
	b.edges = append(b.edges, graph.Edge{From: from, To: to, Type: typ})
}

func (b *builder) result(lang string) Result {
	out := Result{
		Path:  b.path,
		Lang:  lang,
		Edges: b.edges,
	}
	for _, n := range b.nodes {
		out.Nodes = append(out.Nodes, n)
	}
	out.Normalize()
	return out
}

func fileID(path string) graph.NodeID {
	return graph.NodeID("file:" + path)
}

func packageID(path, name string) graph.NodeID {
	return graph.NodeID(fmt.Sprintf("package:%s#%s", path, name))
}

func moduleID(path string) graph.NodeID {
	return graph.NodeID("module:" + path)
}

func funcID(path, name string) graph.NodeID {
	return graph.NodeID(fmt.Sprintf("func:%s#%s", path, name))
}

func methodID(path, recv, name string) graph.NodeID {
	return graph.NodeID(fmt.Sprintf("method:%s#%s.%s", path, recv, name))
}

func typeID(path, name string) graph.NodeID {
	return graph.NodeID(fmt.Sprintf("type:%s#%s", path, name))
}

func importID(path, spec string) graph.NodeID {
	return graph.NodeID(fmt.Sprintf("import:%s#%s", path, spec))
}

func symbolID(name string) graph.NodeID {
	return graph.NodeID("symbol:" + name)
}

func walkNamed(n *tree_sitter.Node, fn func(*tree_sitter.Node)) {
	if n == nil {
		return
	}
	fn(n)
	for i := uint(0); i < n.NamedChildCount(); i++ {
		walkNamed(n.NamedChild(i), fn)
	}
}

func field(n *tree_sitter.Node, name string) *tree_sitter.Node {
	if n == nil {
		return nil
	}
	return n.ChildByFieldName(name)
}

func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') || (s[0] == '`' && s[len(s)-1] == '`') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

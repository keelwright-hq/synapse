package parse

import (
	"fmt"
	"strings"

	"github.com/keelwright-hq/synapse/internal/graph"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
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

func (b *builder) putSpan(ts *tree_sitter.Node, n graph.Node) {
	if ts != nil {
		if n.Props == nil {
			n.Props = map[string]string{}
		}
		n.Props["start_line"] = fmt.Sprintf("%d", ts.StartPosition().Row+1)
		n.Props["end_line"] = fmt.Sprintf("%d", ts.EndPosition().Row+1)
		if n.Props[PropDoc] == "" {
			if doc := extractDocComment(b, ts); doc != "" {
				n.Props[PropDoc] = doc
			}
		}
	}
	b.put(n)
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

// funcIDWithParams identifies a function including a normalized parameter
// signature so JVM overloads do not collapse (e.g. overloaded(Int) vs overloaded(String)).
func funcIDWithParams(path, name, paramSig string) graph.NodeID {
	return graph.NodeID(fmt.Sprintf("func:%s#%s(%s)", path, name, paramSig))
}

func methodID(path, recv, name string) graph.NodeID {
	return graph.NodeID(fmt.Sprintf("method:%s#%s.%s", path, recv, name))
}

// methodIDWithParams identifies a method including a normalized parameter
// signature so JVM overloads and constructors stay distinct.
func methodIDWithParams(path, recv, name, paramSig string) graph.NodeID {
	return graph.NodeID(fmt.Sprintf("method:%s#%s.%s(%s)", path, recv, name, paramSig))
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

func extractDocComment(b *builder, ts *tree_sitter.Node) string {
	if ts == nil {
		return ""
	}
	if doc := extractPythonDocstring(b, ts); doc != "" {
		return doc
	}
	cur := ts
	for cur != nil {
		if doc := precedingComments(b, cur); doc != "" {
			return doc
		}
		p := cur.Parent()
		if p == nil || !isDocWrapperKind(p.Kind()) {
			break
		}
		cur = p
	}
	return ""
}

func isDocWrapperKind(kind string) bool {
	switch kind {
	case "export_statement", "decorated_definition", "type_declaration",
		"variable_declarator", "lexical_declaration", "variable_declaration",
		"type_definition", "template_declaration", "declaration":
		return true
	default:
		return false
	}
}

func extractPythonDocstring(b *builder, ts *tree_sitter.Node) string {
	if ts == nil {
		return ""
	}
	switch ts.Kind() {
	case "function_definition", "class_definition":
	default:
		return ""
	}
	body := field(ts, "body")
	if body == nil || body.Kind() != "block" || body.NamedChildCount() == 0 {
		return ""
	}
	first := body.NamedChild(0)
	if first == nil {
		return ""
	}
	var strNode *tree_sitter.Node
	if first.Kind() == "string" {
		strNode = first
	} else if first.Kind() == "expression_statement" && first.NamedChildCount() > 0 {
		if ch := first.NamedChild(0); ch != nil && ch.Kind() == "string" {
			strNode = ch
		}
	}
	if strNode == nil {
		return ""
	}
	raw := strings.TrimSpace(b.text(strNode))
	for _, prefix := range []string{"r", "u", "b", "f", "R", "U", "B", "F"} {
		if strings.HasPrefix(raw, prefix+`"`) || strings.HasPrefix(raw, prefix+`'`) {
			raw = raw[1:]
			break
		}
	}
	switch {
	case strings.HasPrefix(raw, `"""`) && strings.HasSuffix(raw, `"""`) && len(raw) >= 6:
		raw = raw[3 : len(raw)-3]
	case strings.HasPrefix(raw, `'''`) && strings.HasSuffix(raw, `'''`) && len(raw) >= 6:
		raw = raw[3 : len(raw)-3]
	case len(raw) >= 2 && ((raw[0] == '"' && raw[len(raw)-1] == '"') || (raw[0] == '\'' && raw[len(raw)-1] == '\'')):
		raw = raw[1 : len(raw)-1]
	default:
		return ""
	}
	var lines []string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func precedingComments(b *builder, n *tree_sitter.Node) string {
	if n == nil {
		return ""
	}
	var lines []string
	nextRow := n.StartPosition().Row
	prev := n.PrevSibling()
	for prev != nil {
		if !isCommentNode(prev) {
			break
		}
		if prev.EndPosition().Row+1 < nextRow {
			break
		}
		raw := strings.TrimSpace(b.text(prev))
		if strings.HasPrefix(raw, "/*") {
			if len(lines) == 0 && isDocBlockComment(raw) {
				return cleanBlockComment(raw)
			}
			break
		}
		if cleaned := cleanLineComment(raw); cleaned != "" {
			lines = append([]string{cleaned}, lines...)
		}
		nextRow = prev.StartPosition().Row
		prev = prev.PrevSibling()
	}
	if len(lines) > 0 {
		return strings.TrimSpace(strings.Join(lines, "\n"))
	}

	// Some grammars attach leading comments as initial children of the declaration node.
	var childLines []string
	for i := uint(0); i < n.ChildCount(); i++ {
		ch := n.Child(i)
		if ch == nil {
			continue
		}
		if !isCommentNode(ch) {
			break
		}
		raw := strings.TrimSpace(b.text(ch))
		if strings.HasPrefix(raw, "/*") {
			if isDocBlockComment(raw) {
				return cleanBlockComment(raw)
			}
			break
		}
		if cleaned := cleanLineComment(raw); cleaned != "" {
			childLines = append(childLines, cleaned)
		}
	}
	return strings.TrimSpace(strings.Join(childLines, "\n"))
}

func isDocBlockComment(raw string) bool {
	t := strings.TrimSpace(raw)
	return (strings.HasPrefix(t, "/**") && !strings.HasPrefix(t, "/***")) || strings.HasPrefix(t, "/*!")
}

func isCommentNode(n *tree_sitter.Node) bool {
	if n == nil {
		return false
	}
	return strings.Contains(n.Kind(), "comment")
}

func cleanBlockComment(raw string) string {
	t := strings.TrimSpace(raw)
	switch {
	case strings.HasPrefix(t, "/**"):
		t = strings.TrimPrefix(t, "/**")
	case strings.HasPrefix(t, "/*!"):
		t = strings.TrimPrefix(t, "/*!")
	case strings.HasPrefix(t, "/*"):
		t = strings.TrimPrefix(t, "/*")
	}
	t = strings.TrimSuffix(t, "*/")
	var lines []string
	for _, line := range strings.Split(t, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "*")
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func cleanLineComment(raw string) string {
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		l := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(l, "///"):
			l = strings.TrimPrefix(l, "///")
		case strings.HasPrefix(l, "//!"):
			l = strings.TrimPrefix(l, "//!")
		case strings.HasPrefix(l, "//"):
			l = strings.TrimPrefix(l, "//")
		case strings.HasPrefix(l, "##"):
			l = strings.TrimPrefix(l, "##")
		case strings.HasPrefix(l, "#"):
			l = strings.TrimPrefix(l, "#")
		}
		l = strings.TrimSpace(l)
		if l != "" {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

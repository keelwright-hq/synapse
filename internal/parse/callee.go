package parse

import tree_sitter "github.com/tree-sitter/go-tree-sitter"

func calleeName(b *builder, fn *tree_sitter.Node) string {
	if fn == nil {
		return ""
	}
	switch fn.Kind() {
	case "identifier", "property_identifier", "type_identifier", "simple_identifier":
		return b.text(fn)
	case "selector_expression", "member_expression", "attribute", "navigation_expression", "field_access":
		if f := field(fn, "field"); f != nil {
			return b.text(f)
		}
		if f := field(fn, "property"); f != nil {
			return b.text(f)
		}
		if f := field(fn, "attr"); f != nil {
			return b.text(f)
		}
		if n := fn.NamedChildCount(); n > 0 {
			last := fn.NamedChild(n - 1)
			if last != nil && last.Kind() == "navigation_suffix" {
				for i := uint(0); i < last.NamedChildCount(); i++ {
					ch := last.NamedChild(i)
					if ch != nil && (ch.Kind() == "simple_identifier" || ch.Kind() == "identifier") {
						return b.text(ch)
					}
				}
			}
			return b.text(last)
		}
		return ""
	default:
		return ""
	}
}

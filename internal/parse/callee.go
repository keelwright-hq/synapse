package parse

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func calleeName(b *builder, fn *tree_sitter.Node) string {
	if fn == nil {
		return ""
	}
	switch fn.Kind() {
	case "identifier", "property_identifier", "type_identifier", "simple_identifier", "field_identifier":
		return b.text(fn)
	case "selector_expression", "member_expression", "attribute", "navigation_expression", "field_access", "member_access_expression":
		member := selectorMemberName(b, fn)
		if member == "" {
			return ""
		}
		qual := qualifierName(b, selectorQualifierNode(fn))
		if qual == "" {
			qual = "_"
		}
		return qual + "." + member
	default:
		return ""
	}
}

func selectorMemberName(b *builder, fn *tree_sitter.Node) string {
	if f := field(fn, "field"); f != nil {
		return b.text(f)
	}
	if f := field(fn, "property"); f != nil {
		return b.text(f)
	}
	if f := field(fn, "attr"); f != nil {
		return b.text(f)
	}
	if f := field(fn, "attribute"); f != nil {
		return b.text(f)
	}
	if f := field(fn, "name"); f != nil {
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
}

func selectorQualifierNode(fn *tree_sitter.Node) *tree_sitter.Node {
	for _, key := range []string{"operand", "object", "argument", "expression"} {
		if q := field(fn, key); q != nil {
			return q
		}
	}
	if fn.NamedChildCount() > 1 {
		return fn.NamedChild(0)
	}
	return nil
}

func qualifierName(b *builder, q *tree_sitter.Node) string {
	if q == nil {
		return ""
	}
	switch q.Kind() {
	case "identifier", "property_identifier", "type_identifier", "simple_identifier", "field_identifier",
		"this", "self", "super", "scoped_identifier", "qualified_identifier", "namespace_name":
		return strings.TrimSpace(b.text(q))
	case "selector_expression", "member_expression", "attribute", "navigation_expression", "field_access", "member_access_expression":
		return calleeName(b, q)
	case "call_expression", "call", "method_invocation", "invocation_expression", "function_call_expression":
		if innerFn := field(q, "function"); innerFn != nil {
			if c := calleeName(b, innerFn); c != "" {
				return c
			}
		}
		if q.NamedChildCount() > 0 {
			if c := calleeName(b, q.NamedChild(0)); c != "" {
				return c
			}
		}
		return "_"
	default:
		return "_"
	}
}

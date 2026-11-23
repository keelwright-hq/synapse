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
		"this", "self", "super", "self_expression", "super_expression",
		"scoped_identifier", "qualified_identifier", "namespace_name":
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

// maxWrapperDepth bounds how many nested wrapper calls wrapsFunctionLiteral
// looks through (withAuth(withRateLimit(handler)) is depth 2).
const maxWrapperDepth = 4

// wrapsFunctionLiteral reports whether a JS/TS declarator value is a call that
// wraps a function literal, so the declared name is a function in all but
// syntax:
//
//	export const publishFn = inngest.createFunction({ id: "x" }, async ({ step }) => { ... })
//	export const Button = React.memo((props) => { ... })
//	export const POST = withAuth(withRateLimit(async (req) => { ... }))
//	export const fn = inngest.createFunction(cfg, handler) as AnyFunction
//
// A function literal may be any argument, or the argument of a nested call.
// Type assertions, `satisfies`, `!` and parentheses around a call are looked
// through. Calls to collection and promise methods (rows.map(fn),
// fetch(url).then(fn)) produce values, not functions, and never match.
func wrapsFunctionLiteral(b *builder, value *tree_sitter.Node) bool {
	return wrapsFunctionLiteralDepth(b, value, 0)
}

func wrapsFunctionLiteralDepth(b *builder, n *tree_sitter.Node, depth int) bool {
	n = unwrapTSExpression(n)
	if n == nil || depth >= maxWrapperDepth || n.Kind() != "call_expression" {
		return false
	}
	if fn := field(n, "function"); fn != nil && fn.Kind() == "member_expression" &&
		valueProducingMethods[selectorMemberName(b, fn)] {
		return false
	}
	args := field(n, "arguments")
	if args == nil {
		return false
	}
	for i := uint(0); i < args.NamedChildCount(); i++ {
		a := unwrapTSExpression(args.NamedChild(i))
		if a == nil {
			continue
		}
		switch a.Kind() {
		case "arrow_function", "function_expression", "function", "generator_function":
			return true
		case "call_expression":
			if wrapsFunctionLiteralDepth(b, a, depth+1) {
				return true
			}
		}
	}
	return false
}

// valueProducingMethods are array, promise and string methods whose callback
// argument makes the call's result a value (an array, a promise, a string),
// not a function.
var valueProducingMethods = map[string]bool{
	"map": true, "filter": true, "flatMap": true, "reduce": true, "reduceRight": true,
	"forEach": true, "find": true, "findIndex": true, "findLast": true, "findLastIndex": true,
	"some": true, "every": true, "sort": true, "toSorted": true,
	"then": true, "catch": true, "finally": true,
	"replace": true, "replaceAll": true,
}

// unwrapTSExpression strips wrappers that don't change which value an
// expression evaluates to: `x as T`, `x satisfies T`, `x!` and `(x)`.
func unwrapTSExpression(n *tree_sitter.Node) *tree_sitter.Node {
	for n != nil {
		switch n.Kind() {
		case "as_expression", "satisfies_expression", "non_null_expression", "parenthesized_expression":
			n = n.NamedChild(0)
		default:
			return n
		}
	}
	return nil
}

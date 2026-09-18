package parse

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// compactType normalizes a type text for use in declaration IDs.
func compactType(s string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(s)), "")
}

// javaParamSig returns a comma-separated parameter type list for a Java
// method_declaration or constructor_declaration (e.g. "int,String" or "String...").
func javaParamSig(b *builder, decl *tree_sitter.Node) string {
	params := field(decl, "parameters")
	if params == nil {
		return ""
	}
	var parts []string
	for i := uint(0); i < params.NamedChildCount(); i++ {
		c := params.NamedChild(i)
		if c == nil {
			continue
		}
		switch c.Kind() {
		case "formal_parameter":
			typ := ""
			if t := field(c, "type"); t != nil {
				typ = compactType(b.text(t))
			}
			if dims := field(c, "dimensions"); dims != nil {
				typ += compactType(b.text(dims))
			}
			if typ != "" {
				parts = append(parts, typ)
			}
		case "spread_parameter":
			// Text looks like "String... xs" (optional modifiers).
			raw := b.text(c)
			if idx := strings.Index(raw, "..."); idx >= 0 {
				typ := compactType(raw[:idx]) + "..."
				if typ != "..." {
					parts = append(parts, typ)
				}
			}
		}
	}
	return strings.Join(parts, ",")
}

// kotlinParamSig returns a comma-separated parameter type list for a Kotlin
// function_declaration or secondary_constructor.
func kotlinParamSig(b *builder, decl *tree_sitter.Node) string {
	params := field(decl, "parameters")
	if params == nil {
		return ""
	}
	var parts []string
	for i := uint(0); i < params.NamedChildCount(); i++ {
		c := params.NamedChild(i)
		if c == nil || c.Kind() != "parameter" {
			continue
		}
		var typ string
		for j := uint(0); j < c.NamedChildCount(); j++ {
			ch := c.NamedChild(j)
			if ch == nil {
				continue
			}
			switch ch.Kind() {
			case "user_type", "nullable_type", "function_type", "parenthesized_type", "not_nullable_type":
				typ = compactType(b.text(ch))
			}
		}
		if typ != "" {
			parts = append(parts, typ)
		}
	}
	return strings.Join(parts, ",")
}

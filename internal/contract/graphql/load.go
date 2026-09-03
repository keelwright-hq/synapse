// Package graphql loads GraphQL SDL schemas and maps them into Synapse graph IR.
package graphql

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/parser"
)

// sdlCueRe matches common GraphQL SDL keywords at line start (optional leading spaces).
var sdlCueRe = regexp.MustCompile(`(?m)^\s*(type|schema|extend|interface|enum|input|union|scalar|directive)\b`)

// LooksLikeGraphQL reports whether data appears to be a GraphQL SDL document.
func LooksLikeGraphQL(data []byte) bool {
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	return sdlCueRe.Match(data)
}

// Load reads and parses a GraphQL SDL document from path (syntax only; no schema validation).
func Load(path string) (*ast.SchemaDocument, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return LoadBytes(data, path)
}

// LoadBytes parses GraphQL SDL from data into a SchemaDocument without cross-file
// validation, so split schemas can be indexed file-by-file. name is used as the
// source name in errors.
func LoadBytes(data []byte, name string) (*ast.SchemaDocument, error) {
	if !LooksLikeGraphQL(data) {
		return nil, fmt.Errorf("graphql: not a GraphQL SDL document")
	}
	if name == "" {
		name = "schema.graphql"
	}
	doc, err := parser.ParseSchema(&ast.Source{
		Name:  name,
		Input: string(data),
	})
	if err != nil {
		return nil, fmt.Errorf("graphql: parse: %w", err)
	}
	if doc == nil {
		return nil, fmt.Errorf("graphql: empty schema document")
	}
	return doc, nil
}

// GQLKindProp maps an AST definition kind to the graph props gql_kind value.
func GQLKindProp(kind ast.DefinitionKind) string {
	switch kind {
	case ast.Scalar:
		return "scalar"
	case ast.Object:
		return "object"
	case ast.Interface:
		return "interface"
	case ast.Union:
		return "union"
	case ast.Enum:
		return "enum"
	case ast.InputObject:
		return "input"
	default:
		return strings.ToLower(string(kind))
	}
}

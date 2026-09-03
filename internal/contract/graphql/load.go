// Package graphql loads GraphQL SDL schemas and maps them into Synapse graph IR.
package graphql

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/ast"
)

// sdlCueRe matches common GraphQL SDL keywords at line start (optional leading spaces).
var sdlCueRe = regexp.MustCompile(`(?m)^\s*(type|schema|extend|interface|enum|input|union|scalar|directive)\b`)

// LooksLikeGraphQL reports whether data appears to be a GraphQL SDL document.
func LooksLikeGraphQL(data []byte) bool {
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	return sdlCueRe.Match(data)
}

// Load reads and parses a GraphQL SDL schema from path.
func Load(path string) (*ast.Schema, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return LoadBytes(data, path)
}

// LoadBytes parses GraphQL SDL from data. name is used as the source name in errors.
func LoadBytes(data []byte, name string) (*ast.Schema, error) {
	if !LooksLikeGraphQL(data) {
		return nil, fmt.Errorf("graphql: not a GraphQL SDL document")
	}
	if name == "" {
		name = "schema.graphql"
	}
	schema, err := gqlparser.LoadSchema(&ast.Source{
		Name:  name,
		Input: string(data),
	})
	if err != nil {
		return nil, fmt.Errorf("graphql: load: %w", err)
	}
	if schema == nil {
		return nil, fmt.Errorf("graphql: empty schema")
	}
	return schema, nil
}

// IsBuiltIn reports whether name is a GraphQL built-in scalar (when schema marks BuiltIn).
func IsBuiltIn(def *ast.Definition) bool {
	return def != nil && def.BuiltIn
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

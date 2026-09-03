package graphql

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/vektah/gqlparser/v2/ast"

	"github.com/taricsa/synapse/internal/graph"
	"github.com/taricsa/synapse/internal/parse"
)

// OperationSymbol is the URI/legacy symbol for a root GraphQL field
// (e.g. "query users", "mutation createUser").
func OperationSymbol(root, field string) string {
	return strings.ToLower(root) + " " + field
}

// OperationID builds a Phase-1 node id for a root GraphQL field.
func OperationID(specPath, root, field string) graph.NodeID {
	return graph.NodeID(fmt.Sprintf("operation:%s#%s", specPath, OperationSymbol(root, field)))
}

// TypeID builds a Phase-1 node id for a GraphQL named type.
func TypeID(specPath, name string) graph.NodeID {
	return graph.NodeID(fmt.Sprintf("type:%s#%s", specPath, name))
}

// FieldID builds a Phase-1 node id for a GraphQL field (Type.field).
func FieldID(specPath, typeName, field string) graph.NodeID {
	return graph.NodeID(fmt.Sprintf("field:%s#%s.%s", specPath, typeName, field))
}

// FieldSymbol is the display/URI symbol for a field node.
func FieldSymbol(typeName, field string) string {
	return typeName + "." + field
}

// ToResult maps a GraphQL schema into Synapse parse IR for the given
// repo-relative spec path (slash-separated).
func ToResult(specPath string, schema *ast.Schema) parse.Result {
	specPath = filepath.ToSlash(specPath)
	fid := graph.NodeID("file:" + specPath)
	out := parse.Result{
		Path: specPath,
		Lang: "graphql",
		Nodes: []graph.Node{{
			ID:   fid,
			Kind: parse.KindFile,
			Name: filepath.Base(specPath),
			Path: specPath,
		}},
	}

	rootNames := map[string]string{} // type name → Query|Mutation|Subscription
	if schema.Query != nil {
		rootNames[schema.Query.Name] = "Query"
	}
	if schema.Mutation != nil {
		rootNames[schema.Mutation.Name] = "Mutation"
	}
	if schema.Subscription != nil {
		rootNames[schema.Subscription.Name] = "Subscription"
	}

	for name, def := range schema.Types {
		if def == nil || IsBuiltIn(def) {
			continue
		}
		tid := TypeID(specPath, name)
		out.Nodes = append(out.Nodes, graph.Node{
			ID:   tid,
			Kind: parse.KindType,
			Name: name,
			Path: specPath,
			Props: map[string]string{
				"gql_kind": GQLKindProp(def.Kind),
			},
		})
		out.Edges = append(out.Edges, graph.Edge{
			From: fid,
			To:   tid,
			Type: parse.EdgeContains,
		})

		rootLabel, isRoot := rootNames[name]
		for _, f := range def.Fields {
			if f == nil || f.Name == "" {
				continue
			}
			// Skip introspection fields injected by the GraphQL prelude (__schema, __type, …).
			if strings.HasPrefix(f.Name, "__") {
				continue
			}
			if isRoot {
				sym := OperationSymbol(rootLabel, f.Name)
				oid := OperationID(specPath, rootLabel, f.Name)
				props := map[string]string{
					"operation_id": f.Name,
					"gql_root":     rootLabel,
				}
				if f.Type != nil {
					props["return_type"] = f.Type.String()
				}
				out.Nodes = append(out.Nodes, graph.Node{
					ID:    oid,
					Kind:  parse.KindOperation,
					Name:  sym,
					Path:  specPath,
					Props: props,
				})
				out.Edges = append(out.Edges, graph.Edge{
					From: fid,
					To:   oid,
					Type: parse.EdgeContains,
				})
				continue
			}

			// Non-root fields on object/interface/input types.
			if def.Kind != ast.Object && def.Kind != ast.Interface && def.Kind != ast.InputObject {
				continue
			}
			fsym := FieldSymbol(name, f.Name)
			fidField := FieldID(specPath, name, f.Name)
			props := map[string]string{
				"parent": name,
			}
			if f.Type != nil {
				props["return_type"] = f.Type.String()
			}
			out.Nodes = append(out.Nodes, graph.Node{
				ID:    fidField,
				Kind:  parse.KindField,
				Name:  fsym,
				Path:  specPath,
				Props: props,
			})
			out.Edges = append(out.Edges, graph.Edge{
				From: tid,
				To:   fidField,
				Type: parse.EdgeContains,
			})
		}
	}

	out.Normalize()
	return out
}

// ParseFile loads path and returns graph IR. Relative path is used as Node.Path
// when relPath is non-empty; otherwise filepath.Base of path is used.
func ParseFile(absPath, relPath string) (parse.Result, error) {
	schema, err := Load(absPath)
	if err != nil {
		return parse.Result{}, err
	}
	if relPath == "" {
		relPath = filepath.Base(absPath)
	}
	return ToResult(relPath, schema), nil
}

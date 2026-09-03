package graphql

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/vektah/gqlparser/v2/ast"

	"github.com/keelwright-hq/synapse/internal/graph"
	"github.com/keelwright-hq/synapse/internal/parse"
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

// ToResult maps a GraphQL SchemaDocument into Synapse parse IR for the given
// repo-relative spec path (slash-separated). Uses syntactic parse output only
// (no validated Schema), so partial/split files still emit nodes.
func ToResult(specPath string, doc *ast.SchemaDocument) parse.Result {
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

	types := collectDefinitions(doc)
	rootNames := resolveRootNames(doc, types)

	for name, def := range types {
		if def == nil || name == "" {
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
			// Skip introspection-style names if present in source.
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

// collectDefinitions merges type definitions and same-file extensions into a name map.
func collectDefinitions(doc *ast.SchemaDocument) map[string]*ast.Definition {
	types := make(map[string]*ast.Definition)
	if doc == nil {
		return types
	}
	for _, def := range doc.Definitions {
		if def == nil || def.Name == "" {
			continue
		}
		// Copy so we can append extension fields without mutating the AST.
		cp := *def
		fields := append(ast.FieldList(nil), def.Fields...)
		cp.Fields = fields
		types[def.Name] = &cp
	}
	for _, ext := range doc.Extensions {
		if ext == nil || ext.Name == "" {
			continue
		}
		if existing, ok := types[ext.Name]; ok {
			existing.Fields = append(existing.Fields, ext.Fields...)
			continue
		}
		// Extension of a type defined in another file — still emit what this file contributes.
		cp := *ext
		fields := append(ast.FieldList(nil), ext.Fields...)
		cp.Fields = fields
		types[ext.Name] = &cp
	}
	return types
}

// resolveRootNames maps type names to Query|Mutation|Subscription labels.
func resolveRootNames(doc *ast.SchemaDocument, types map[string]*ast.Definition) map[string]string {
	rootNames := map[string]string{}
	applySchemaRoots := func(list ast.SchemaDefinitionList) {
		for _, sd := range list {
			if sd == nil {
				continue
			}
			for _, ot := range sd.OperationTypes {
				if ot == nil || ot.Type == "" {
					continue
				}
				switch ot.Operation {
				case ast.Query:
					rootNames[ot.Type] = "Query"
				case ast.Mutation:
					rootNames[ot.Type] = "Mutation"
				case ast.Subscription:
					rootNames[ot.Type] = "Subscription"
				}
			}
		}
	}
	if doc != nil {
		applySchemaRoots(doc.Schema)
		applySchemaRoots(doc.SchemaExtension)
	}
	// Default GraphQL root type names when no schema block is present.
	if len(rootNames) == 0 {
		for _, name := range []string{"Query", "Mutation", "Subscription"} {
			if _, ok := types[name]; ok {
				rootNames[name] = name
			}
		}
	}
	return rootNames
}

// ParseFile loads path and returns graph IR. Relative path is used as Node.Path
// when relPath is non-empty; otherwise filepath.Base of path is used.
func ParseFile(absPath, relPath string) (parse.Result, error) {
	doc, err := Load(absPath)
	if err != nil {
		return parse.Result{}, err
	}
	if relPath == "" {
		relPath = filepath.Base(absPath)
	}
	return ToResult(relPath, doc), nil
}

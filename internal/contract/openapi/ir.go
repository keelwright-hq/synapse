package openapi

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/keelwright-hq/synapse/internal/graph"
	"github.com/keelwright-hq/synapse/internal/parse"
)

var httpMethods = []struct {
	name string
	get  func(*openapi3.PathItem) *openapi3.Operation
}{
	{"GET", func(p *openapi3.PathItem) *openapi3.Operation { return p.Get }},
	{"PUT", func(p *openapi3.PathItem) *openapi3.Operation { return p.Put }},
	{"POST", func(p *openapi3.PathItem) *openapi3.Operation { return p.Post }},
	{"DELETE", func(p *openapi3.PathItem) *openapi3.Operation { return p.Delete }},
	{"OPTIONS", func(p *openapi3.PathItem) *openapi3.Operation { return p.Options }},
	{"HEAD", func(p *openapi3.PathItem) *openapi3.Operation { return p.Head }},
	{"PATCH", func(p *openapi3.PathItem) *openapi3.Operation { return p.Patch }},
	{"TRACE", func(p *openapi3.PathItem) *openapi3.Operation { return p.Trace }},
}

// OperationSymbol is the URI/legacy symbol for an HTTP operation (e.g. "GET /users").
func OperationSymbol(method, path string) string {
	return strings.ToUpper(method) + " " + path
}

// OperationID builds a Phase-1 node id for an operation.
func OperationID(specPath, method, path string) graph.NodeID {
	return graph.NodeID(fmt.Sprintf("operation:%s#%s", specPath, OperationSymbol(method, path)))
}

// SchemaID builds a Phase-1 node id for a component schema.
func SchemaID(specPath, name string) graph.NodeID {
	return graph.NodeID(fmt.Sprintf("schema:%s#%s", specPath, name))
}

// ToResult maps an OpenAPI document into Synapse parse IR for the given
// repo-relative spec path (slash-separated).
func ToResult(specPath string, doc *openapi3.T) parse.Result {
	specPath = filepath.ToSlash(specPath)
	fid := graph.NodeID("file:" + specPath)
	out := parse.Result{
		Path: specPath,
		Lang: "openapi",
		Nodes: []graph.Node{{
			ID:   fid,
			Kind: parse.KindFile,
			Name: filepath.Base(specPath),
			Path: specPath,
		}},
	}

	if doc.Paths != nil {
		for path, item := range doc.Paths.Map() {
			if item == nil {
				continue
			}
			for _, m := range httpMethods {
				op := m.get(item)
				if op == nil {
					continue
				}
				sym := OperationSymbol(m.name, path)
				oid := OperationID(specPath, m.name, path)
				props := map[string]string{
					"method": m.name,
					"path":   path,
				}
				if op.OperationID != "" {
					props["operation_id"] = op.OperationID
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
			}
		}
	}

	if doc.Components != nil && doc.Components.Schemas != nil {
		for name := range doc.Components.Schemas {
			sid := SchemaID(specPath, name)
			out.Nodes = append(out.Nodes, graph.Node{
				ID:   sid,
				Kind: parse.KindSchema,
				Name: name,
				Path: specPath,
			})
			out.Edges = append(out.Edges, graph.Edge{
				From: fid,
				To:   sid,
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
	doc, err := Load(absPath)
	if err != nil {
		return parse.Result{}, err
	}
	if relPath == "" {
		relPath = filepath.Base(absPath)
	}
	return ToResult(relPath, doc), nil
}

package openapi_test

import (
	"testing"

	"github.com/keelwright-hq/synapse/internal/contract/openapi"
	"github.com/keelwright-hq/synapse/internal/parse"
)

func TestToResultOperationsAndSchemas(t *testing.T) {
	doc, err := openapi.LoadBytes([]byte(sampleYAML))
	if err != nil {
		t.Fatal(err)
	}
	res := openapi.ToResult("openapi.yaml", doc)

	assertKind := func(kind, name string) {
		t.Helper()
		for _, n := range res.Nodes {
			if n.Kind == kind && n.Name == name {
				return
			}
		}
		t.Fatalf("missing %s %q in %+v", kind, name, res.Nodes)
	}
	assertKind(parse.KindFile, "openapi.yaml")
	assertKind(parse.KindOperation, "GET /users")
	assertKind(parse.KindSchema, "User")

	var foundOp bool
	for _, n := range res.Nodes {
		if n.Kind != parse.KindOperation {
			continue
		}
		foundOp = true
		if n.ID != "operation:openapi.yaml#GET /users" {
			t.Fatalf("operation id: %q", n.ID)
		}
		if n.Props["operation_id"] != "ListUsers" {
			t.Fatalf("operation_id prop: %q", n.Props["operation_id"])
		}
		if n.Props["method"] != "GET" || n.Props["path"] != "/users" {
			t.Fatalf("props: %#v", n.Props)
		}
	}
	if !foundOp {
		t.Fatal("no operation node")
	}

	wantEdge := false
	for _, e := range res.Edges {
		if e.Type == parse.EdgeContains && e.From == "file:openapi.yaml" && e.To == "operation:openapi.yaml#GET /users" {
			wantEdge = true
		}
	}
	if !wantEdge {
		t.Fatalf("missing contains edge: %+v", res.Edges)
	}
}

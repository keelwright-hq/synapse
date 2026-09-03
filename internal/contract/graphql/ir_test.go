package graphql_test

import (
	"testing"

	"github.com/keelwright-hq/synapse/internal/contract/graphql"
	"github.com/keelwright-hq/synapse/internal/parse"
)

func TestToResultTypesFieldsOperations(t *testing.T) {
	schema, err := graphql.LoadBytes([]byte(sampleSDL), "schema.graphql")
	if err != nil {
		t.Fatal(err)
	}
	res := graphql.ToResult("schema.graphql", schema)

	assertKind := func(kind, name string) {
		t.Helper()
		for _, n := range res.Nodes {
			if n.Kind == kind && n.Name == name {
				return
			}
		}
		t.Fatalf("missing %s %q in %+v", kind, name, res.Nodes)
	}
	assertKind(parse.KindFile, "schema.graphql")
	assertKind(parse.KindType, "Query")
	assertKind(parse.KindType, "User")
	assertKind(parse.KindOperation, "query users")
	assertKind(parse.KindField, "User.id")
	assertKind(parse.KindField, "User.name")

	var foundOp bool
	for _, n := range res.Nodes {
		if n.Kind != parse.KindOperation {
			continue
		}
		foundOp = true
		if n.ID != "operation:schema.graphql#query users" {
			t.Fatalf("operation id: %q", n.ID)
		}
		if n.Props["operation_id"] != "users" {
			t.Fatalf("operation_id prop: %q", n.Props["operation_id"])
		}
		if n.Props["gql_root"] != "Query" {
			t.Fatalf("gql_root: %q", n.Props["gql_root"])
		}
	}
	if !foundOp {
		t.Fatal("no operation node")
	}

	var foundField bool
	for _, n := range res.Nodes {
		if n.Kind == parse.KindField && n.Name == "User.name" {
			foundField = true
			if n.ID != "field:schema.graphql#User.name" {
				t.Fatalf("field id: %q", n.ID)
			}
			if n.Props["parent"] != "User" {
				t.Fatalf("parent: %q", n.Props["parent"])
			}
		}
	}
	if !foundField {
		t.Fatal("missing User.name field")
	}

	// Root fields are operations only — not KindField.
	for _, n := range res.Nodes {
		if n.Kind == parse.KindField && n.Name == "Query.users" {
			t.Fatal("root field should not be KindField")
		}
	}

	wantEdge := false
	for _, e := range res.Edges {
		if e.Type == parse.EdgeContains && e.From == "file:schema.graphql" && e.To == "operation:schema.graphql#query users" {
			wantEdge = true
		}
	}
	if !wantEdge {
		t.Fatalf("missing file→operation contains: %+v", res.Edges)
	}

	wantTypeField := false
	for _, e := range res.Edges {
		if e.Type == parse.EdgeContains && e.From == "type:schema.graphql#User" && e.To == "field:schema.graphql#User.name" {
			wantTypeField = true
		}
	}
	if !wantTypeField {
		t.Fatalf("missing type→field contains: %+v", res.Edges)
	}
}

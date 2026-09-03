package graphql_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/keelwright-hq/synapse/internal/contract/graphql"
	"github.com/keelwright-hq/synapse/internal/parse"
)

const sampleSDL = `
type Query {
  users: [User!]!
}

type User {
  id: ID!
  name: String!
}
`

func TestLoadBytes(t *testing.T) {
	doc, err := graphql.LoadBytes([]byte(sampleSDL), "schema.graphql")
	if err != nil {
		t.Fatal(err)
	}
	var foundQuery, foundUser bool
	for _, d := range doc.Definitions {
		if d.Name == "Query" {
			foundQuery = true
			if d.Fields.ForName("users") == nil {
				t.Fatal("expected Query.users")
			}
		}
		if d.Name == "User" {
			foundUser = true
		}
	}
	if !foundQuery {
		t.Fatal("expected Query type")
	}
	if !foundUser {
		t.Fatal("expected User type")
	}
}

func TestLoadFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "schema.graphql")
	if err := os.WriteFile(path, []byte(sampleSDL), 0o644); err != nil {
		t.Fatal(err)
	}
	doc, err := graphql.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range doc.Definitions {
		if d.Name == "User" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected User")
	}
}

func TestLooksLikeGraphQLRejectsNoise(t *testing.T) {
	noise := []byte("package main\n\nfunc main() {}\n")
	if graphql.LooksLikeGraphQL(noise) {
		t.Fatal("Go source should not look like GraphQL")
	}
	if _, err := graphql.LoadBytes(noise, "x.graphql"); err == nil {
		t.Fatal("expected load error")
	}
}

func TestLooksLikeGraphQLAcceptsSchemaKeyword(t *testing.T) {
	sdl := []byte("schema {\n  query: Query\n}\ntype Query { ok: Boolean }\n")
	if !graphql.LooksLikeGraphQL(sdl) {
		t.Fatal("expected schema keyword to match")
	}
}

// Split schemas reference types defined in other files; syntactic parse must succeed.
func TestLoadBytesAllowsUnresolvedTypes(t *testing.T) {
	partial := []byte(`
type Query {
  users: [User!]!
}
`)
	doc, err := graphql.LoadBytes(partial, "query.graphql")
	if err != nil {
		t.Fatalf("split schema should parse without validation: %v", err)
	}
	res := graphql.ToResult("query.graphql", doc)
	found := false
	for _, n := range res.Nodes {
		if n.Kind == parse.KindOperation && n.Name == "query users" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected query users operation, nodes=%+v", res.Nodes)
	}
}

package graphql_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/taricsa/synapse/internal/contract/graphql"
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
	schema, err := graphql.LoadBytes([]byte(sampleSDL), "schema.graphql")
	if err != nil {
		t.Fatal(err)
	}
	if schema.Query == nil {
		t.Fatal("expected Query root")
	}
	if _, ok := schema.Types["User"]; !ok {
		t.Fatal("expected User type")
	}
	users := schema.Query.Fields.ForName("users")
	if users == nil {
		t.Fatal("expected Query.users")
	}
}

func TestLoadFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "schema.graphql")
	if err := os.WriteFile(path, []byte(sampleSDL), 0o644); err != nil {
		t.Fatal(err)
	}
	schema, err := graphql.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if schema.Types["User"] == nil {
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

package index_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/keelwright-hq/synapse/internal/index"
	"github.com/keelwright-hq/synapse/internal/parse"
	"github.com/keelwright-hq/synapse/internal/store/memory"
)

func TestIndexerIndexesGraphQLSchema(t *testing.T) {
	root := t.TempDir()
	spec := filepath.Join(root, "schema.graphql")
	const sampleSDL = `
type Query {
  users: [User!]!
}

type User {
  id: ID!
  name: String!
}
`
	if err := os.WriteFile(spec, []byte(sampleSDL), 0o644); err != nil {
		t.Fatal(err)
	}
	// Non-SDL file with graphql-ish extension should be ignored when sniff fails.
	if err := os.WriteFile(filepath.Join(root, "notes.graphql"), []byte("just some notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	store := memory.New()
	stats, err := index.New(store).Run(root, index.Options{Repo: "demo", Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Processed < 1 {
		t.Fatalf("expected processed graphql, stats=%+v", stats)
	}

	op, err := store.GetNodeByURI("repo://demo/schema.graphql#operation:query users")
	if err != nil {
		t.Fatal(err)
	}
	if op.Kind != parse.KindOperation || op.Props["operation_id"] != "users" {
		t.Fatalf("operation: %+v", op)
	}
	if _, err := store.GetNodeByURI("repo://demo/schema.graphql#type:User"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetNodeByURI("repo://demo/schema.graphql#field:User.name"); err != nil {
		t.Fatal(err)
	}
}

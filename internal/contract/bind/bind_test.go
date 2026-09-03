package bind_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/keelwright-hq/synapse/internal/contract/bind"
	"github.com/keelwright-hq/synapse/internal/index"
	"github.com/keelwright-hq/synapse/internal/parse"
	"github.com/keelwright-hq/synapse/internal/store/memory"
)

func TestBindImplementsSameRepo(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "openapi.yaml"), []byte(`openapi: 3.0.3
info:
  title: t
  version: "1"
paths:
  /users:
    get:
      operationId: ListUsers
      responses:
        "200":
          description: ok
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "handler.go"), []byte(`package main
func ListUsers() {}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	store := memory.New()
	if _, err := index.New(store).Run(root, index.Options{Repo: "api", Workers: 1}); err != nil {
		t.Fatal(err)
	}
	if err := bind.Bind(bind.Options{
		Members: []bind.Member{{Name: "api", Root: root, Store: store}},
	}); err != nil {
		t.Fatal(err)
	}

	handler, err := store.GetNodeByURI("repo://api/handler.go#func:ListUsers")
	if err != nil {
		t.Fatal(err)
	}
	op, err := store.GetNodeByURI("repo://api/openapi.yaml#operation:GET /users")
	if err != nil {
		t.Fatal(err)
	}
	edges, err := store.OutEdges(handler.ID, parse.EdgeImplements)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range edges {
		if e.To == op.ID {
			if e.Props["match"] != bind.MatchOperationID {
				t.Fatalf("match prop: got %q want %q", e.Props["match"], bind.MatchOperationID)
			}
			return
		}
	}
	t.Fatalf("missing implements edge: %+v", edges)
}

func TestBindGraphQLResolverVariants(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "schema.graphql"), []byte(`
type Query {
  users: [User!]!
}
type User {
  id: ID!
  name: String!
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "handler.go"), []byte(`package main
func ResolveUsers() {}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	store := memory.New()
	if _, err := index.New(store).Run(root, index.Options{Repo: "api", Workers: 1}); err != nil {
		t.Fatal(err)
	}
	if err := bind.Bind(bind.Options{
		Members: []bind.Member{{Name: "api", Root: root, Store: store}},
	}); err != nil {
		t.Fatal(err)
	}

	handler, err := store.GetNodeByURI("repo://api/handler.go#func:ResolveUsers")
	if err != nil {
		t.Fatal(err)
	}
	op, err := store.GetNodeByURI("repo://api/schema.graphql#operation:query users")
	if err != nil {
		t.Fatal(err)
	}
	edges, err := store.OutEdges(handler.ID, parse.EdgeImplements)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range edges {
		if e.To == op.ID {
			return
		}
	}
	t.Fatalf("missing implements edge for ResolveUsers: %+v", edges)
}

func TestBindGRPCStubVariants(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "users.proto"), []byte(`
syntax = "proto3";
package users;
service UserService {
  rpc ListUsers(ListUsersRequest) returns (ListUsersResponse);
}
message ListUsersRequest {}
message ListUsersResponse {}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "handler.go"), []byte(`package main
func UserService_ListUsers() {}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	store := memory.New()
	if _, err := index.New(store).Run(root, index.Options{Repo: "api", Workers: 1}); err != nil {
		t.Fatal(err)
	}
	if err := bind.Bind(bind.Options{
		Members: []bind.Member{{Name: "api", Root: root, Store: store}},
	}); err != nil {
		t.Fatal(err)
	}

	handler, err := store.GetNodeByURI("repo://api/handler.go#func:UserService_ListUsers")
	if err != nil {
		t.Fatal(err)
	}
	op, err := store.GetNodeByURI("repo://api/users.proto#operation:UserService.ListUsers")
	if err != nil {
		t.Fatal(err)
	}
	edges, err := store.OutEdges(handler.ID, parse.EdgeImplements)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range edges {
		if e.To == op.ID {
			return
		}
	}
	t.Fatalf("missing implements edge for UserService_ListUsers: %+v", edges)
}



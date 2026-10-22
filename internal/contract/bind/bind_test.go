package bind_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/keelwright-hq/synapse/internal/contract/bind"
	"github.com/keelwright-hq/synapse/internal/graph"
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
			if e.Props["provenance"] != "INFERRED" {
				t.Fatalf("provenance prop: got %q want INFERRED", e.Props["provenance"])
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

func TestBindCrossRepoCollidingNodeIDsAndNeighborhood(t *testing.T) {
	apiRoot := t.TempDir()
	workerRoot := t.TempDir()

	if err := os.WriteFile(filepath.Join(apiRoot, "openapi.yaml"), []byte(`openapi: 3.0.3
info:
  title: Users API
  version: "1.0"
paths:
  /users:
    get:
      operationId: Users
      responses:
        "200":
          description: ok
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(apiRoot, "svc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(apiRoot, "svc", "handler.go"), []byte(`package svc
// API Users handler
func Users() string { return "from-api" }
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(workerRoot, "svc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workerRoot, "svc", "handler.go"), []byte(`package svc
// Worker Users caller
func Users() string { return "/users" }
`), 0o644); err != nil {
		t.Fatal(err)
	}

	apiStore := memory.New()
	workerStore := memory.New()
	overlayStore := memory.New()

	if _, err := index.New(apiStore).Run(apiRoot, index.Options{Repo: "api", Workers: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := index.New(workerStore).Run(workerRoot, index.Options{Repo: "worker", Workers: 1}); err != nil {
		t.Fatal(err)
	}

	if err := bind.Bind(bind.Options{
		Members: []bind.Member{
			{Name: "api", Root: apiRoot, Store: apiStore},
			{Name: "worker", Root: workerRoot, Store: workerStore},
		},
		Overlay: overlayStore,
	}); err != nil {
		t.Fatal(err)
	}

	op, err := apiStore.GetNodeByURI("repo://api/openapi.yaml#operation:GET /users")
	if err != nil {
		t.Fatal(err)
	}
	implEdges, err := apiStore.InEdges(op.ID, parse.EdgeImplements)
	if err != nil || len(implEdges) != 1 {
		t.Fatalf("expected 1 same-repo implements edge in apiStore, got %+v (err=%v)", implEdges, err)
	}
	consEdges, err := overlayStore.InEdges(
		graph.NodeID(op.Props["repo_uri"]),
		parse.EdgeConsumes,
	)
	if err != nil || len(consEdges) == 0 {
		t.Fatalf("expected cross-repo consumes edge in overlayStore even when NodeIDs collide, got %+v (err=%v)", consEdges, err)
	}
}

func TestBindPrunesStaleSameRepoContractEdges(t *testing.T) {
	root := t.TempDir()
	spec := filepath.Join(root, "openapi.yaml")
	code := filepath.Join(root, "handler.go")
	if err := os.WriteFile(spec, []byte(`openapi: 3.0.3
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
	if err := os.WriteFile(code, []byte("package main\nfunc ListUsers() {}\nfunc Other() {}\n"), 0o644); err != nil {
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

	op, err := store.GetNodeByURI("repo://api/openapi.yaml#operation:GET /users")
	if err != nil {
		t.Fatal(err)
	}
	in1, err := store.InEdges(op.ID, parse.EdgeImplements)
	if err != nil || len(in1) != 1 {
		t.Fatalf("expected 1 implements edge before rename, got %v (err=%v)", in1, err)
	}

	// Remove ListUsers from openapi.yaml operationId so no symbol matches it on re-bind.
	if err := os.WriteFile(spec, []byte(`openapi: 3.0.3
info:
  title: t
  version: "1"
paths:
  /users:
    get:
      operationId: CompletelyDifferentOp
      responses:
        "200":
          description: ok
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := index.New(store).Run(root, index.Options{Repo: "api", Workers: 1}); err != nil {
		t.Fatal(err)
	}
	if err := bind.Bind(bind.Options{
		Members: []bind.Member{{Name: "api", Root: root, Store: store}},
	}); err != nil {
		t.Fatal(err)
	}

	in2, err := store.InEdges(op.ID, parse.EdgeImplements)
	if err != nil {
		t.Fatal(err)
	}
	if len(in2) != 0 {
		t.Fatalf("expected stale implements edge to be pruned on re-bind, got %+v", in2)
	}
}



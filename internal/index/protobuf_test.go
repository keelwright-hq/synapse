package index_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/keelwright-hq/synapse/internal/index"
	"github.com/keelwright-hq/synapse/internal/parse"
	"github.com/keelwright-hq/synapse/internal/store/memory"
)

func TestIndexerIndexesProtobuf(t *testing.T) {
	root := t.TempDir()
	messages := `
syntax = "proto3";
package users;
message User {
  string id = 1;
  string name = 2;
}
`
	users := `
syntax = "proto3";
package users;
import "messages.proto";
service UserService {
  rpc ListUsers(ListUsersRequest) returns (ListUsersResponse);
}
message ListUsersRequest {}
message ListUsersResponse {
  repeated User users = 1;
}
`
	if err := os.WriteFile(filepath.Join(root, "messages.proto"), []byte(messages), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "users.proto"), []byte(users), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "notes.proto"), []byte("just notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	store := memory.New()
	stats, err := index.New(store).Run(root, index.Options{Repo: "demo", Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Processed < 1 {
		t.Fatalf("expected processed protobuf, stats=%+v", stats)
	}

	op, err := store.GetNodeByURI("repo://demo/users.proto#operation:UserService.ListUsers")
	if err != nil {
		t.Fatal(err)
	}
	if op.Kind != parse.KindOperation || op.Props["operation_id"] != "ListUsers" {
		t.Fatalf("operation: %+v", op)
	}
	if _, err := store.GetNodeByURI("repo://demo/users.proto#service:UserService"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetNodeByURI("repo://demo/messages.proto#schema:User"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetNodeByURI("repo://demo/messages.proto#field:User.name"); err != nil {
		t.Fatal(err)
	}
}

package protobuf_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/keelwright-hq/synapse/internal/contract/protobuf"
	"github.com/keelwright-hq/synapse/internal/parse"
)

const sampleProto = `
syntax = "proto3";

package users;

service UserService {
  rpc ListUsers(ListUsersRequest) returns (ListUsersResponse);
}

message ListUsersRequest {}

message ListUsersResponse {
  string status = 1;
}

message User {
  string id = 1;
  string name = 2;
}
`

func TestLoadBytes(t *testing.T) {
	fd, err := protobuf.LoadBytes([]byte(sampleProto), "users.proto", protobuf.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if fd.Package != "users" {
		t.Fatalf("package: %q", fd.Package)
	}
	if fd.Desc == nil {
		t.Fatal("expected linked descriptor")
	}
	if fd.Desc.Services().Len() != 1 {
		t.Fatalf("services: %d", fd.Desc.Services().Len())
	}
}

func TestLoadFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "users.proto")
	if err := os.WriteFile(path, []byte(sampleProto), 0o644); err != nil {
		t.Fatal(err)
	}
	fd, err := protobuf.Load(path, "users.proto", protobuf.LoadOptions{IncludePaths: []string{dir}})
	if err != nil {
		t.Fatal(err)
	}
	if fd.Desc == nil || fd.Desc.Services().Get(0).Name() != "UserService" {
		t.Fatalf("unexpected: %+v", fd)
	}
}

func TestLooksLikeProtoRejectsNoise(t *testing.T) {
	noise := []byte("package main\n\nfunc main() {}\n")
	if protobuf.LooksLikeProto(noise) {
		t.Fatal("Go source should not look like proto")
	}
	if _, err := protobuf.LoadBytes(noise, "x.proto", protobuf.LoadOptions{}); err == nil {
		t.Fatal("expected load error")
	}
}

func TestLoadBytesResolvesImports(t *testing.T) {
	messages := `
syntax = "proto3";
package users;
message User {
  string id = 1;
}
`
	users := `
syntax = "proto3";
package users;
import "messages.proto";
service UserService {
  rpc GetUser(GetUserRequest) returns (User);
}
message GetUserRequest {
  string id = 1;
}
`
	fd, err := protobuf.LoadBytes([]byte(users), "users.proto", protobuf.LoadOptions{
		ExtraSources: map[string]string{"messages.proto": messages},
	})
	if err != nil {
		t.Fatal(err)
	}
	if fd.Desc == nil {
		t.Fatal("expected linked descriptor with import resolved")
	}
	svc := fd.Desc.Services().Get(0)
	m := svc.Methods().Get(0)
	if string(m.Output().Name()) != "User" {
		t.Fatalf("output type: %s", m.Output().FullName())
	}
}

func TestLoadBytesSoftFailsMissingImport(t *testing.T) {
	users := `
syntax = "proto3";
package users;
import "missing.proto";
service UserService {
  rpc ListUsers(ListUsersRequest) returns (ListUsersResponse);
}
message ListUsersRequest {}
message ListUsersResponse {}
`
	fd, err := protobuf.LoadBytes([]byte(users), "users.proto", protobuf.LoadOptions{})
	if err != nil {
		t.Fatalf("soft-fail should still parse: %v", err)
	}
	if fd.Proto == nil {
		t.Fatal("expected unlinked proto fallback")
	}
	res := protobuf.ToResult("users.proto", fd)
	found := false
	for _, n := range res.Nodes {
		if n.Kind == parse.KindOperation && n.Name == "UserService.ListUsers" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected rpc node after soft-fail, nodes=%+v", res.Nodes)
	}
}

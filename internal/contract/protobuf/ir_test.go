package protobuf_test

import (
	"testing"

	"github.com/keelwright-hq/synapse/internal/contract/protobuf"
	"github.com/keelwright-hq/synapse/internal/parse"
)

func TestToResultServicesMessagesFields(t *testing.T) {
	fd, err := protobuf.LoadBytes([]byte(sampleProto), "users.proto", protobuf.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	res := protobuf.ToResult("users.proto", fd)

	assertKind := func(kind, name string) {
		t.Helper()
		for _, n := range res.Nodes {
			if n.Kind == kind && n.Name == name {
				return
			}
		}
		t.Fatalf("missing %s %q in %+v", kind, name, res.Nodes)
	}
	assertKind(parse.KindFile, "users.proto")
	assertKind(parse.KindService, "UserService")
	assertKind(parse.KindOperation, "UserService.ListUsers")
	assertKind(parse.KindSchema, "User")
	assertKind(parse.KindField, "User.name")

	var foundOp bool
	for _, n := range res.Nodes {
		if n.Kind != parse.KindOperation {
			continue
		}
		foundOp = true
		if n.ID != "operation:users.proto#UserService.ListUsers" {
			t.Fatalf("operation id: %q", n.ID)
		}
		if n.Props["operation_id"] != "ListUsers" {
			t.Fatalf("operation_id: %q", n.Props["operation_id"])
		}
		if n.Props["service"] != "UserService" {
			t.Fatalf("service: %q", n.Props["service"])
		}
		if n.Props["grpc_path"] != "/users.UserService/ListUsers" {
			t.Fatalf("grpc_path: %q", n.Props["grpc_path"])
		}
	}
	if !foundOp {
		t.Fatal("no operation")
	}

	wantSvcOp := false
	for _, e := range res.Edges {
		if e.Type == parse.EdgeContains && e.From == "service:users.proto#UserService" && e.To == "operation:users.proto#UserService.ListUsers" {
			wantSvcOp = true
		}
	}
	if !wantSvcOp {
		t.Fatalf("missing service→operation contains: %+v", res.Edges)
	}
}

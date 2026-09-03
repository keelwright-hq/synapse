package mcpserver_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/taricsa/synapse/internal/contract/bind"
	"github.com/taricsa/synapse/internal/index"
	mcpserver "github.com/taricsa/synapse/internal/mcp"
	"github.com/taricsa/synapse/internal/store/federated"
	"github.com/taricsa/synapse/internal/store/memory"
)

func TestMCPResolveAPIWorkspaceOpenAPIandGRPC(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	wsRoot := filepath.Join(filepath.Dir(file), "..", "..", "testdata", "fixtures", "workspace")
	apiRoot := filepath.Join(wsRoot, "api")
	workerRoot := filepath.Join(wsRoot, "worker")

	apiStore := memory.New()
	workerStore := memory.New()
	overlay := memory.New()
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
		Overlay: overlay,
	}); err != nil {
		t.Fatal(err)
	}
	fed, err := federated.NewWithOverlay([]federated.Member{
		{Name: "api", Store: apiStore},
		{Name: "worker", Store: workerStore},
	}, overlay)
	if err != nil {
		t.Fatal(err)
	}

	s := mcpserver.New(mcpserver.Options{
		Federated: fed,
		RepoRoots: map[string]string{"api": apiRoot, "worker": workerRoot},
	})
	_ = s.HandleMessage(context.Background(), []byte(`{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0"}}}`))

	list := s.HandleMessage(context.Background(), []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	listBody, _ := json.Marshal(list)
	for _, tool := range []string{"resolve_api", "list_providers", "list_consumers"} {
		if !strings.Contains(string(listBody), tool) {
			t.Fatalf("missing tool %q: %s", tool, listBody)
		}
	}

	// OpenAPI resolve_api
	call := s.HandleMessage(context.Background(), []byte(mustJSON(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      10,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "resolve_api",
			"arguments": map[string]any{"query": "GET /users"},
		},
	})))
	body, _ := json.Marshal(call)
	text := string(body)
	if !strings.Contains(text, "repo://api/") {
		t.Fatalf("openapi resolve missing api uri: %s", text)
	}
	if !strings.Contains(text, "repo://worker/") {
		t.Fatalf("openapi resolve missing worker uri: %s", text)
	}
	if !strings.Contains(text, "providers") || !strings.Contains(text, "consumers") {
		t.Fatalf("openapi resolve missing providers/consumers: %s", text)
	}
	if !strings.Contains(text, "matched via") && !strings.Contains(text, "contract edge") {
		t.Fatalf("openapi resolve missing heuristic note: %s", text)
	}

	// gRPC resolve_api
	call = s.HandleMessage(context.Background(), []byte(mustJSON(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      11,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "resolve_api",
			"arguments": map[string]any{
				"query": "repo://api/users.proto#operation:UserService.ListUsers",
			},
		},
	})))
	body, _ = json.Marshal(call)
	text = string(body)
	if !strings.Contains(text, "UserService") && !strings.Contains(text, "ListUsers") {
		t.Fatalf("grpc resolve: %s", text)
	}
	if !strings.Contains(text, "repo://") {
		t.Fatalf("grpc resolve missing repo://: %s", text)
	}

	call = s.HandleMessage(context.Background(), []byte(mustJSON(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      12,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "list_providers",
			"arguments": map[string]any{"operation": "GET /users"},
		},
	})))
	body, _ = json.Marshal(call)
	if !strings.Contains(string(body), "providers") {
		t.Fatalf("list_providers: %s", body)
	}

	call = s.HandleMessage(context.Background(), []byte(mustJSON(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      13,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "list_consumers",
			"arguments": map[string]any{"operation": "GET /users"},
		},
	})))
	body, _ = json.Marshal(call)
	if !strings.Contains(string(body), "consumers") {
		t.Fatalf("list_consumers: %s", body)
	}
	if !strings.Contains(string(body), "repo://worker/") {
		t.Fatalf("list_consumers missing worker: %s", body)
	}
}

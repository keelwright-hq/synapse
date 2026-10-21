package mcpserver_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keelwright-hq/synapse/internal/graph"
	"github.com/keelwright-hq/synapse/internal/index"
	mcpserver "github.com/keelwright-hq/synapse/internal/mcp"
	"github.com/keelwright-hq/synapse/internal/store/badger"
)

// End-to-end smoke: index a tiny repo, list MCP tools, call get_symbol.
func TestE2EIndexAndMCP(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "greet.go")
	if err := os.WriteFile(src, []byte("package greet\n\nfunc Hello() string { return \"hi\" }\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	store, err := badger.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	stats, err := index.New(store).Run(root, index.Options{Workers: 2})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Processed < 1 {
		t.Fatalf("expected processed files, got %+v", stats)
	}

	s := mcpserver.New(mcpserver.Options{Store: store, RootDir: root})
	_ = s.HandleMessage(context.Background(), []byte(`{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"e2e","version":"0"}}}`))

	list := s.HandleMessage(context.Background(), []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	listBody, _ := json.Marshal(list)
	if !strings.Contains(string(listBody), "get_neighborhood") {
		t.Fatalf("tools/list: %s", listBody)
	}

	call := s.HandleMessage(context.Background(), []byte(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_symbol","arguments":{"symbol":"Hello"}}}`))
	body, _ := json.Marshal(call)
	if !strings.Contains(string(body), "func:greet.go#Hello") && !strings.Contains(string(body), "Hello") {
		t.Fatalf("get_symbol: %s", body)
	}
}

func TestMCPOpenSessionAllowsConcurrentCLIReindex(t *testing.T) {
	root := t.TempDir()
	dataDir := t.TempDir()
	src := filepath.Join(root, "greet.go")
	if err := os.WriteFile(src, []byte("package greet\n\nfunc Hello() string { return \"hi\" }\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	initStore, err := badger.OpenWithRepo(dataDir, "greet")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := index.New(initStore).Run(root, index.Options{Workers: 2, Repo: "greet"}); err != nil {
		t.Fatal(err)
	}
	if err := initStore.Close(); err != nil {
		t.Fatal(err)
	}

	s := mcpserver.New(mcpserver.Options{
		RootDir: root,
		OpenSession: func() (graph.Store, []string, func(), error) {
			cur, err := badger.OpenWithRepo(dataDir, "greet")
			if err != nil {
				return nil, nil, nil, err
			}
			return cur, nil, func() { _ = cur.Close() }, nil
		},
	})
	_ = s.HandleMessage(context.Background(), []byte(`{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"e2e","version":"0"}}}`))

	call1 := s.HandleMessage(context.Background(), []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_symbol","arguments":{"symbol":"Hello"}}}`))
	body1, _ := json.Marshal(call1)
	if !strings.Contains(string(body1), "func:greet.go#Hello") {
		t.Fatalf("get_symbol Hello: %s", body1)
	}

	// While the MCP server instance is active, re-open Badger as a CLI index command would.
	if err := os.WriteFile(src, []byte("package greet\n\nfunc Hello() string { return \"hi\" }\n\nfunc World() string { return \"world\" }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cliStore, err := badger.OpenWithRepo(dataDir, "greet")
	if err != nil {
		t.Fatalf("expected no Badger lock contention while MCP server is idle: %v", err)
	}
	if _, err := index.New(cliStore).Run(root, index.Options{Workers: 2, Repo: "greet"}); err != nil {
		t.Fatal(err)
	}
	if err := cliStore.Close(); err != nil {
		t.Fatal(err)
	}

	call2 := s.HandleMessage(context.Background(), []byte(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_symbol","arguments":{"symbol":"World"}}}`))
	body2, _ := json.Marshal(call2)
	if !strings.Contains(string(body2), "func:greet.go#World") {
		t.Fatalf("expected MCP server to observe newly indexed symbol World without restart, got: %s", body2)
	}
}

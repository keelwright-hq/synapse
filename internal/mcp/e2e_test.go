package mcpserver_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/taricsa/synapse/internal/index"
	mcpserver "github.com/taricsa/synapse/internal/mcp"
	"github.com/taricsa/synapse/internal/store/badger"
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

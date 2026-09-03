package mcpserver_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/keelwright-hq/synapse/internal/graph"
	mcpserver "github.com/keelwright-hq/synapse/internal/mcp"
	"github.com/keelwright-hq/synapse/internal/parse"
	"github.com/keelwright-hq/synapse/internal/store/memory"
)

func TestToolsAgainstFixtureGraph(t *testing.T) {
	store := memory.New()
	mustPut(t, store, graph.Node{ID: "file:a.go", Kind: parse.KindFile, Name: "a.go", Path: "a.go"})
	mustPut(t, store, graph.Node{ID: "func:a.go#Alpha", Kind: parse.KindFunction, Name: "Alpha", Path: "a.go"})
	mustPut(t, store, graph.Node{ID: "symbol:Printf", Kind: parse.KindSymbol, Name: "Printf"})
	mustEdge(t, store, graph.Edge{From: "file:a.go", To: "func:a.go#Alpha", Type: parse.EdgeContains})
	mustEdge(t, store, graph.Edge{From: "func:a.go#Alpha", To: "symbol:Printf", Type: parse.EdgeCalls})

	s := mcpserver.New(mcpserver.Options{Store: store, RootDir: "."})

	// Initialize handshake so tool listing works.
	initMsg := mustJSON(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      0,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "test", "version": "0"},
		},
	})
	if msg := s.HandleMessage(context.Background(), []byte(initMsg)); isRPCError(msg) {
		t.Fatalf("initialize: %v", msg)
	}

	listRes := s.HandleMessage(context.Background(), []byte(mustJSON(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/list",
	})))
	listBody, _ := json.Marshal(listRes)
	for _, want := range []string{
		"get_symbol", "find_references", "get_neighborhood", "search_graph",
		"resolve_api", "list_providers", "list_consumers",
	} {
		if !strings.Contains(string(listBody), want) {
			t.Fatalf("missing tool %q in %s", want, listBody)
		}
	}

	res := s.HandleMessage(context.Background(), []byte(mustJSON(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "get_symbol",
			"arguments": map[string]any{
				"symbol": "Alpha",
			},
		},
	})))
	body, _ := json.Marshal(res)
	if !strings.Contains(string(body), "func:a.go#Alpha") {
		t.Fatalf("unexpected get_symbol response: %s", body)
	}

	res = s.HandleMessage(context.Background(), []byte(mustJSON(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "search_graph",
			"arguments": map[string]any{
				"query": "Alpha",
			},
		},
	})))
	body, _ = json.Marshal(res)
	if !strings.Contains(string(body), "Alpha") {
		t.Fatalf("search_graph: %s", body)
	}

	res = s.HandleMessage(context.Background(), []byte(mustJSON(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      4,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "get_symbol",
			"arguments": map[string]any{
				"symbol": "does-not-exist",
			},
		},
	})))
	body, _ = json.Marshal(res)
	if !strings.Contains(strings.ToLower(string(body)), "not found") &&
		!strings.Contains(string(body), `"isError":true`) {
		t.Fatalf("expected graceful missing-symbol error, got %s", body)
	}
}

func isRPCError(msg mcp.JSONRPCMessage) bool {
	_, ok := msg.(mcp.JSONRPCError)
	return ok
}

func mustPut(t *testing.T, s *memory.Store, n graph.Node) {
	t.Helper()
	if err := s.PutNode(n); err != nil {
		t.Fatal(err)
	}
}

func mustEdge(t *testing.T, s *memory.Store, e graph.Edge) {
	t.Helper()
	if err := s.PutEdge(e); err != nil {
		t.Fatal(err)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

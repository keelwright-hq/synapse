package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/keelwright-hq/synapse/internal/buildinfo"
	"github.com/keelwright-hq/synapse/internal/graph"
	"github.com/keelwright-hq/synapse/internal/rank"
	"github.com/keelwright-hq/synapse/internal/store/federated"
)

// Options configure the MCP server.
type Options struct {
	Store   graph.Store
	RootDir string
	// Federated is an optional long-lived federated base store. When set, each
	// tool call uses Federated.Session() (pin maps are not concurrency-safe).
	Federated *federated.Store
	// RepoRoots maps logical repo names to filesystem roots (workspace mode).
	RepoRoots map[string]string
	// OpenWarnings are soft-fail messages from opening workspace shards.
	OpenWarnings []string
}

// session returns the store for one tool call.
func (o Options) session() graph.Store {
	if o.Federated != nil {
		return o.Federated.Session()
	}
	return o.Store
}

func (o Options) withWarnings(warnings []string) []string {
	out := append([]string(nil), o.OpenWarnings...)
	out = append(out, warnings...)
	return out
}

func takeFedWarnings(store graph.Store) []string {
	if taker, ok := store.(interface{ TakeWarnings() []string }); ok {
		return taker.TakeWarnings()
	}
	return nil
}

// New builds an MCP server with Synapse graph tools and resources.
func New(opts Options) *server.MCPServer {
	s := server.NewMCPServer(
		"synapse",
		buildinfo.Version,
		server.WithToolCapabilities(false),
		server.WithResourceCapabilities(true, false),
	)
	registerTools(s, opts)
	registerResources(s, opts)
	return s
}

// ServeStdio runs the server over stdin/stdout.
func ServeStdio(s *server.MCPServer) error {
	return server.ServeStdio(s)
}

func registerTools(s *server.MCPServer, opts Options) {
	getSymbol := mcp.NewTool("get_symbol",
		mcp.WithDescription("Fetch a graph node by repo:// URI, Phase-1 id, or unique name"),
		mcp.WithString("symbol", mcp.Required(), mcp.Description("repo:// URI, node id, or symbol name")),
	)
	s.AddTool(getSymbol, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sym, err := req.RequireString("symbol")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		store := opts.session()
		id, err := rank.ResolveSeed(store, sym)
		if err != nil {
			return toolErr(err), nil
		}
		node, err := store.GetNode(id)
		if err != nil {
			return toolErr(err), nil
		}
		return jsonResult(node)
	})

	findRefs := mcp.NewTool("find_references",
		mcp.WithDescription("Find incoming call edges to a symbol"),
		mcp.WithString("symbol", mcp.Required(), mcp.Description("repo:// URI, node id, or symbol name")),
	)
	s.AddTool(findRefs, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sym, err := req.RequireString("symbol")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		store := opts.session()
		id, err := rank.ResolveSeed(store, sym)
		if err != nil {
			return toolErr(err), nil
		}
		edges, err := rank.FindReferences(store, id, "")
		if err != nil {
			return toolErr(err), nil
		}
		return jsonResult(edges)
	})

	getNeighborhood := mcp.NewTool("get_neighborhood",
		mcp.WithDescription("Ranked neighborhood context around a symbol"),
		mcp.WithString("symbol", mcp.Required(), mcp.Description("repo:// URI, node id, or symbol name")),
		mcp.WithNumber("depth", mcp.Description("Traversal depth (default 2)")),
		mcp.WithNumber("max_nodes", mcp.Description("Max nodes (default 32)")),
		mcp.WithNumber("budget", mcp.Description("Character budget (0 = unlimited)")),
	)
	s.AddTool(getNeighborhood, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sym, err := req.RequireString("symbol")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		store := opts.session()
		id, err := rank.ResolveSeed(store, sym)
		if err != nil {
			return toolErr(err), nil
		}
		depth := intArg(req, "depth", 2)
		maxNodes := intArg(req, "max_nodes", 32)
		budget := intArg(req, "budget", 0)
		res, err := rank.Neighborhood(store, id, rank.Options{
			Depth:     depth,
			MaxNodes:  maxNodes,
			Budget:    budget,
			RootDir:   opts.RootDir,
			RepoRoots: opts.RepoRoots,
		})
		if err != nil {
			return toolErr(err), nil
		}
		res.Warnings = opts.withWarnings(append(res.Warnings, takeFedWarnings(store)...))
		return jsonResult(res)
	})

	searchGraph := mcp.NewTool("search_graph",
		mcp.WithDescription("Search graph nodes by id/name substring"),
		mcp.WithString("query", mcp.Required(), mcp.Description("Substring to match")),
		mcp.WithNumber("limit", mcp.Description("Max results (default 20)")),
	)
	s.AddTool(searchGraph, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		q, err := req.RequireString("query")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		limit := intArg(req, "limit", 20)
		store := opts.session()
		nodes, err := rank.Search(store, q, limit)
		if err != nil {
			return toolErr(err), nil
		}
		return jsonResult(nodes)
	})

	resolveAPI := mcp.NewTool("resolve_api",
		mcp.WithDescription("Resolve a contract operation to providers and consumers across repos"),
		mcp.WithString("query", mcp.Required(), mcp.Description("repo:// URI, operation id, GET /path, or grpc method")),
	)
	s.AddTool(resolveAPI, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		q, err := req.RequireString("query")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		store := opts.session()
		res, err := rank.ResolveAPI(store, q)
		if err != nil {
			return toolErr(err), nil
		}
		res.Warnings = opts.withWarnings(takeFedWarnings(store))
		return jsonResult(res)
	})

	listProviders := mcp.NewTool("list_providers",
		mcp.WithDescription("List symbols that implement a contract operation"),
		mcp.WithString("operation", mcp.Required(), mcp.Description("repo:// URI, operation id, GET /path, or grpc method")),
	)
	s.AddTool(listProviders, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		opQuery, err := req.RequireString("operation")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		store := opts.session()
		res, err := rank.ResolveAPI(store, opQuery)
		if err != nil {
			return toolErr(err), nil
		}
		payload := map[string]any{
			"operation": res.Operation,
			"repo_uri":  res.RepoURI,
			"providers": res.Providers,
			"warnings":  opts.withWarnings(takeFedWarnings(store)),
		}
		return jsonResult(payload)
	})

	listConsumers := mcp.NewTool("list_consumers",
		mcp.WithDescription("List symbols that consume a contract operation"),
		mcp.WithString("operation", mcp.Required(), mcp.Description("repo:// URI, operation id, GET /path, or grpc method")),
	)
	s.AddTool(listConsumers, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		opQuery, err := req.RequireString("operation")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		store := opts.session()
		res, err := rank.ResolveAPI(store, opQuery)
		if err != nil {
			return toolErr(err), nil
		}
		payload := map[string]any{
			"operation": res.Operation,
			"repo_uri":  res.RepoURI,
			"consumers": res.Consumers,
			"warnings":  opts.withWarnings(takeFedWarnings(store)),
		}
		return jsonResult(payload)
	})
}

func registerResources(s *server.MCPServer, opts Options) {
	fileTpl := mcp.NewResourceTemplate(
		"synapse://file/{path*}",
		"Indexed source file node",
		mcp.WithTemplateDescription("Graph file node for a relative path"),
		mcp.WithTemplateMIMEType("application/json"),
	)
	s.AddResourceTemplate(fileTpl, func(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
		path := req.Params.URI
		// URI is full synapse://file/...
		const prefix = "synapse://file/"
		rel := path
		if len(path) >= len(prefix) && path[:len(prefix)] == prefix {
			rel = path[len(prefix):]
		}
		if unescaped, err := url.PathUnescape(rel); err == nil {
			rel = unescaped
		}
		store := opts.session()
		node, err := store.GetNode(graph.NodeID("file:" + rel))
		if err != nil {
			return nil, err
		}
		b, err := json.Marshal(node)
		if err != nil {
			return nil, err
		}
		return []mcp.ResourceContents{
			mcp.TextResourceContents{URI: req.Params.URI, MIMEType: "application/json", Text: string(b)},
		}, nil
	})

	symTpl := mcp.NewResourceTemplate(
		"synapse://symbol/{id*}",
		"Indexed symbol node",
		mcp.WithTemplateDescription("Graph symbol/function/type node by id"),
		mcp.WithTemplateMIMEType("application/json"),
	)
	s.AddResourceTemplate(symTpl, func(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
		uri := req.Params.URI
		const prefix = "synapse://symbol/"
		id := uri
		if len(uri) >= len(prefix) && uri[:len(prefix)] == prefix {
			id = uri[len(prefix):]
		}
		if unescaped, err := url.PathUnescape(id); err == nil {
			id = unescaped
		}
		store := opts.session()
		node, err := store.GetNode(graph.NodeID(id))
		if err != nil {
			return nil, err
		}
		b, err := json.Marshal(node)
		if err != nil {
			return nil, err
		}
		return []mcp.ResourceContents{
			mcp.TextResourceContents{URI: uri, MIMEType: "application/json", Text: string(b)},
		}, nil
	})
}

func jsonResult(v any) (*mcp.CallToolResult, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(b)), nil
}

func toolErr(err error) *mcp.CallToolResult {
	if errors.Is(err, graph.ErrNotFound) {
		return mcp.NewToolResultError(fmt.Sprintf("not found: %v", err))
	}
	return mcp.NewToolResultError(err.Error())
}

func intArg(req mcp.CallToolRequest, key string, def int) int {
	args := req.GetArguments()
	if args == nil {
		return def
	}
	v, ok := args[key]
	if !ok || v == nil {
		return def
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return def
		}
		return int(i)
	case string:
		i, err := strconv.Atoi(n)
		if err != nil {
			return def
		}
		return i
	default:
		return def
	}
}

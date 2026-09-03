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
	"github.com/taricsa/synapse/internal/buildinfo"
	"github.com/taricsa/synapse/internal/graph"
	"github.com/taricsa/synapse/internal/rank"
)

// Options configure the MCP server.
type Options struct {
	Store   graph.Store
	RootDir string
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
		id, err := rank.ResolveSeed(opts.Store, sym)
		if err != nil {
			return toolErr(err), nil
		}
		node, err := opts.Store.GetNode(id)
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
		id, err := rank.ResolveSeed(opts.Store, sym)
		if err != nil {
			return toolErr(err), nil
		}
		edges, err := rank.FindReferences(opts.Store, id, "")
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
		id, err := rank.ResolveSeed(opts.Store, sym)
		if err != nil {
			return toolErr(err), nil
		}
		depth := intArg(req, "depth", 2)
		maxNodes := intArg(req, "max_nodes", 32)
		budget := intArg(req, "budget", 0)
		res, err := rank.Neighborhood(opts.Store, id, rank.Options{
			Depth:    depth,
			MaxNodes: maxNodes,
			Budget:   budget,
			RootDir:  opts.RootDir,
		})
		if err != nil {
			return toolErr(err), nil
		}
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
		nodes, err := rank.Search(opts.Store, q, limit)
		if err != nil {
			return toolErr(err), nil
		}
		return jsonResult(nodes)
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
		node, err := opts.Store.GetNode(graph.NodeID("file:" + rel))
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
		node, err := opts.Store.GetNode(graph.NodeID(id))
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

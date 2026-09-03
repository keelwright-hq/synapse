package rank

import (
	"fmt"
	"sort"
	"strings"

	"github.com/keelwright-hq/synapse/internal/graph"
	"github.com/keelwright-hq/synapse/internal/parse"
	"github.com/keelwright-hq/synapse/internal/uri"
)

// ContractHit is a provider or consumer linked to a contract operation.
type ContractHit struct {
	Node    graph.Node     `json:"node"`
	RepoURI string         `json:"repo_uri,omitempty"`
	Match   string         `json:"match,omitempty"`
	Note    string         `json:"note"`
	Edge    graph.EdgeType `json:"edge"`
}

// APIResolveResult is the payload for resolve_api (providers + consumers).
type APIResolveResult struct {
	Operation graph.Node    `json:"operation"`
	RepoURI   string        `json:"repo_uri,omitempty"`
	Providers []ContractHit `json:"providers"`
	Consumers []ContractHit `json:"consumers"`
	Warnings  []string      `json:"warnings,omitempty"`
}

// ListProviders returns symbols that implement the given operation
// (incoming implements edges).
func ListProviders(store graph.Store, opID graph.NodeID) ([]ContractHit, error) {
	return listContractHits(store, opID, parse.EdgeImplements)
}

// ListConsumers returns symbols that consume the given operation
// (incoming consumes edges).
func ListConsumers(store graph.Store, opID graph.NodeID) ([]ContractHit, error) {
	return listContractHits(store, opID, parse.EdgeConsumes)
}

// ResolveAPI finds a contract operation by query and returns its providers and consumers.
// Query may be a repo:// URI, Phase-1 id, unique name, operationId, path, or grpc_path.
func ResolveAPI(store graph.Store, query string) (APIResolveResult, error) {
	var out APIResolveResult
	op, err := resolveOperation(store, query)
	if err != nil {
		return out, err
	}
	out.Operation = op
	out.RepoURI = repoURIOf(op)
	out.Providers, err = ListProviders(store, op.ID)
	if err != nil {
		return out, err
	}
	out.Consumers, err = ListConsumers(store, op.ID)
	if err != nil {
		return out, err
	}
	return out, nil
}

func listContractHits(store graph.Store, opID graph.NodeID, edgeType graph.EdgeType) ([]ContractHit, error) {
	edges, err := store.InEdges(opID, edgeType)
	if err != nil {
		return nil, err
	}
	hits := make([]ContractHit, 0, len(edges))
	for _, e := range edges {
		n, err := store.GetNode(e.From)
		if err != nil {
			if err == graph.ErrNotFound {
				continue
			}
			return nil, err
		}
		match := ""
		if e.Props != nil {
			match = e.Props["match"]
		}
		hits = append(hits, ContractHit{
			Node:    n,
			RepoURI: repoURIOf(n),
			Match:   match,
			Note:    noteFromMatch(match),
			Edge:    edgeType,
		})
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].RepoURI != hits[j].RepoURI {
			return hits[i].RepoURI < hits[j].RepoURI
		}
		return hits[i].Node.ID < hits[j].Node.ID
	})
	return hits, nil
}

func resolveOperation(store graph.Store, query string) (graph.Node, error) {
	if query == "" {
		return graph.Node{}, fmt.Errorf("rank: empty api query")
	}

	if id, err := ResolveSeed(store, query); err == nil {
		n, err := store.GetNode(id)
		if err != nil {
			return graph.Node{}, err
		}
		if n.Kind == parse.KindOperation {
			return n, nil
		}
	}

	q := strings.TrimSpace(query)
	var matches []graph.Node
	err := store.ForEachNode(func(n graph.Node) bool {
		if n.Kind != parse.KindOperation {
			return true
		}
		if operationMatchesQuery(n, q) {
			matches = append(matches, n)
			if len(matches) >= 2 {
				return false
			}
		}
		return true
	})
	if err != nil {
		return graph.Node{}, err
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) == 0 {
		return graph.Node{}, fmt.Errorf("%w: operation %q", graph.ErrNotFound, query)
	}
	return graph.Node{}, fmt.Errorf("rank: ambiguous operation %q (multiple matches found); use a repo:// URI", query)
}

func operationMatchesQuery(n graph.Node, q string) bool {
	if n.Name == q || string(n.ID) == q {
		return true
	}
	if n.Props == nil {
		return false
	}
	if n.Props[uri.PropKey] == q {
		return true
	}
	for _, key := range []string{"operation_id", "path", "grpc_path", "method"} {
		if n.Props[key] == q {
			return true
		}
	}
	// Allow "GET /users" style against name or method+path.
	if n.Props["method"] != "" && n.Props["path"] != "" {
		combo := n.Props["method"] + " " + n.Props["path"]
		if combo == q {
			return true
		}
	}
	return false
}

func repoURIOf(n graph.Node) string {
	if n.Props == nil {
		return ""
	}
	return n.Props[uri.PropKey]
}

func noteFromMatch(match string) string {
	switch match {
	case "operation_id":
		return "matched via operationId / name fold heuristic"
	case "path_literal":
		return "matched via path/grpc_path string literal in source"
	default:
		return "contract edge"
	}
}

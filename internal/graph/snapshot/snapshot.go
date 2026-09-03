// Package snapshot streams graph shards as versioned NDJSON (SYN-16).
package snapshot

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"

	"github.com/taricsa/synapse/internal/graph"
	"github.com/taricsa/synapse/internal/uri"
)

const (
	// FormatID identifies Synapse graph snapshots.
	FormatID = "synapse.graph.snapshot"
	// Version is the only supported snapshot version.
	Version = 1

	KindRepo    = "repo"
	KindOverlay = "overlay"
)

// Meta describes the snapshot header.
type Meta struct {
	Repo string // logical repo:// name; empty for overlay
	Kind string // KindRepo or KindOverlay
}

type headerRecord struct {
	Type    string `json:"type"`
	Format  string `json:"format"`
	Version int    `json:"version"`
	Repo    string `json:"repo,omitempty"`
	Kind    string `json:"kind"`
}

type nodeRecord struct {
	Type  string            `json:"type"`
	ID    graph.NodeID      `json:"id"`
	Kind  string            `json:"kind"`
	Name  string            `json:"name,omitempty"`
	Path  string            `json:"path,omitempty"`
	Props map[string]string `json:"props,omitempty"`
}

type edgeRecord struct {
	Type     string            `json:"type"`
	From     graph.NodeID      `json:"from"`
	To       graph.NodeID      `json:"to"`
	EdgeType graph.EdgeType    `json:"edge_type"`
	Props    map[string]string `json:"props,omitempty"`
}

// Export writes a v1 NDJSON snapshot of store to w.
func Export(w io.Writer, store graph.Store, meta Meta) error {
	if store == nil {
		return fmt.Errorf("snapshot: nil store")
	}
	kind := meta.Kind
	if kind == "" {
		kind = KindRepo
	}
	if kind != KindRepo && kind != KindOverlay {
		return fmt.Errorf("snapshot: invalid kind %q", kind)
	}
	if kind == KindRepo && meta.Repo == "" {
		return fmt.Errorf("snapshot: repo name required for kind %q", KindRepo)
	}

	bw := bufio.NewWriter(w)
	enc := json.NewEncoder(bw)
	if err := enc.Encode(headerRecord{
		Type:    "header",
		Format:  FormatID,
		Version: Version,
		Repo:    meta.Repo,
		Kind:    kind,
	}); err != nil {
		return fmt.Errorf("snapshot: write header: %w", err)
	}

	// Two-pass streaming: nodes first, then edges. O(1) memory — no node slice.
	var writeErr error
	err := store.ForEachNode(func(n graph.Node) bool {
		rec := nodeRecord{
			Type:  "node",
			ID:    n.ID,
			Kind:  n.Kind,
			Name:  n.Name,
			Path:  n.Path,
			Props: n.Props,
		}
		if err := enc.Encode(rec); err != nil {
			writeErr = fmt.Errorf("snapshot: write node %s: %w", n.ID, err)
			return false
		}
		return true
	})
	if err != nil {
		return err
	}
	if writeErr != nil {
		return writeErr
	}

	err = store.ForEachNode(func(n graph.Node) bool {
		edges, err := store.OutEdges(n.ID, "")
		if err != nil {
			writeErr = fmt.Errorf("snapshot: out edges %s: %w", n.ID, err)
			return false
		}
		for _, e := range edges {
			erec := edgeRecord{
				Type:     "edge",
				From:     e.From,
				To:       e.To,
				EdgeType: e.Type,
				Props:    e.Props,
			}
			if err := enc.Encode(erec); err != nil {
				writeErr = fmt.Errorf("snapshot: write edge: %w", err)
				return false
			}
		}
		return true
	})
	if err != nil {
		return err
	}
	if writeErr != nil {
		return writeErr
	}

	if err := bw.Flush(); err != nil {
		return fmt.Errorf("snapshot: flush: %w", err)
	}
	return nil
}

// ImportResult summarizes a successful import.
type ImportResult struct {
	Meta       Meta
	Nodes      int
	Edges      int
	HeaderSeen bool
}

// ImportOptions control destination checks and optional repo_uri rewrite (SYN-94).
type ImportOptions struct {
	// TargetRepo is the --repo destination for KindRepo snapshots.
	// Empty skips the dest check (tests / overlay).
	TargetRepo string
	// RewriteRepo remaps header.Repo → TargetRepo on repo_uri and URI-keyed IDs.
	RewriteRepo bool
}

// Import streams a v1 NDJSON snapshot from r into store.
func Import(r io.Reader, store graph.Store) (ImportResult, error) {
	return ImportWithOptions(r, store, ImportOptions{})
}

// ImportWithOptions streams a v1 NDJSON snapshot with dest/rewrite options.
func ImportWithOptions(r io.Reader, store graph.Store, opts ImportOptions) (ImportResult, error) {
	var out ImportResult
	if store == nil {
		return out, fmt.Errorf("snapshot: nil store")
	}
	sc := bufio.NewScanner(r)
	// Large graphs / long props — allow bigger lines than default 64KiB.
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, 16*1024*1024)

	rewriteFrom, rewriteTo := "", ""
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var probe struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(line, &probe); err != nil {
			return out, fmt.Errorf("snapshot: line %d: %w", lineNo, err)
		}
		switch probe.Type {
		case "header":
			if out.HeaderSeen {
				return out, fmt.Errorf("snapshot: line %d: duplicate header", lineNo)
			}
			var h headerRecord
			if err := json.Unmarshal(line, &h); err != nil {
				return out, fmt.Errorf("snapshot: line %d header: %w", lineNo, err)
			}
			if h.Format != FormatID {
				return out, fmt.Errorf("snapshot: unsupported format %q", h.Format)
			}
			if h.Version != Version {
				return out, fmt.Errorf("snapshot: unsupported version %d (want %d)", h.Version, Version)
			}
			if h.Kind != KindRepo && h.Kind != KindOverlay {
				return out, fmt.Errorf("snapshot: invalid kind %q", h.Kind)
			}
			if h.Kind == KindRepo && h.Repo == "" {
				return out, fmt.Errorf("snapshot: header missing repo")
			}
			out.HeaderSeen = true
			out.Meta = Meta{Repo: h.Repo, Kind: h.Kind}
			if h.Kind == KindRepo && opts.TargetRepo != "" && h.Repo != opts.TargetRepo {
				if !opts.RewriteRepo {
					return out, fmt.Errorf("snapshot: repo %q does not match target %q (pass --rewrite-repo to remap repo_uri)", h.Repo, opts.TargetRepo)
				}
				rewriteFrom, rewriteTo = h.Repo, opts.TargetRepo
				out.Meta.Repo = opts.TargetRepo
			}
		case "node":
			if !out.HeaderSeen {
				return out, fmt.Errorf("snapshot: line %d: node before header", lineNo)
			}
			var n nodeRecord
			if err := json.Unmarshal(line, &n); err != nil {
				return out, fmt.Errorf("snapshot: line %d node: %w", lineNo, err)
			}
			if rewriteFrom != "" {
				id, err := rewriteNodeID(n.ID, rewriteFrom, rewriteTo)
				if err != nil {
					return out, fmt.Errorf("snapshot: line %d node id: %w", lineNo, err)
				}
				n.ID = id
				props, err := rewriteProps(n.Props, rewriteFrom, rewriteTo)
				if err != nil {
					return out, fmt.Errorf("snapshot: line %d node props: %w", lineNo, err)
				}
				n.Props = props
			}
			if err := store.PutNode(graph.Node{
				ID: n.ID, Kind: n.Kind, Name: n.Name, Path: n.Path, Props: n.Props,
			}); err != nil {
				return out, fmt.Errorf("snapshot: put node %s: %w", n.ID, err)
			}
			out.Nodes++
		case "edge":
			if !out.HeaderSeen {
				return out, fmt.Errorf("snapshot: line %d: edge before header", lineNo)
			}
			var e edgeRecord
			if err := json.Unmarshal(line, &e); err != nil {
				return out, fmt.Errorf("snapshot: line %d edge: %w", lineNo, err)
			}
			// Accept legacy "type" field name for edge relationship if edge_type empty.
			et := e.EdgeType
			if et == "" {
				var legacy struct {
					Rel graph.EdgeType `json:"rel"`
				}
				_ = json.Unmarshal(line, &legacy)
				et = legacy.Rel
			}
			if et == "" {
				return out, fmt.Errorf("snapshot: line %d: edge missing edge_type", lineNo)
			}
			if rewriteFrom != "" {
				from, err := rewriteNodeID(e.From, rewriteFrom, rewriteTo)
				if err != nil {
					return out, fmt.Errorf("snapshot: line %d edge from: %w", lineNo, err)
				}
				to, err := rewriteNodeID(e.To, rewriteFrom, rewriteTo)
				if err != nil {
					return out, fmt.Errorf("snapshot: line %d edge to: %w", lineNo, err)
				}
				props, err := rewriteProps(e.Props, rewriteFrom, rewriteTo)
				if err != nil {
					return out, fmt.Errorf("snapshot: line %d edge props: %w", lineNo, err)
				}
				e.From, e.To, e.Props = from, to, props
			}
			if err := store.PutEdge(graph.Edge{
				From: e.From, To: e.To, Type: et, Props: e.Props,
			}); err != nil {
				return out, fmt.Errorf("snapshot: put edge %s→%s: %w", e.From, e.To, err)
			}
			out.Edges++
		default:
			return out, fmt.Errorf("snapshot: line %d: unknown type %q", lineNo, probe.Type)
		}
	}
	if err := sc.Err(); err != nil {
		return out, fmt.Errorf("snapshot: read: %w", err)
	}
	if !out.HeaderSeen {
		return out, fmt.Errorf("snapshot: missing header")
	}
	return out, nil
}

func rewriteNodeID(id graph.NodeID, from, to string) (graph.NodeID, error) {
	s, err := uri.RewriteRepo(string(id), from, to)
	if err != nil {
		return "", err
	}
	return graph.NodeID(s), nil
}

func rewriteProps(props map[string]string, from, to string) (map[string]string, error) {
	if len(props) == 0 {
		return props, nil
	}
	out := make(map[string]string, len(props))
	for k, v := range props {
		nv, err := uri.RewriteRepo(v, from, to)
		if err != nil {
			return nil, err
		}
		out[k] = nv
	}
	return out, nil
}

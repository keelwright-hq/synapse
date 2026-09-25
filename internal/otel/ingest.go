// Package otel ingests OpenTelemetry traces into observed_calls edges (SYN-19).
package otel

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/keelwright-hq/synapse/internal/graph"
	"github.com/keelwright-hq/synapse/internal/parse"
	"github.com/keelwright-hq/synapse/internal/uri"
)

// Span is a minimal span record for file import (OTLP JSON subset).
type Span struct {
	Name       string            `json:"name"`
	Service    string            `json:"service,omitempty"`
	Attributes map[string]string `json:"attributes,omitempty"`
	Parent     string            `json:"parent,omitempty"` // parent span name within the same file
}

// TraceFile is the on-disk import format.
type TraceFile struct {
	Spans []Span `json:"spans"`
}

// ImportFile reads a Synapse OTLP-ish JSON trace file.
func ImportFile(path string) ([]Span, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var tf TraceFile
	if err := json.Unmarshal(data, &tf); err != nil {
		return nil, fmt.Errorf("otel import: %w", err)
	}
	return tf.Spans, nil
}

// Resolve maps a span to a graph node using attribute heuristics.
func Resolve(store graph.Store, sp Span) (graph.NodeID, bool) {
	attrs := sp.Attributes
	if attrs == nil {
		attrs = map[string]string{}
	}
	candidates := []string{
		attrs["code.function"],
		attrs["code.filepath"],
		attrs["rpc.method"],
		sp.Name,
	}
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if strings.HasPrefix(c, uri.Scheme+"://") {
			if n, err := store.GetNodeByURI(c); err == nil {
				return n.ID, true
			}
		}
		if n, err := store.GetNode(graph.NodeID(c)); err == nil {
			return n.ID, true
		}
		// Try file:path
		if strings.Contains(c, "/") || strings.Contains(c, ".") {
			if n, err := store.GetNode(graph.NodeID("file:" + filepath.ToSlash(c))); err == nil {
				return n.ID, true
			}
		}
		// Prefer function/method over package/file when names collide.
		var matches []graph.Node
		_ = store.ForEachNode(func(n graph.Node) bool {
			if n.Name == c || strings.HasSuffix(string(n.ID), "#"+c) {
				matches = append(matches, n)
			}
			return true
		})
		if len(matches) == 1 {
			return matches[0].ID, true
		}
		if len(matches) > 1 {
			var preferred []graph.Node
			for _, n := range matches {
				if n.Kind == parse.KindFunction || n.Kind == parse.KindMethod {
					preferred = append(preferred, n)
				}
			}
			if len(preferred) == 1 {
				return preferred[0].ID, true
			}
		}
	}
	return "", false
}

// Ingest upserts observed_calls edges for parent→child span pairs that resolve.
func Ingest(store graph.Store, spans []Span) (int, error) {
	byName := map[string]Span{}
	for _, sp := range spans {
		byName[sp.Name] = sp
	}
	now := time.Now().UTC().Format(time.RFC3339)
	n := 0
	for _, sp := range spans {
		if sp.Parent == "" {
			continue
		}
		parent, ok := byName[sp.Parent]
		if !ok {
			continue
		}
		fromID, ok := Resolve(store, parent)
		if !ok {
			continue
		}
		toID, ok := Resolve(store, sp)
		if !ok {
			continue
		}
		if fromID == toID {
			continue
		}
		count := 1
		existing, err := store.OutEdges(fromID, parse.EdgeObservedCalls)
		if err != nil {
			return n, err
		}
		for _, e := range existing {
			if e.To == toID {
				if c, err := strconv.Atoi(e.Props[parse.PropCount]); err == nil {
					count = c + 1
				}
				break
			}
		}
		edge := graph.Edge{
			From: fromID,
			To:   toID,
			Type: parse.EdgeObservedCalls,
			Props: map[string]string{
				parse.PropProvenance: parse.ProvenanceObserved,
				parse.PropCount:      strconv.Itoa(count),
				parse.PropLastSeen:   now,
			},
		}
		if err := store.PutEdge(edge); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

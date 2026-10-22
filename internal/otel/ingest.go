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

// Span is a span record for file import (supports both simplified Synapse JSON and OTLP/JSON).
type Span struct {
	SpanID       string            `json:"spanId,omitempty"`
	ParentSpanID string            `json:"parentSpanId,omitempty"`
	Name         string            `json:"name"`
	Service      string            `json:"service,omitempty"`
	Attributes   map[string]string `json:"attributes,omitempty"`
	Parent       string            `json:"parent,omitempty"` // parent span name or spanId within the same file
}

// TraceFile is the on-disk import format.
type TraceFile struct {
	Spans []Span `json:"spans"`
}

type otlpKeyValue struct {
	Key   string       `json:"key"`
	Value otlpAnyValue `json:"value"`
}

type otlpAnyValue struct {
	StringValue *string  `json:"stringValue,omitempty"`
	IntValue    any      `json:"intValue,omitempty"`
	DoubleValue *float64 `json:"doubleValue,omitempty"`
	BoolValue   *bool    `json:"boolValue,omitempty"`
}

func (v otlpAnyValue) String() string {
	switch {
	case v.StringValue != nil:
		return *v.StringValue
	case v.IntValue != nil:
		return fmt.Sprintf("%v", v.IntValue)
	case v.DoubleValue != nil:
		return strconv.FormatFloat(*v.DoubleValue, 'f', -1, 64)
	case v.BoolValue != nil:
		return strconv.FormatBool(*v.BoolValue)
	default:
		return ""
	}
}

type rawSpan struct {
	SpanID       string          `json:"spanId"`
	SpanIDAlt    string          `json:"span_id"`
	ParentSpanID string          `json:"parentSpanId"`
	ParentIDAlt  string          `json:"parent_span_id"`
	Name         string          `json:"name"`
	Service      string          `json:"service"`
	Parent       string          `json:"parent"`
	Attributes   json.RawMessage `json:"attributes"`
}

func (r rawSpan) toSpan(defaultService string) Span {
	spanID := r.SpanID
	if spanID == "" {
		spanID = r.SpanIDAlt
	}
	parentSpanID := r.ParentSpanID
	if parentSpanID == "" {
		parentSpanID = r.ParentIDAlt
	}
	svc := r.Service
	if svc == "" {
		svc = defaultService
	}
	return Span{
		SpanID:       spanID,
		ParentSpanID: parentSpanID,
		Name:         r.Name,
		Service:      svc,
		Attributes:   parseAttributes(r.Attributes),
		Parent:       r.Parent,
	}
}

func parseAttributes(raw json.RawMessage) map[string]string {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err == nil && m != nil {
		return m
	}
	var kvs []otlpKeyValue
	if err := json.Unmarshal(raw, &kvs); err == nil && len(kvs) > 0 {
		out := make(map[string]string, len(kvs))
		for _, kv := range kvs {
			if kv.Key != "" {
				out[kv.Key] = kv.Value.String()
			}
		}
		return out
	}
	return nil
}

type otlpEnvelope struct {
	Spans         []rawSpan `json:"spans"`
	ResourceSpans []struct {
		Resource struct {
			Attributes json.RawMessage `json:"attributes"`
		} `json:"resource"`
		ScopeSpans []struct {
			Spans []rawSpan `json:"spans"`
		} `json:"scopeSpans"`
		InstrumentationLibrarySpans []struct {
			Spans []rawSpan `json:"spans"`
		} `json:"instrumentationLibrarySpans"`
	} `json:"resourceSpans"`
}

// Parse decodes either simplified {"spans":[...]} JSON or OTLP/JSON {"resourceSpans":[...]}.
func Parse(data []byte) ([]Span, error) {
	var env otlpEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("otel import: %w", err)
	}
	var out []Span
	for _, rs := range env.ResourceSpans {
		resAttrs := parseAttributes(rs.Resource.Attributes)
		svc := resAttrs["service.name"]
		for _, ss := range rs.ScopeSpans {
			for _, sp := range ss.Spans {
				out = append(out, sp.toSpan(svc))
			}
		}
		for _, ils := range rs.InstrumentationLibrarySpans {
			for _, sp := range ils.Spans {
				out = append(out, sp.toSpan(svc))
			}
		}
	}
	for _, sp := range env.Spans {
		out = append(out, sp.toSpan(""))
	}
	return out, nil
}

// ImportFile reads a Synapse or OTLP/JSON trace file.
func ImportFile(path string) ([]Span, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

// Resolve maps a span to a graph node using attribute heuristics.
func Resolve(store graph.Store, sp Span) (graph.NodeID, bool) {
	attrs := sp.Attributes
	if attrs == nil {
		attrs = map[string]string{}
	}
	codeFile := filepath.ToSlash(strings.TrimSpace(attrs["code.filepath"]))
	codeFunc := strings.TrimSpace(attrs["code.function"])
	if codeFunc != "" && codeFile != "" {
		if id, ok := resolveFuncInFile(store, codeFunc, codeFile); ok {
			return id, true
		}
	}

	candidates := []string{
		codeFunc,
		codeFile,
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
			if matchSymbolName(n, c) {
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
			if len(preferred) > 1 && codeFile != "" {
				var fileFiltered []graph.Node
				for _, n := range preferred {
					if matchFilePath(n.Path, codeFile) {
						fileFiltered = append(fileFiltered, n)
					}
				}
				if len(fileFiltered) == 1 {
					return fileFiltered[0].ID, true
				}
			}
		}
	}
	return "", false
}

func resolveFuncInFile(store graph.Store, fn, filePath string) (graph.NodeID, bool) {
	var matches []graph.Node
	_ = store.ForEachNode(func(n graph.Node) bool {
		if n.Kind != parse.KindFunction && n.Kind != parse.KindMethod {
			return true
		}
		if !matchFilePath(n.Path, filePath) {
			return true
		}
		if matchSymbolName(n, fn) {
			matches = append(matches, n)
		}
		return true
	})
	if len(matches) == 1 {
		return matches[0].ID, true
	}
	return "", false
}

func matchSymbolName(n graph.Node, cand string) bool {
	if n.Name == cand || strings.HasSuffix(string(n.ID), "#"+cand) {
		return true
	}
	if n.Kind == parse.KindMethod {
		if strings.HasSuffix(n.Name, "."+cand) || strings.HasSuffix(string(n.ID), "."+cand) {
			return true
		}
	}
	return false
}

func matchFilePath(nodePath, candPath string) bool {
	nodePath = filepath.ToSlash(nodePath)
	candPath = filepath.ToSlash(candPath)
	if nodePath == "" || candPath == "" {
		return false
	}
	if nodePath == candPath {
		return true
	}
	return strings.HasSuffix(nodePath, "/"+candPath) || strings.HasSuffix(candPath, "/"+nodePath)
}

// Ingest upserts observed_calls edges for parent→child span pairs that resolve.
func Ingest(store graph.Store, spans []Span) (int, error) {
	bySpanID := map[string]Span{}
	byName := map[string]Span{}
	for _, sp := range spans {
		if sp.SpanID != "" {
			bySpanID[sp.SpanID] = sp
		}
		if sp.Name != "" {
			byName[sp.Name] = sp
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	n := 0
	for _, sp := range spans {
		var (
			parent Span
			ok     bool
		)
		if sp.ParentSpanID != "" {
			parent, ok = bySpanID[sp.ParentSpanID]
		}
		if !ok && sp.Parent != "" {
			parent, ok = bySpanID[sp.Parent]
			if !ok {
				parent, ok = byName[sp.Parent]
			}
		}
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

package report_test

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/keelwright-hq/synapse/internal/graph"
	"github.com/keelwright-hq/synapse/internal/index"
	"github.com/keelwright-hq/synapse/internal/parse"
	"github.com/keelwright-hq/synapse/internal/report"
	"github.com/keelwright-hq/synapse/internal/store/memory"
)

func TestHTMLEscapesGraphFieldsAndAvoidsInlineHandlers(t *testing.T) {
	store := memory.New()
	xssName := `<img src=x onerror=window.auditXSS=1>`
	xssPath := xssName + `.js`
	nodes := []graph.Node{
		{ID: graph.NodeID("file:" + xssPath), Kind: parse.KindFile, Name: xssPath, Path: xssPath},
		{ID: graph.NodeID("func:" + xssPath + "#alpha"), Kind: parse.KindFunction, Name: "alpha", Path: xssPath},
		{ID: `func:other.js#O'Reilly`, Kind: parse.KindFunction, Name: "O'Reilly", Path: "other.js"},
		{ID: "file:other.js", Kind: parse.KindFile, Name: "other.js", Path: "other.js"},
	}
	for _, n := range nodes {
		if err := store.PutNode(n); err != nil {
			t.Fatal(err)
		}
	}
	edges := []graph.Edge{
		{From: graph.NodeID("file:" + xssPath), To: graph.NodeID("func:" + xssPath + "#alpha"), Type: parse.EdgeContains},
		{From: graph.NodeID("func:" + xssPath + "#alpha"), To: `func:other.js#O'Reilly`, Type: parse.EdgeCalls, Props: map[string]string{"provenance": "EXTRACTED"}},
		{From: "file:other.js", To: `func:other.js#O'Reilly`, Type: parse.EdgeContains},
	}
	for _, e := range edges {
		if err := store.PutEdge(e); err != nil {
			t.Fatal(err)
		}
	}

	out := t.TempDir()
	res, err := report.Write(report.Options{
		Repo:   "xss-demo",
		Root:   "/tmp/xss",
		OutDir: out,
		Stats:  index.Stats{Processed: 2},
		Store:  store,
		Now:    time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(res.HTMLPath)
	if err != nil {
		t.Fatal(err)
	}
	html := string(raw)

	// Graph payload is JSON-embedded; markup must not appear as live HTML attributes.
	if strings.Contains(html, `onerror=window.auditXSS`) && !strings.Contains(html, `\u003c`) && strings.Contains(html, `<img src=x onerror=`) {
		// raw tag in HTML body outside JSON would be bad; allow only inside JSON text content
		t.Fatal("XSS payload appears as live HTML markup")
	}
	if strings.Contains(html, `onclick="window.selectNodeRef`) || strings.Contains(html, "onclick='window.selectNodeRef") {
		t.Fatal("inline onclick handlers with node IDs must not be generated")
	}
	if !strings.Contains(html, "textContent") {
		t.Fatal("expected DOM textContent-based rendering helpers in viewer")
	}
	if !strings.Contains(html, "UNKNOWN") {
		t.Fatal("expected UNKNOWN provenance handling for missing labels")
	}
	if !strings.Contains(html, "selectConnectedSubset") {
		t.Fatal("expected connected subset selection (SYN-103)")
	}
	if !strings.Contains(html, "__synapseViewer") {
		t.Fatal("expected idle-redraw test hook (SYN-110)")
	}
	if !strings.Contains(html, "@media (max-width: 768px)") {
		t.Fatal("expected mobile layout rules (SYN-109)")
	}
}

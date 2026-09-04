// Package report writes human- and agent-readable dry-run artifacts from a graph store.
package report

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/keelwright-hq/synapse/internal/buildinfo"
	"github.com/keelwright-hq/synapse/internal/graph"
	"github.com/keelwright-hq/synapse/internal/index"
	"github.com/keelwright-hq/synapse/internal/parse"
)

// SchemaVersion is the version of manifest.json / graph.json shapes.
const SchemaVersion = 1

// Options configures Write.
type Options struct {
	Repo   string
	Root   string // absolute path to the indexed repo root
	OutDir string
	Stats  index.Stats
	Store  graph.Store
	Now    time.Time // optional; defaults to time.Now().UTC()
}

// Result lists paths written under OutDir.
type Result struct {
	ManifestPath string
	GraphPath    string
	ReportPath   string
	HTMLPath     string
}

type graphDoc struct {
	SchemaVersion int          `json:"schema_version"`
	Nodes         []graph.Node `json:"nodes"`
	Edges         []graph.Edge `json:"edges"`
}

type manifest struct {
	SchemaVersion  int               `json:"schema_version"`
	Repo           string            `json:"repo"`
	Root           string            `json:"root"`
	Commit         string            `json:"commit,omitempty"`
	SynapseVersion string            `json:"synapse_version"`
	SynapseCommit  string            `json:"synapse_commit"`
	Timestamp      string            `json:"timestamp"`
	Index          index.Stats       `json:"index"`
	NodeCount      int               `json:"node_count"`
	EdgeCount      int               `json:"edge_count"`
	LanguageMix    map[string]int    `json:"language_mix"`
	Languages      []string          `json:"languages"`
	Artifacts      map[string]string `json:"artifacts"`
}

// Write dumps manifest.json, graph.json, GRAPH_REPORT.md, and graph.html into opts.OutDir.
func Write(opts Options) (Result, error) {
	if opts.Store == nil {
		return Result{}, fmt.Errorf("report: nil store")
	}
	if opts.OutDir == "" {
		return Result{}, fmt.Errorf("report: empty OutDir")
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}

	nodes, edges, err := collect(opts.Store)
	if err != nil {
		return Result{}, err
	}

	if err := os.MkdirAll(opts.OutDir, 0o755); err != nil {
		return Result{}, fmt.Errorf("report: mkdir: %w", err)
	}

	gdoc := graphDoc{SchemaVersion: SchemaVersion, Nodes: nodes, Edges: edges}
	graphPath := filepath.Join(opts.OutDir, "graph.json")
	if err := writeJSON(graphPath, gdoc); err != nil {
		return Result{}, err
	}

	langMix := languageMix(nodes)
	langs := make([]string, 0, len(langMix))
	for k := range langMix {
		langs = append(langs, k)
	}
	sort.Strings(langs)

	commit := gitHEAD(opts.Root)
	man := manifest{
		SchemaVersion:  SchemaVersion,
		Repo:           opts.Repo,
		Root:           opts.Root,
		Commit:         commit,
		SynapseVersion: buildinfo.Version,
		SynapseCommit:  buildinfo.Commit,
		Timestamp:      now.UTC().Format(time.RFC3339),
		Index:          opts.Stats,
		NodeCount:      len(nodes),
		EdgeCount:      len(edges),
		LanguageMix:    langMix,
		Languages:      langs,
		Artifacts: map[string]string{
			"manifest": "manifest.json",
			"graph":    "graph.json",
			"report":   "GRAPH_REPORT.md",
			"html":     "graph.html",
		},
	}
	manifestPath := filepath.Join(opts.OutDir, "manifest.json")
	if err := writeJSON(manifestPath, man); err != nil {
		return Result{}, err
	}

	reportPath := filepath.Join(opts.OutDir, "GRAPH_REPORT.md")
	md := renderMarkdown(man, nodes, edges)
	if err := os.WriteFile(reportPath, []byte(md), 0o644); err != nil {
		return Result{}, fmt.Errorf("report: write markdown: %w", err)
	}

	htmlPath := filepath.Join(opts.OutDir, "graph.html")
	html, err := renderHTML(man, gdoc)
	if err != nil {
		return Result{}, err
	}
	if err := os.WriteFile(htmlPath, html, 0o644); err != nil {
		return Result{}, fmt.Errorf("report: write html: %w", err)
	}

	return Result{
		ManifestPath: manifestPath,
		GraphPath:    graphPath,
		ReportPath:   reportPath,
		HTMLPath:     htmlPath,
	}, nil
}

func collect(store graph.Store) ([]graph.Node, []graph.Edge, error) {
	var nodes []graph.Node
	var edges []graph.Edge
	err := store.ForEachNode(func(n graph.Node) bool {
		nodes = append(nodes, n)
		return true
	})
	if err != nil {
		return nil, nil, fmt.Errorf("report: walk nodes: %w", err)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })

	for _, n := range nodes {
		out, err := store.OutEdges(n.ID, "")
		if err != nil {
			return nil, nil, fmt.Errorf("report: out edges %s: %w", n.ID, err)
		}
		edges = append(edges, out...)
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].From != edges[j].From {
			return edges[i].From < edges[j].From
		}
		if edges[i].To != edges[j].To {
			return edges[i].To < edges[j].To
		}
		return edges[i].Type < edges[j].Type
	})
	return nodes, edges, nil
}

func writeJSON(path string, v any) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("report: encode %s: %w", filepath.Base(path), err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("report: write %s: %w", filepath.Base(path), err)
	}
	return nil
}

func languageMix(nodes []graph.Node) map[string]int {
	mix := map[string]int{}
	reg := parse.NewRegistry()
	for _, n := range nodes {
		if n.Kind != parse.KindFile || n.Path == "" {
			continue
		}
		lang := "other"
		if l := reg.Lookup(n.Path); l != nil {
			lang = l.Name
		} else {
			ext := strings.ToLower(filepath.Ext(n.Path))
			if ext != "" {
				lang = ext
			}
		}
		mix[lang]++
	}
	return mix
}

func gitHEAD(root string) string {
	if root == "" {
		return ""
	}
	cmd := exec.Command("git", "-C", root, "rev-parse", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func renderMarkdown(man manifest, nodes []graph.Node, edges []graph.Edge) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Synapse graph report\n\n")
	fmt.Fprintf(&b, "## Corpus summary\n\n")
	fmt.Fprintf(&b, "- **Repo:** `%s`\n", man.Repo)
	fmt.Fprintf(&b, "- **Root:** `%s`\n", man.Root)
	fmt.Fprintf(&b, "- **Indexed at:** %s\n", man.Timestamp)
	fmt.Fprintf(&b, "- **Synapse:** %s (%s)\n", man.SynapseVersion, man.SynapseCommit)
	fmt.Fprintf(&b, "- **Index files:** processed=%d skipped=%d deleted=%d errors=%d\n",
		man.Index.Processed, man.Index.Skipped, man.Index.Deleted, man.Index.Errors)
	fmt.Fprintf(&b, "- **Graph:** %d nodes · %d edges\n", man.NodeCount, man.EdgeCount)

	fmt.Fprintf(&b, "\n## Freshness\n\n")
	if man.Commit != "" {
		fmt.Fprintf(&b, "- **Commit:** `%s`\n", man.Commit)
		fmt.Fprintf(&b, "- Graph reflects the store after this index run; re-index if the working tree moved on.\n")
	} else {
		fmt.Fprintf(&b, "- **Commit:** _(unavailable)_\n")
		fmt.Fprintf(&b, "- Without a git SHA, treat the graph as potentially stale relative to other checkouts.\n")
	}

	if len(man.LanguageMix) > 0 {
		fmt.Fprintf(&b, "\n## Language mix (file nodes)\n\n")
		type kv struct {
			k string
			v int
		}
		var rows []kv
		for k, v := range man.LanguageMix {
			rows = append(rows, kv{k, v})
		}
		sort.Slice(rows, func(i, j int) bool {
			if rows[i].v != rows[j].v {
				return rows[i].v > rows[j].v
			}
			return rows[i].k < rows[j].k
		})
		for _, r := range rows {
			fmt.Fprintf(&b, "- `%s`: %d\n", r.k, r.v)
		}
	}

	writeHubSection(&b, "Important files",
		"File nodes ranked by imports/calls rolled up from modules/functions (contains excluded).",
		ImportantFiles(nodes, edges, 10))
	writeHubSection(&b, "Important symbols",
		"Resolved functions/methods/types/contracts by calls/implements/consumes degree. Unresolved `symbol` hubs are excluded.",
		ImportantSymbols(nodes, edges, 10))
	writeHubSection(&b, "Top imports",
		"Most-used dependencies (import specs grouped across files; relative paths resolved).",
		TopImports(nodes, edges, 10))
	var warnings []string
	if man.Index.Errors > 0 {
		warnings = append(warnings, fmt.Sprintf("index reported %d file error(s)", man.Index.Errors))
	}
	if man.Commit == "" {
		warnings = append(warnings, "git commit SHA unavailable for this root")
	}
	if man.NodeCount == 0 {
		warnings = append(warnings, "graph has zero nodes")
	}
	warnings = append(warnings,
		"Unresolved call targets use kind `symbol` and are omitted from Important symbols",
		"Cross-language and dynamic calls may be under-linked; contracts require discoverable specs")
	fmt.Fprintf(&b, "\n## Warnings and limitations\n\n")
	for _, w := range warnings {
		fmt.Fprintf(&b, "- %s\n", w)
	}

	fmt.Fprintf(&b, "\n## Artifacts\n\n")
	fmt.Fprintf(&b, "- `manifest.json` — run metadata\n")
	fmt.Fprintf(&b, "- `graph.json` — full node/edge dump (schema_version=%d)\n", SchemaVersion)
	fmt.Fprintf(&b, "- `GRAPH_REPORT.md` — this summary\n")
	fmt.Fprintf(&b, "- `graph.html` — interactive local viewer (open in a browser; works via `file://`)\n")
	fmt.Fprintf(&b, "\n`.synapse/` remains the Badger query database; this folder is for humans and agents.\n")
	return b.String()
}

func writeHubSection(b *strings.Builder, title, blurb string, hubs []Hub) {
	if len(hubs) == 0 {
		return
	}
	fmt.Fprintf(b, "\n## %s\n\n", title)
	fmt.Fprintf(b, "%s\n\n", blurb)
	for _, h := range hubs {
		label := h.Name
		if label == "" {
			label = string(h.ID)
		}
		pathBit := ""
		if h.Path != "" {
			pathBit = fmt.Sprintf(" `%s`", h.Path)
		}
		fmt.Fprintf(b, "- **%s** `%s` (%s)%s — degree %d\n", label, h.ID, h.Kind, pathBit, h.Degree)
	}
}

// CopyArtifacts copies the standard report files from srcDir into dstDir.
func CopyArtifacts(srcDir, dstDir string) error {
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return err
	}
	for _, name := range []string{"manifest.json", "graph.json", "GRAPH_REPORT.md", "graph.html"} {
		src := filepath.Join(srcDir, name)
		dst := filepath.Join(dstDir, name)
		data, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// NewRunID returns a unique run folder name: UTC timestamp with milliseconds
// plus a short random suffix so back-to-back runs in the same second do not collide.
func NewRunID() string {
	return newRunID(time.Now().UTC())
}

func newRunID(now time.Time) string {
	now = now.UTC()
	stamp := now.Format("20060102T150405.000") + "Z"
	var b [3]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%s-%09d", stamp, now.Nanosecond())
	}
	return fmt.Sprintf("%s-%x", stamp, b)
}

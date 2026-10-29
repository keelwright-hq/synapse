// Package docs indexes Markdown/ADR files and links them to code symbols (SYN-20).
package docs

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/keelwright-hq/synapse/internal/graph"
	"github.com/keelwright-hq/synapse/internal/parse"
	"github.com/keelwright-hq/synapse/internal/uri"
)

var (
	headingRe  = regexp.MustCompile(`^(#{1,6})\s+(.+)$`)
	backtickRe = regexp.MustCompile("`([^`]+)`")
	pathRe     = regexp.MustCompile(`(?:^|[\s(])([A-Za-z0-9_./-]+\.(?:go|ts|tsx|js|jsx|py|swift|md))`)
	adrIDRe    = regexp.MustCompile(`(?i)\bADR[- ]?(\d+)\b`)
)

// ListDocFiles returns absolute paths of Markdown docs under root.
func ListDocFiles(root string, ignoreDirNames []string) ([]string, error) {
	var files []string
	err := parse.WalkFiles(root, ignoreDirNames, func(path string, d fs.DirEntry) error {
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".md" && ext != ".markdown" {
			return nil
		}
		files = append(files, path)
		return nil
	})
	return files, err
}

// ParseFile maps a markdown file into parse IR (doc + heading nodes).
func ParseFile(absPath, relPath string) (parse.Result, error) {
	relPath = filepath.ToSlash(relPath)
	data, err := os.ReadFile(absPath)
	if err != nil {
		return parse.Result{}, err
	}
	fid := graph.NodeID("file:" + relPath)
	docName := strings.TrimSuffix(filepath.Base(relPath), filepath.Ext(relPath))
	docID := graph.NodeID(fmt.Sprintf("doc:%s#%s", relPath, docName))
	out := parse.Result{
		Path: relPath,
		Lang: "markdown",
		Nodes: []graph.Node{
			{ID: fid, Kind: parse.KindFile, Name: filepath.Base(relPath), Path: relPath},
			{ID: docID, Kind: parse.KindDoc, Name: docName, Path: relPath},
		},
		Edges: []graph.Edge{{From: fid, To: docID, Type: parse.EdgeContains}},
	}
	sc := bufio.NewScanner(strings.NewReader(string(data)))
	line := 0
	seenTitles := map[string]int{}
	for sc.Scan() {
		line++
		m := headingRe.FindStringSubmatch(sc.Text())
		if m == nil {
			continue
		}
		title := strings.TrimSpace(m[2])
		seenTitles[title]++
		sym := title
		if seenTitles[title] > 1 {
			sym = fmt.Sprintf("%s~%d", title, seenTitles[title])
		}
		hid := graph.NodeID(fmt.Sprintf("heading:%s#%s", relPath, sym))
		out.Nodes = append(out.Nodes, graph.Node{
			ID: hid, Kind: parse.KindHeading, Name: title, Path: relPath,
			Props: map[string]string{
				"start_line": fmt.Sprintf("%d", line),
				"end_line":   fmt.Sprintf("%d", line),
				"level":      fmt.Sprintf("%d", len(m[1])),
			},
		})
		out.Edges = append(out.Edges, graph.Edge{From: docID, To: hid, Type: parse.EdgeContains})
	}
	if line > 0 {
		out.Nodes[1].Props = map[string]string{
			"start_line": "1",
			"end_line":   fmt.Sprintf("%d", line),
		}
	}
	out.Normalize()
	return out, sc.Err()
}

// LinkOptions configure the post-index documentation linker.
type LinkOptions struct {
	Root           string
	Store          graph.Store
	Repo           string
	IgnoreDirNames []string
}

// Link scans indexed markdown file contents and creates documents edges to code.
func Link(opts LinkOptions) (int, error) {
	if opts.Store == nil || opts.Root == "" {
		return 0, fmt.Errorf("docs link: store and root required")
	}
	if err := clearDocEdges(opts.Store); err != nil {
		return 0, err
	}
	files, err := ListDocFiles(opts.Root, opts.IgnoreDirNames)
	if err != nil {
		return 0, err
	}
	if len(files) == 0 {
		return 0, nil
	}
	byName, err := buildSymbolNameIndex(opts.Store)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, abs := range files {
		rel, err := filepath.Rel(opts.Root, abs)
		if err != nil {
			continue
		}
		rel = filepath.ToSlash(rel)
		docName := strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel))
		docID := graph.NodeID(fmt.Sprintf("doc:%s#%s", rel, docName))
		if _, err := opts.Store.GetNode(docID); err != nil {
			// Prefer file node as from if doc missing.
			docID = graph.NodeID("file:" + rel)
			if _, err := opts.Store.GetNode(docID); err != nil {
				continue
			}
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			continue
		}
		targets := map[graph.NodeID]int{}
		recordTarget := func(id graph.NodeID, lineNo int) {
			if _, exists := targets[id]; !exists {
				targets[id] = lineNo
			}
		}
		sc := bufio.NewScanner(strings.NewReader(string(data)))
		lineNo := 0
		for sc.Scan() {
			lineNo++
			lineText := sc.Text()
			for _, m := range backtickRe.FindAllStringSubmatch(lineText, -1) {
				if id, ok := resolveMention(opts.Store, byName, m[1]); ok {
					recordTarget(id, lineNo)
				}
			}
			for _, m := range pathRe.FindAllStringSubmatch(lineText, -1) {
				p := filepath.ToSlash(m[1])
				fid := graph.NodeID("file:" + p)
				if _, err := opts.Store.GetNode(fid); err == nil {
					recordTarget(fid, lineNo)
				}
			}
			for _, m := range adrIDRe.FindAllStringSubmatch(lineText, -1) {
				// ADR cross-refs within docs; link same-repo ADR files if present
				cand := filepath.ToSlash(filepath.Join("docs", "adr", fmt.Sprintf("%s.md", m[1])))
				fid := graph.NodeID("file:" + cand)
				if _, err := opts.Store.GetNode(fid); err == nil {
					recordTarget(fid, lineNo)
				}
			}
		}
		for to, mentionLine := range targets {
			if to == docID {
				continue
			}
			props := map[string]string{
				parse.PropProvenance: parse.ProvenanceInferred,
			}
			if mentionLine > 0 {
				props["start_line"] = fmt.Sprintf("%d", mentionLine)
				props["end_line"] = fmt.Sprintf("%d", mentionLine)
			}
			edge := graph.Edge{
				From:  docID,
				To:    to,
				Type:  parse.EdgeDocuments,
				Props: props,
			}
			if err := opts.Store.PutEdge(edge); err != nil {
				return n, err
			}
			n++
		}
		_ = uri.PropKey
	}
	return n, nil
}

func clearDocEdges(s graph.Store) error {
	var ids []graph.NodeID
	err := s.ForEachNode(func(n graph.Node) bool {
		ids = append(ids, n.ID)
		return true
	})
	if err != nil {
		return err
	}
	for _, id := range ids {
		edges, err := s.OutEdges(id, parse.EdgeDocuments)
		if err != nil {
			return err
		}
		for _, e := range edges {
			if err := s.DeleteEdge(e.From, e.To, e.Type); err != nil && err != graph.ErrNotFound {
				return err
			}
		}
	}
	return nil
}

func buildSymbolNameIndex(store graph.Store) (map[string][]graph.NodeID, error) {
	byName := make(map[string][]graph.NodeID)
	err := store.ForEachNode(func(n graph.Node) bool {
		switch n.Kind {
		case parse.KindSymbol, parse.KindImport, parse.KindDoc, parse.KindHeading:
			return true
		}
		if n.Name != "" && len(byName[n.Name]) < 2 {
			byName[n.Name] = append(byName[n.Name], n.ID)
		}
		return true
	})
	return byName, err
}

func resolveMention(store graph.Store, byName map[string][]graph.NodeID, mention string) (graph.NodeID, bool) {
	mention = strings.TrimSpace(mention)
	if mention == "" {
		return "", false
	}
	if n, err := store.GetNode(graph.NodeID(mention)); err == nil {
		return n.ID, true
	}
	if strings.Contains(mention, "/") {
		fid := graph.NodeID("file:" + filepath.ToSlash(mention))
		if _, err := store.GetNode(fid); err == nil {
			return fid, true
		}
	}
	if matches := byName[mention]; len(matches) == 1 {
		return matches[0], true
	}
	return "", false
}

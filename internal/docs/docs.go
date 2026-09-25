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
	headingRe = regexp.MustCompile(`^(#{1,6})\s+(.+)$`)
	backtickRe = regexp.MustCompile("`([^`]+)`")
	pathRe    = regexp.MustCompile(`(?:^|[\s(])([A-Za-z0-9_./-]+\.(?:go|ts|tsx|js|jsx|py|swift|md))`)
	adrIDRe   = regexp.MustCompile(`(?i)\bADR[- ]?(\d+)\b`)
)

// ListDocFiles returns absolute paths of Markdown docs under root.
func ListDocFiles(root string, ignoreDirNames []string) ([]string, error) {
	if ignoreDirNames == nil {
		ignoreDirNames = []string{"vendor", "node_modules", ".git", ".synapse", ".synapse-out"}
	}
	ignoreSet := map[string]struct{}{}
	for _, d := range ignoreDirNames {
		ignoreSet[d] = struct{}{}
	}
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if _, skip := ignoreSet[d.Name()]; skip && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
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
	for sc.Scan() {
		line++
		m := headingRe.FindStringSubmatch(sc.Text())
		if m == nil {
			continue
		}
		title := strings.TrimSpace(m[2])
		hid := graph.NodeID(fmt.Sprintf("heading:%s#%s", relPath, title))
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
	out.Normalize()
	return out, sc.Err()
}

// LinkOptions configure the post-index documentation linker.
type LinkOptions struct {
	Root  string
	Store graph.Store
	Repo  string
}

// Link scans indexed markdown file contents and creates documents edges to code.
func Link(opts LinkOptions) (int, error) {
	if opts.Store == nil || opts.Root == "" {
		return 0, fmt.Errorf("docs link: store and root required")
	}
	files, err := ListDocFiles(opts.Root, nil)
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
		text := string(data)
		targets := map[graph.NodeID]struct{}{}
		for _, m := range backtickRe.FindAllStringSubmatch(text, -1) {
			if id, ok := resolveMention(opts.Store, m[1]); ok {
				targets[id] = struct{}{}
			}
		}
		for _, m := range pathRe.FindAllStringSubmatch(text, -1) {
			p := filepath.ToSlash(m[1])
			fid := graph.NodeID("file:" + p)
			if _, err := opts.Store.GetNode(fid); err == nil {
				targets[fid] = struct{}{}
			}
		}
		for _, m := range adrIDRe.FindAllStringSubmatch(text, -1) {
			_ = m // ADR cross-refs within docs; link same-repo ADR files if present
			cand := filepath.ToSlash(filepath.Join("docs", "adr", fmt.Sprintf("%s.md", m[1])))
			fid := graph.NodeID("file:" + cand)
			if _, err := opts.Store.GetNode(fid); err == nil {
				targets[fid] = struct{}{}
			}
		}
		for to := range targets {
			if to == docID {
				continue
			}
			edge := graph.Edge{
				From: docID,
				To:   to,
				Type: parse.EdgeDocuments,
				Props: map[string]string{
					parse.PropProvenance: parse.ProvenanceInferred,
				},
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

func resolveMention(store graph.Store, mention string) (graph.NodeID, bool) {
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
	var matches []graph.NodeID
	_ = store.ForEachNode(func(n graph.Node) bool {
		if n.Name == mention {
			matches = append(matches, n.ID)
			return len(matches) < 2
		}
		return true
	})
	if len(matches) == 1 {
		return matches[0], true
	}
	return "", false
}

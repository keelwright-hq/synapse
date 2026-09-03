package index

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/taricsa/synapse/internal/graph"
	"github.com/taricsa/synapse/internal/parse"
	"github.com/taricsa/synapse/internal/uri"
)

// Store combines graph persistence with fingerprint/ownership metadata.
type Store interface {
	graph.Store
	MetaStore
}

// Stats summarizes an index run.
type Stats struct {
	Processed int `json:"processed"`
	Skipped   int `json:"skipped"`
	Deleted   int `json:"deleted"`
	Errors    int `json:"errors"`
}

// Options configure Indexer.Run.
type Options struct {
	Workers        int
	IgnoreDirNames []string
	Registry       *parse.Registry
	Logger         *slog.Logger
	Repo           string // repo:// name; required for URI assignment (caller supplies default)
}

// Indexer incrementally parses a repo and upserts into Store.
type Indexer struct {
	store Store
	mu    sync.Mutex // serializes multi-step graph replaces
}

// New returns an Indexer writing to store.
func New(store Store) *Indexer {
	return &Indexer{store: store}
}

// Run walks root, skips unchanged files by content hash, replaces changed file
// subgraphs, and removes orphans for deleted files.
func (idx *Indexer) Run(root string, opts Options) (Stats, error) {
	if err := parse.EnsureRootExists(root); err != nil {
		return Stats{}, err
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return Stats{}, err
	}
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	reg := opts.Registry
	if reg == nil {
		reg = parse.NewRegistry()
	}
	workers := opts.Workers
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}
	if workers < 1 {
		workers = 1
	}
	repo := opts.Repo
	if repo == "" {
		repo = filepath.Base(root)
	}
	if _, err := uri.NormalizeRepo(repo); err != nil {
		return Stats{}, fmt.Errorf("index: repo name: %w", err)
	}

	files, err := parse.ListSourceFiles(root, reg, opts.IgnoreDirNames)
	if err != nil {
		return Stats{}, err
	}

	existing, err := idx.store.ListFingerprints()
	if err != nil {
		return Stats{}, err
	}

	seen := make(map[string]struct{}, len(files))
	type job struct {
		absPath string
		relPath string
	}
	jobs := make(chan job, workers*2)
	var (
		stats  Stats
		wg     sync.WaitGroup
		statMu sync.Mutex
		errMu  sync.Mutex
		errs   []error
	)

	inc := func(field *int) {
		statMu.Lock()
		*field++
		statMu.Unlock()
	}

	worker := func() {
		defer wg.Done()
		for j := range jobs {
			hash, err := hashFile(j.absPath)
			if err != nil {
				errMu.Lock()
				errs = append(errs, fmt.Errorf("%s: hash: %w", j.relPath, err))
				errMu.Unlock()
				inc(&stats.Errors)
				log.Error("index hash failed", "path", j.relPath, "err", err)
				continue
			}
			prev, ok, err := idx.store.GetFingerprint(j.relPath)
			if err != nil {
				errMu.Lock()
				errs = append(errs, err)
				errMu.Unlock()
				inc(&stats.Errors)
				log.Error("index fingerprint lookup failed", "path", j.relPath, "err", err)
				continue
			}
			if ok && prev == hash {
				inc(&stats.Skipped)
				log.Debug("index skip unchanged", "path", j.relPath)
				continue
			}
			res, err := parse.ParseFile(reg, j.absPath)
			if err != nil {
				errMu.Lock()
				errs = append(errs, fmt.Errorf("%s: parse: %w", j.relPath, err))
				errMu.Unlock()
				inc(&stats.Errors)
				log.Error("index parse failed", "path", j.relPath, "err", err)
				continue
			}
			if res.Skipped {
				inc(&stats.Skipped)
				continue
			}
			res = rewritePaths(res, j.absPath, j.relPath)
			res = assignRepoURIs(res, repo)
			if err := idx.replaceFile(j.relPath, hash, res); err != nil {
				errMu.Lock()
				errs = append(errs, fmt.Errorf("%s: replace: %w", j.relPath, err))
				errMu.Unlock()
				inc(&stats.Errors)
				log.Error("index replace failed", "path", j.relPath, "err", err)
				continue
			}
			inc(&stats.Processed)
			log.Info("index processed", "path", j.relPath)
		}
	}

	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go worker()
	}
	for _, abs := range files {
		rel, err := filepath.Rel(root, abs)
		if err != nil {
			close(jobs)
			wg.Wait()
			return stats, err
		}
		rel = filepath.ToSlash(rel)
		seen[rel] = struct{}{}
		jobs <- job{absPath: abs, relPath: rel}
	}
	close(jobs)
	wg.Wait()

	for path := range existing {
		if _, ok := seen[path]; ok {
			continue
		}
		if err := idx.deleteFile(path); err != nil {
			errs = append(errs, fmt.Errorf("%s: delete: %w", path, err))
			stats.Errors++
			log.Error("index delete failed", "path", path, "err", err)
			continue
		}
		stats.Deleted++
		log.Info("index deleted", "path", path)
	}

	log.Info("index complete",
		"processed", stats.Processed,
		"skipped", stats.Skipped,
		"deleted", stats.Deleted,
		"errors", stats.Errors,
	)
	if len(errs) > 0 && stats.Processed == 0 && stats.Skipped == 0 && stats.Deleted == 0 {
		return stats, errs[0]
	}
	return stats, nil
}

func (idx *Indexer) replaceFile(relPath, hash string, res parse.Result) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	owned := ownedNodeIDs(res)
	old, err := idx.store.GetOwnedNodes(relPath)
	if err != nil {
		return err
	}
	for _, id := range old {
		if err := idx.store.DeleteNode(id); err != nil && err != graph.ErrNotFound {
			return err
		}
	}
	for _, n := range res.Nodes {
		if err := idx.store.PutNode(n); err != nil {
			return err
		}
	}
	for _, e := range res.Edges {
		if err := idx.store.PutEdge(e); err != nil {
			return err
		}
	}
	if err := idx.store.PutOwnedNodes(relPath, owned); err != nil {
		return err
	}
	return idx.store.PutFingerprint(relPath, hash)
}

func (idx *Indexer) deleteFile(relPath string) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	old, err := idx.store.GetOwnedNodes(relPath)
	if err != nil {
		return err
	}
	for _, id := range old {
		if err := idx.store.DeleteNode(id); err != nil && err != graph.ErrNotFound {
			return err
		}
	}
	if err := idx.store.DeleteOwnedNodes(relPath); err != nil {
		return err
	}
	return idx.store.DeleteFingerprint(relPath)
}

func ownedNodeIDs(res parse.Result) []graph.NodeID {
	ids := make([]graph.NodeID, 0, len(res.Nodes))
	for _, n := range res.Nodes {
		// Shared unresolved call targets are not file-owned so other files keep edges.
		if n.Kind == parse.KindSymbol {
			continue
		}
		ids = append(ids, n.ID)
	}
	return ids
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// rewritePaths remaps absolute file paths in IR to repo-relative slash paths.
func rewritePaths(res parse.Result, absPath, relPath string) parse.Result {
	relPath = filepath.ToSlash(relPath)
	absSlash := filepath.ToSlash(absPath)
	absNative := absPath

	rewriteID := func(id graph.NodeID) graph.NodeID {
		s := string(id)
		s = strings.ReplaceAll(s, absSlash, relPath)
		if absNative != absSlash {
			s = strings.ReplaceAll(s, absNative, relPath)
		}
		return graph.NodeID(s)
	}

	out := parse.Result{Path: relPath, Lang: res.Lang}
	for _, n := range res.Nodes {
		n.ID = rewriteID(n.ID)
		switch n.Path {
		case absSlash, absNative, res.Path:
			n.Path = relPath
		}
		out.Nodes = append(out.Nodes, n)
	}
	for _, e := range res.Edges {
		e.From = rewriteID(e.From)
		e.To = rewriteID(e.To)
		out.Edges = append(out.Edges, e)
	}
	out.Normalize()
	return out
}

// assignRepoURIs sets props.repo_uri on nodes that can receive a canonical URI.
func assignRepoURIs(res parse.Result, repo string) parse.Result {
	for i := range res.Nodes {
		n := &res.Nodes[i]
		canonical, ok, err := uri.Assign(repo, n.Path, n.Kind, n.Name, string(n.ID))
		if err != nil || !ok {
			continue
		}
		if n.Props == nil {
			n.Props = map[string]string{}
		}
		n.Props[uri.PropKey] = canonical
	}
	return res
}

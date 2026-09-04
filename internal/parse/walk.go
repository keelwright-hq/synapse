package parse

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

// WalkOptions configures repository walking.
type WalkOptions struct {
	// IgnoreDirNames are directory basenames to skip (default: vendor, node_modules, .git, .synapse).
	IgnoreDirNames []string
	// Workers is the concurrent parse pool size (default: GOMAXPROCS).
	Workers int
	// Registry selects parsers; nil uses NewRegistry().
	Registry *Registry
}

// DefaultIgnoreDirNames are skipped unless overridden.
var DefaultIgnoreDirNames = []string{"vendor", "node_modules", ".git", ".synapse", ".synapse-out"}

// WalkResult aggregates per-file parse results and errors.
type WalkResult struct {
	Results []Result
	Errors  []error
}

// WalkTree walks root, skipping ignored dirs, and parses registered source files concurrently.
func WalkTree(root string, opts WalkOptions) (WalkResult, error) {
	if err := EnsureRootExists(root); err != nil {
		return WalkResult{}, err
	}
	if opts.Registry == nil {
		opts.Registry = NewRegistry()
	}
	ignores := opts.IgnoreDirNames
	if ignores == nil {
		ignores = DefaultIgnoreDirNames
	}
	ignoreSet := make(map[string]struct{}, len(ignores))
	for _, d := range ignores {
		ignoreSet[d] = struct{}{}
	}
	workers := opts.Workers
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}
	if workers < 1 {
		workers = 1
	}

	type job struct {
		path string
	}
	jobs := make(chan job, workers*2)
	var (
		mu  sync.Mutex
		out WalkResult
		wg  sync.WaitGroup
	)

	worker := func() {
		defer wg.Done()
		for j := range jobs {
			res, err := ParseFile(opts.Registry, j.path)
			mu.Lock()
			if err != nil {
				out.Errors = append(out.Errors, fmt.Errorf("%s: %w", j.path, err))
			} else if !res.Skipped {
				out.Results = append(out.Results, res)
			}
			mu.Unlock()
		}
	}

	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go worker()
	}

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if _, skip := ignoreSet[name]; skip && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if opts.Registry.Lookup(path) == nil {
			return nil
		}
		jobs <- job{path: path}
		return nil
	})
	close(jobs)
	wg.Wait()
	if err != nil {
		return out, err
	}
	return out, nil
}

// ListSourceFiles returns registered source paths under root (sync, for tests).
func ListSourceFiles(root string, reg *Registry, ignoreDirNames []string) ([]string, error) {
	if reg == nil {
		reg = NewRegistry()
	}
	if ignoreDirNames == nil {
		ignoreDirNames = DefaultIgnoreDirNames
	}
	ignoreSet := make(map[string]struct{}, len(ignoreDirNames))
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
		if reg.Lookup(path) == nil {
			return nil
		}
		files = append(files, path)
		return nil
	})
	return files, err
}

// EnsureRootExists is a tiny helper for clearer Walk errors.
func EnsureRootExists(root string) error {
	st, err := os.Stat(root)
	if err != nil {
		return err
	}
	if !st.IsDir() {
		return fmt.Errorf("parse: not a directory: %s", root)
	}
	return nil
}

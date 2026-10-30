package parse

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/go-git/go-git/v5/plumbing/format/gitignore"
)

// WalkOptions configures repository walking.
type WalkOptions struct {
	// IgnoreDirNames are directory basenames to skip (default: DefaultIgnoreDirNames).
	IgnoreDirNames []string
	// Workers is the concurrent parse pool size (default: GOMAXPROCS).
	Workers int
	// Registry selects parsers; nil uses NewRegistry().
	Registry *Registry
}

// DefaultIgnoreDirNames are skipped unless overridden.
var DefaultIgnoreDirNames = []string{
	"vendor",
	"node_modules",
	"third_party",
	".git",
	".synapse",
	".synapse-out",
}

// WalkResult aggregates per-file parse results and errors.
type WalkResult struct {
	Results []Result
	Errors  []error
}

type gitignoreRule struct {
	pattern  gitignore.Pattern
	negation bool
	domain   []string
	parts    []string
}

// WalkFiles walks root, skipping directories in ignoreDirNames (defaulting to
// DefaultIgnoreDirNames when nil) and paths matched by .gitignore files under
// root, and invokes visit for each regular file.
func WalkFiles(root string, ignoreDirNames []string, visit func(path string, d fs.DirEntry) error) error {
	if ignoreDirNames == nil {
		ignoreDirNames = DefaultIgnoreDirNames
	}
	ignoreSet := make(map[string]struct{}, len(ignoreDirNames))
	for _, d := range ignoreDirNames {
		ignoreSet[d] = struct{}{}
	}
	var (
		rules    []gitignoreRule
		patterns []gitignore.Pattern
		matcher  gitignore.Matcher
	)
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relParts := splitRelPath(root, path)
		if d.IsDir() {
			if path != root {
				if _, skip := ignoreSet[d.Name()]; skip {
					return filepath.SkipDir
				}
				if matcher != nil && len(relParts) > 0 && matcher.Match(relParts, true) {
					if !hasLaterNegationForDir(rules, relParts) {
						return filepath.SkipDir
					}
				}
			}
			if dirRules := readGitignoreRules(path, relParts); len(dirRules) > 0 {
				for _, r := range dirRules {
					rules = append(rules, r)
					patterns = append(patterns, r.pattern)
				}
				matcher = gitignore.NewMatcher(patterns)
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if path != root && matcher != nil && len(relParts) > 0 && matcher.Match(relParts, false) {
			return nil
		}
		return visit(path, d)
	})
}

func splitRelPath(root, path string) []string {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || rel == "" {
		return nil
	}
	return strings.Split(filepath.ToSlash(rel), "/")
}

func readGitignoreRules(dir string, domain []string) []gitignoreRule {
	f, err := os.Open(filepath.Join(dir, ".gitignore"))
	if err != nil {
		return nil
	}
	defer f.Close()

	var out []gitignoreRule
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		neg := strings.HasPrefix(line, "!")
		raw := strings.TrimPrefix(line, "!")
		raw = strings.Trim(raw, "/")
		var parts []string
		if raw != "" {
			parts = strings.Split(raw, "/")
		}
		var domCopy []string
		if len(domain) > 0 {
			domCopy = append([]string(nil), domain...)
		}
		out = append(out, gitignoreRule{
			pattern:  gitignore.ParsePattern(line, domCopy),
			negation: neg,
			domain:   domCopy,
			parts:    parts,
		})
	}
	return out
}

// hasLaterNegationForDir reports whether any '!' rule after the last rule
// excluding dirParts can match a path inside dirParts.
func hasLaterNegationForDir(rules []gitignoreRule, dirParts []string) bool {
	lastExclude := -1
	for i, r := range rules {
		switch r.pattern.Match(dirParts, true) {
		case gitignore.Exclude:
			lastExclude = i
		case gitignore.Include:
			lastExclude = -1
		}
	}
	if lastExclude < 0 {
		return false
	}
	for i := lastExclude + 1; i < len(rules); i++ {
		if rules[i].negation && negationCanMatchUnderDir(rules[i], dirParts) {
			return true
		}
	}
	return false
}

func negationCanMatchUnderDir(r gitignoreRule, dirParts []string) bool {
	if len(dirParts) < len(r.domain) {
		return false
	}
	for i, d := range r.domain {
		if dirParts[i] != d {
			return false
		}
	}
	subDir := dirParts[len(r.domain):]
	if len(r.parts) <= 1 {
		// Unanchored basename pattern (e.g. "!keep.go" or "!*.gen.go") can match at any depth.
		return len(r.parts) == 1
	}
	for i := 0; i < len(subDir); i++ {
		if i >= len(r.parts) {
			return false
		}
		if r.parts[i] == "**" {
			return true
		}
		if matched, err := filepath.Match(r.parts[i], subDir[i]); err != nil || !matched {
			return false
		}
	}
	return len(r.parts) > len(subDir)
}

// WalkTree walks root, skipping ignored dirs, and parses registered source files concurrently.
func WalkTree(root string, opts WalkOptions) (WalkResult, error) {
	if err := EnsureRootExists(root); err != nil {
		return WalkResult{}, err
	}
	if opts.Registry == nil {
		opts.Registry = NewRegistry()
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

	err := WalkFiles(root, opts.IgnoreDirNames, func(path string, d fs.DirEntry) error {
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
	var files []string
	err := WalkFiles(root, ignoreDirNames, func(path string, d fs.DirEntry) error {
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

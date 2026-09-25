// Package git correlates commit history into weighted co_committed graph edges (SYN-14).
package git

import (
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/keelwright-hq/synapse/internal/graph"
	"github.com/keelwright-hq/synapse/internal/parse"
)

// Options configure history walking and co-change aggregation.
type Options struct {
	MaxCommits int
	Since      time.Time
	MinSupport int
	// HalfLifeDays controls recency decay (weight *= 0.5^(ageDays/HalfLifeDays)).
	HalfLifeDays float64
}

// CommitFiles is one commit's touched paths (repo-relative slash paths).
type CommitFiles struct {
	Hash  string
	When  time.Time
	Paths []string
}

// Pair is an aggregated co-change between two file paths.
type Pair struct {
	A, B     string
	Support  int
	Weight   float64
	LastHash string
}

// Walk returns commit file sets via go-git (no git CLI).
func Walk(repoRoot string, opts Options) ([]CommitFiles, error) {
	if opts.MaxCommits <= 0 {
		opts.MaxCommits = 500
	}
	r, err := git.PlainOpen(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("git walk: %w", err)
	}
	iter, err := r.Log(&git.LogOptions{Order: git.LogOrderCommitterTime})
	if err != nil {
		return nil, fmt.Errorf("git log: %w", err)
	}
	defer iter.Close()

	var out []CommitFiles
	err = iter.ForEach(func(c *object.Commit) error {
		if !opts.Since.IsZero() && c.Committer.When.Before(opts.Since) {
			return nil
		}
		stats, err := c.Stats()
		if err != nil {
			// Merge commits / empty trees — skip quietly.
			return nil
		}
		paths := make([]string, 0, len(stats))
		seen := map[string]struct{}{}
		for _, s := range stats {
			p := filepath.ToSlash(s.Name)
			if p == "" {
				continue
			}
			if _, ok := seen[p]; ok {
				continue
			}
			seen[p] = struct{}{}
			paths = append(paths, p)
		}
		if len(paths) < 2 {
			return nil
		}
		sort.Strings(paths)
		out = append(out, CommitFiles{
			Hash:  c.Hash.String(),
			When:  c.Committer.When,
			Paths: paths,
		})
		if len(out) >= opts.MaxCommits {
			return fmt.Errorf("git: stop")
		}
		return nil
	})
	if err != nil && err.Error() != "git: stop" {
		return nil, err
	}
	return out, nil
}

// Aggregate builds pairwise co-change weights with recency decay and min-support.
func Aggregate(commits []CommitFiles, opts Options) []Pair {
	if opts.MinSupport <= 0 {
		opts.MinSupport = 2
	}
	if opts.HalfLifeDays <= 0 {
		opts.HalfLifeDays = 90
	}
	now := time.Now()
	type acc struct {
		support  int
		weight   float64
		lastHash string
		lastWhen time.Time
	}
	pairs := map[string]*acc{}
	keyOf := func(a, b string) string {
		if a > b {
			a, b = b, a
		}
		return a + "\x00" + b
	}
	for _, c := range commits {
		ageDays := now.Sub(c.When).Hours() / 24
		if ageDays < 0 {
			ageDays = 0
		}
		decay := math.Pow(0.5, ageDays/opts.HalfLifeDays)
		for i := 0; i < len(c.Paths); i++ {
			for j := i + 1; j < len(c.Paths); j++ {
				k := keyOf(c.Paths[i], c.Paths[j])
				a := pairs[k]
				if a == nil {
					a = &acc{}
					pairs[k] = a
				}
				a.support++
				a.weight += decay
				if c.When.After(a.lastWhen) {
					a.lastWhen = c.When
					a.lastHash = c.Hash
				}
			}
		}
	}
	out := make([]Pair, 0, len(pairs))
	for k, a := range pairs {
		if a.support < opts.MinSupport {
			continue
		}
		parts := strings.SplitN(k, "\x00", 2)
		out = append(out, Pair{
			A: parts[0], B: parts[1],
			Support: a.support, Weight: a.weight, LastHash: a.lastHash,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Weight != out[j].Weight {
			return out[i].Weight > out[j].Weight
		}
		return out[i].A+out[i].B < out[j].A+out[j].B
	})
	return out
}

// Analyze walks history and upserts co_committed edges between existing file nodes.
// Soft-skips when repoRoot is not a git repository. Writes to the member store only.
func Analyze(store graph.Store, repoRoot string, opts Options) (int, error) {
	if _, err := git.PlainOpen(repoRoot); err != nil {
		return 0, nil
	}
	commits, err := Walk(repoRoot, opts)
	if err != nil {
		return 0, err
	}
	pairs := Aggregate(commits, opts)
	n := 0
	for _, p := range pairs {
		fromID := graph.NodeID("file:" + p.A)
		toID := graph.NodeID("file:" + p.B)
		if _, err := store.GetNode(fromID); err != nil {
			continue
		}
		if _, err := store.GetNode(toID); err != nil {
			continue
		}
		edge := graph.Edge{
			From: fromID,
			To:   toID,
			Type: parse.EdgeCoCommitted,
			Props: map[string]string{
				parse.PropProvenance: parse.ProvenanceHistorical,
				parse.PropWeight:     strconv.FormatFloat(p.Weight, 'f', 4, 64),
				parse.PropSupport:    strconv.Itoa(p.Support),
				"last_commit":        p.LastHash,
			},
		}
		if err := store.PutEdge(edge); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// TopCoChanges returns co_committed neighbors for a file path or file node id, sorted by weight.
func TopCoChanges(store graph.Store, pathOrID string, limit int) ([]graph.Edge, error) {
	if limit <= 0 {
		limit = 20
	}
	id := graph.NodeID(pathOrID)
	if !strings.HasPrefix(pathOrID, "file:") {
		id = graph.NodeID("file:" + filepath.ToSlash(pathOrID))
	}
	out, err := store.OutEdges(id, parse.EdgeCoCommitted)
	if err != nil {
		return nil, err
	}
	in, err := store.InEdges(id, parse.EdgeCoCommitted)
	if err != nil {
		return nil, err
	}
	all := append(out, in...)
	sort.Slice(all, func(i, j int) bool {
		wi, _ := strconv.ParseFloat(all[i].Props[parse.PropWeight], 64)
		wj, _ := strconv.ParseFloat(all[j].Props[parse.PropWeight], 64)
		return wi > wj
	})
	if len(all) > limit {
		all = all[:limit]
	}
	return all, nil
}

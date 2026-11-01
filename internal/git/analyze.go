// Package git correlates commit history into weighted co_committed graph edges (SYN-14).
package git

import (
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/keelwright-hq/synapse/internal/graph"
	"github.com/keelwright-hq/synapse/internal/parse"
)

// DefaultMaxFilesPerCommit caps the number of files in a single commit that
// contribute to pairwise co-change edges, preventing O(k^2) explosions on
// bulk formatting or initial-import commits.
const DefaultMaxFilesPerCommit = 100

// DefaultMaxScanCommits caps the total number of commits inspected (including
// single-file and bulk commits) so Walk remains bounded when recent history
// contains few multi-file commits.
const DefaultMaxScanCommits = 2000

var errStopWalk = errors.New("git: stop")

// ErrNoCommits is returned by Analyze when the repository has no commits yet
// (empty or unborn HEAD), so callers can warn instead of silently skipping.
var ErrNoCommits = errors.New("git: repository has no commits")

// openRepo opens repoRoot, resolving linked worktrees (whose refs live in the
// main repository's common .git directory).
func openRepo(repoRoot string) (*git.Repository, error) {
	return git.PlainOpenWithOptions(repoRoot, &git.PlainOpenOptions{EnableDotGitCommonDir: true})
}

// Options configure history walking and co-change aggregation.
type Options struct {
	// MaxCommits caps the number of qualifying multi-file commits returned (default: 500).
	MaxCommits int
	// MaxScanCommits caps total commits inspected before stopping (default: max(4*MaxCommits, 2000)).
	MaxScanCommits int
	// MaxFilesPerCommit skips commits touching more than this many files (default: 100).
	MaxFilesPerCommit int
	Since             time.Time
	MinSupport        int
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
	maxScan := opts.MaxScanCommits
	if maxScan <= 0 {
		maxScan = opts.MaxCommits * 4
		if maxScan < DefaultMaxScanCommits {
			maxScan = DefaultMaxScanCommits
		}
	}
	maxFiles := opts.MaxFilesPerCommit
	if maxFiles <= 0 {
		maxFiles = DefaultMaxFilesPerCommit
	}
	r, err := openRepo(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("git walk: %w", err)
	}
	iter, err := r.Log(&git.LogOptions{Order: git.LogOrderCommitterTime})
	if err != nil {
		if errors.Is(err, plumbing.ErrReferenceNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("git log: %w", err)
	}
	defer iter.Close()

	var (
		out     []CommitFiles
		scanned int
	)
	err = iter.ForEach(func(c *object.Commit) error {
		if !opts.Since.IsZero() && c.Committer.When.Before(opts.Since) {
			return errStopWalk
		}
		if scanned >= maxScan {
			return errStopWalk
		}
		scanned++
		// Merge commits replay changes already counted on the merged branch's
		// own commits; counting them too would double co-change support.
		if c.NumParents() > 1 {
			return nil
		}
		stats, err := c.Stats()
		if err != nil {
			// Empty trees / unreadable diffs — skip quietly.
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
		if len(paths) < 2 || len(paths) > maxFiles {
			return nil
		}
		sort.Strings(paths)
		out = append(out, CommitFiles{
			Hash:  c.Hash.String(),
			When:  c.Committer.When,
			Paths: paths,
		})
		if len(out) >= opts.MaxCommits {
			return errStopWalk
		}
		return nil
	})
	if err != nil && !errors.Is(err, errStopWalk) {
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
	maxFiles := opts.MaxFilesPerCommit
	if maxFiles <= 0 {
		maxFiles = DefaultMaxFilesPerCommit
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
		if len(c.Paths) < 2 || len(c.Paths) > maxFiles {
			continue
		}
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
	r, err := openRepo(repoRoot)
	if err != nil {
		return 0, nil
	}
	if _, err := r.Head(); errors.Is(err, plumbing.ErrReferenceNotFound) {
		return 0, ErrNoCommits
	}
	commits, err := Walk(repoRoot, opts)
	if err != nil {
		return 0, err
	}
	if err := clearCoCommittedEdges(store); err != nil {
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

func clearCoCommittedEdges(s graph.Store) error {
	var ids []graph.NodeID
	err := s.ForEachNode(func(n graph.Node) bool {
		if n.Kind == parse.KindFile {
			ids = append(ids, n.ID)
		}
		return true
	})
	if err != nil {
		return err
	}
	for _, id := range ids {
		edges, err := s.OutEdges(id, parse.EdgeCoCommitted)
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

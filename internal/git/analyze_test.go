package git_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	synapsegit "github.com/keelwright-hq/synapse/internal/git"
	"github.com/keelwright-hq/synapse/internal/graph"
	"github.com/keelwright-hq/synapse/internal/parse"
	"github.com/keelwright-hq/synapse/internal/store/memory"
)

func TestWalkAggregateAnalyze(t *testing.T) {
	root := t.TempDir()
	mustWrite := func(rel, body string) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("a.go", "package a\n")
	mustWrite("b.go", "package b\n")
	mustWrite("c.go", "package c\n")

	r, err := git.PlainInit(root, false)
	if err != nil {
		t.Fatal(err)
	}
	w, err := r.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	commitPair := func(msg string, files ...string) {
		t.Helper()
		for _, f := range files {
			// Touch so the commit is not empty on repeat.
			p := filepath.Join(root, f)
			b, _ := os.ReadFile(p)
			if err := os.WriteFile(p, append(b, '\n'), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := w.Add(f); err != nil {
				t.Fatal(err)
			}
		}
		_, err := w.Commit(msg, &git.CommitOptions{
			Author: &object.Signature{Name: "t", Email: "t@t", When: time.Now()},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	commitPair("ab1", "a.go", "b.go")
	commitPair("ab2", "a.go", "b.go")
	commitPair("ac1", "a.go", "c.go")
	commitPair("ac2", "a.go", "c.go")

	commits, err := synapsegit.Walk(root, synapsegit.Options{MaxCommits: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) < 2 {
		t.Fatalf("want >=2 commits with multi-file sets, got %d", len(commits))
	}
	pairs := synapsegit.Aggregate(commits, synapsegit.Options{MinSupport: 2})
	if len(pairs) == 0 {
		t.Fatal("expected aggregated pairs")
	}

	store := memory.New()
	for _, p := range []string{"a.go", "b.go", "c.go"} {
		id := graph.NodeID("file:" + p)
		if err := store.PutNode(graph.Node{ID: id, Kind: parse.KindFile, Name: p, Path: p}); err != nil {
			t.Fatal(err)
		}
	}
	n, err := synapsegit.Analyze(store, root, synapsegit.Options{MaxCommits: 50, MinSupport: 2})
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("expected co_committed edges")
	}
	edges, err := synapsegit.TopCoChanges(store, "a.go", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(edges) == 0 {
		t.Fatal("TopCoChanges empty")
	}
	if edges[0].Type != parse.EdgeCoCommitted {
		t.Fatalf("type=%s", edges[0].Type)
	}
	if edges[0].Props[parse.PropProvenance] != parse.ProvenanceHistorical {
		t.Fatalf("provenance=%v", edges[0].Props)
	}

	// Re-running Analyze with higher MinSupport prunes previously written co_committed edges.
	n2, err := synapsegit.Analyze(store, root, synapsegit.Options{MaxCommits: 50, MinSupport: 10})
	if err != nil {
		t.Fatal(err)
	}
	if n2 != 0 {
		t.Fatalf("expected 0 edges with MinSupport=10, got %d", n2)
	}
	edgesAfter, err := synapsegit.TopCoChanges(store, "a.go", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(edgesAfter) != 0 {
		t.Fatalf("expected stale co_committed edges to be pruned, got %+v", edgesAfter)
	}
}

func TestWalkEmptyRepoAndCommitBounds(t *testing.T) {
	emptyRoot := t.TempDir()
	if _, err := git.PlainInit(emptyRoot, false); err != nil {
		t.Fatal(err)
	}
	commits, err := synapsegit.Walk(emptyRoot, synapsegit.Options{})
	if err != nil {
		t.Fatalf("expected nil error on empty git init repo, got %v", err)
	}
	if len(commits) != 0 {
		t.Fatalf("expected 0 commits on empty repo, got %d", len(commits))
	}

	root := t.TempDir()
	r, err := git.PlainInit(root, false)
	if err != nil {
		t.Fatal(err)
	}
	w, err := r.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	commitFiles := func(msg string, files ...string) {
		t.Helper()
		for _, f := range files {
			p := filepath.Join(root, f)
			b, _ := os.ReadFile(p)
			if err := os.WriteFile(p, append(b, 'x'), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := w.Add(f); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := w.Commit(msg, &git.CommitOptions{
			Author: &object.Signature{Name: "t", Email: "t@t", When: time.Now()},
		}); err != nil {
			t.Fatal(err)
		}
	}

	// Older multi-file commit, then a large commit touching 3 files, then two single-file commits.
	commitFiles("pair", "a.go", "b.go")
	commitFiles("bulk", "a.go", "b.go", "c.go")
	commitFiles("single1", "a.go")
	commitFiles("single2", "a.go")

	// MaxCommits counts pair-producing commits, so the 2 recent single-file commits do not
	// exhaust MaxCommits=1; it returns the newest multi-file commit ("bulk", 3 files).
	bounded, err := synapsegit.Walk(root, synapsegit.Options{MaxCommits: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(bounded) != 1 || len(bounded[0].Paths) != 3 {
		t.Fatalf("expected MaxCommits=1 to skip single-file commits and return 1 multi-file commit, got %+v", bounded)
	}

	// MaxFilesPerCommit=2 should skip the 3-file "bulk" commit without burning MaxCommits=1,
	// keeping the older 2-file "pair" commit.
	filtered, err := synapsegit.Walk(root, synapsegit.Options{MaxCommits: 1, MaxFilesPerCommit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 1 || len(filtered[0].Paths) != 2 {
		t.Fatalf("expected 1 2-file commit with MaxCommits=1 MaxFilesPerCommit=2, got %+v", filtered)
	}

	// MaxScanCommits=2 stops after inspecting the two newest single-file commits,
	// before reaching older multi-file commits.
	scanBounded, err := synapsegit.Walk(root, synapsegit.Options{MaxCommits: 10, MaxScanCommits: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(scanBounded) != 0 {
		t.Fatalf("expected 0 qualifying commits when MaxScanCommits=2 stops on single-file commits, got %+v", scanBounded)
	}
}

func TestAnalyzeLinkedWorktreeAndEmptyRepo(t *testing.T) {
	emptyRoot := t.TempDir()
	if _, err := git.PlainInit(emptyRoot, false); err != nil {
		t.Fatal(err)
	}
	if _, err := synapsegit.Analyze(memory.New(), emptyRoot, synapsegit.Options{}); !errors.Is(err, synapsegit.ErrNoCommits) {
		t.Fatalf("Analyze on empty repo: err=%v, want ErrNoCommits", err)
	}

	gitBin, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git CLI not available to create a linked worktree")
	}
	root := t.TempDir()
	runGit := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command(gitBin, args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	runGit(root, "init", "-q")
	for i := 0; i < 2; i++ {
		for _, f := range []string{"a.go", "b.go"} {
			p := filepath.Join(root, f)
			b, _ := os.ReadFile(p)
			if err := os.WriteFile(p, append(b, 'x'), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		runGit(root, "add", "a.go", "b.go")
		runGit(root, "commit", "-q", "-m", "pair")
	}
	wt := filepath.Join(t.TempDir(), "wt")
	runGit(root, "worktree", "add", "-q", "--detach", wt)

	store := memory.New()
	for _, p := range []string{"a.go", "b.go"} {
		if err := store.PutNode(graph.Node{ID: graph.NodeID("file:" + p), Kind: parse.KindFile, Name: p, Path: p}); err != nil {
			t.Fatal(err)
		}
	}
	n, err := synapsegit.Analyze(store, wt, synapsegit.Options{MinSupport: 2})
	if err != nil {
		t.Fatalf("Analyze in linked worktree: %v", err)
	}
	if n == 0 {
		t.Fatal("expected co_committed edges from a linked worktree")
	}
}

func TestWalkSkipsMergeCommits(t *testing.T) {
	gitBin, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git CLI not available to create a merge commit")
	}
	root := t.TempDir()
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command(gitBin, args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	touch := func(files ...string) {
		t.Helper()
		for _, f := range files {
			p := filepath.Join(root, f)
			b, _ := os.ReadFile(p)
			if err := os.WriteFile(p, append(b, 'x'), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	runGit("init", "-q", "-b", "main")
	touch("base.go")
	runGit("add", ".")
	runGit("commit", "-q", "-m", "base")
	runGit("checkout", "-q", "-b", "feature")
	touch("a.go", "b.go")
	runGit("add", ".")
	runGit("commit", "-q", "-m", "feature pair")
	runGit("checkout", "-q", "main")
	touch("base.go")
	runGit("commit", "-q", "-am", "diverge")
	runGit("merge", "-q", "--no-ff", "-m", "merge feature", "feature")

	commits, err := synapsegit.Walk(root, synapsegit.Options{MaxCommits: 50})
	if err != nil {
		t.Fatal(err)
	}
	// Only the feature commit pairs a.go with b.go; the merge must not count again.
	if len(commits) != 1 {
		t.Fatalf("want 1 pair-producing commit (merge skipped), got %+v", commits)
	}
}

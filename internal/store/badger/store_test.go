package badger

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	badgerdb "github.com/dgraph-io/badger/v4"
	"github.com/keelwright-hq/synapse/internal/graph"
	"github.com/keelwright-hq/synapse/internal/graph/storetest"
)

func TestStoreConformance(t *testing.T) {
	storetest.RunConformance(t, func() graph.Store {
		dir := t.TempDir()
		s, err := Open(dir)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		return s
	})
}

func TestDurabilityAcrossRestart(t *testing.T) {
	dir := t.TempDir()

	s1, err := Open(dir)
	if err != nil {
		t.Fatalf("Open first: %v", err)
	}

	node := graph.Node{ID: "file:main.go", Kind: "file", Path: "main.go"}
	if err := s1.PutNode(node); err != nil {
		t.Fatalf("PutNode: %v", err)
	}
	if err := s1.PutNode(graph.Node{ID: "sym:main", Kind: "function", Name: "main"}); err != nil {
		t.Fatalf("PutNode sym: %v", err)
	}
	if err := s1.PutEdge(graph.Edge{From: "file:main.go", To: "sym:main", Type: "contains"}); err != nil {
		t.Fatalf("PutEdge: %v", err)
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("Close first: %v", err)
	}

	s2, err := Open(dir)
	if err != nil {
		t.Fatalf("Open second: %v", err)
	}
	defer s2.Close()

	got, err := s2.GetNode(node.ID)
	if err != nil {
		t.Fatalf("GetNode after restart: %v", err)
	}
	if got.ID != node.ID || got.Path != node.Path {
		t.Fatalf("node mismatch after restart: %+v", got)
	}

	out, err := s2.OutEdges("file:main.go", "")
	if err != nil {
		t.Fatalf("OutEdges after restart: %v", err)
	}
	if len(out) != 1 || out[0].To != "sym:main" {
		t.Fatalf("edges after restart: %+v", out)
	}
}

func TestDefaultDataDir(t *testing.T) {
	orig := defaultDataDir
	tmp := t.TempDir()
	defaultDataDir = tmp
	t.Cleanup(func() { defaultDataDir = orig })

	s, err := Open("")
	if err != nil {
		t.Fatalf("Open default: %v", err)
	}
	defer s.Close()

	want := filepath.Join(tmp, "graph")
	got, err := filepath.EvalSymlinks(s.dir)
	if err != nil {
		got = s.dir
	}
	wantEval, err := filepath.EvalSymlinks(want)
	if err != nil {
		wantEval = want
	}
	if got != wantEval {
		t.Fatalf("graph path = %q want %q", got, wantEval)
	}
}

func TestResolveGraphPathDoesNotSuffixMatch(t *testing.T) {
	path, err := resolveGraphPath(filepath.Join(t.TempDir(), "mygraph"))
	if err != nil {
		t.Fatalf("resolveGraphPath: %v", err)
	}
	if filepath.Base(path) != "graph" {
		t.Fatalf("expected .../mygraph/graph, got %q", path)
	}
}

func TestSlashContainingNodeIDsDoNotCollide(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	parent := graph.Node{ID: "cmd/synapse", Kind: "dir"}
	child := graph.Node{ID: "cmd/synapse/main.go", Kind: "file"}
	other := graph.Node{ID: "other", Kind: "file"}
	for _, n := range []graph.Node{parent, child, other} {
		if err := s.PutNode(n); err != nil {
			t.Fatalf("PutNode %s: %v", n.ID, err)
		}
	}

	// Parent has an edge whose type contains '/', which would collide under
	// slash-delimited keys with a scan of child.
	if err := s.PutEdge(graph.Edge{From: parent.ID, To: other.ID, Type: "main.go/foo"}); err != nil {
		t.Fatalf("PutEdge: %v", err)
	}
	if err := s.PutEdge(graph.Edge{From: child.ID, To: other.ID, Type: "imports"}); err != nil {
		t.Fatalf("PutEdge child: %v", err)
	}

	out, err := s.OutEdges(child.ID, "")
	if err != nil {
		t.Fatalf("OutEdges child: %v", err)
	}
	if len(out) != 1 || out[0].Type != "imports" {
		t.Fatalf("child OutEdges leaked parent edges: %+v", out)
	}

	typed, err := s.OutEdges(parent.ID, "main.go/foo")
	if err != nil {
		t.Fatalf("OutEdges typed: %v", err)
	}
	if len(typed) != 1 || typed[0].To != other.ID {
		t.Fatalf("typed OutEdges: %+v", typed)
	}
}

func TestOpenWaitsForTransientLockRelease(t *testing.T) {
	dir := t.TempDir()
	s1, err := Open(dir)
	if err != nil {
		t.Fatalf("Open first: %v", err)
	}
	go func() {
		time.Sleep(80 * time.Millisecond)
		_ = s1.Close()
	}()

	s2, err := Open(dir)
	if err != nil {
		t.Fatalf("Open second while first releasing lock: %v", err)
	}
	defer s2.Close()
}

func TestReplaceAndDeleteFileGraphSplitsOnErrTxnTooBig(t *testing.T) {
	opts := badgerdb.DefaultOptions("").
		WithInMemory(true).
		WithLogger(nil).
		WithMemTableSize(1 << 20).
		WithValueThreshold(32 << 10)
	db, err := badgerdb.Open(opts)
	if err != nil {
		t.Fatalf("badger Open: %v", err)
	}
	s := &Store{db: db}
	defer s.Close()

	const path = "pkg/huge.go"
	pad := strings.Repeat("x", 1024)
	const count = 250
	nodes := make([]graph.Node, 0, count)
	owned := make([]graph.NodeID, 0, count)
	edges := make([]graph.Edge, 0, count)
	for i := 0; i < count; i++ {
		id := graph.NodeID(fmt.Sprintf("func:%s#F%d", path, i))
		owned = append(owned, id)
		nodes = append(nodes, graph.Node{
			ID:    id,
			Kind:  "function",
			Name:  fmt.Sprintf("F%d", i),
			Path:  path,
			Props: map[string]string{"doc": pad},
		})
		if i > 0 {
			edges = append(edges, graph.Edge{
				From: graph.NodeID(fmt.Sprintf("func:%s#F%d", path, i-1)),
				To:   id,
				Type: "calls",
			})
		}
	}

	if err := s.ReplaceFileGraph(path, "fp-1", nodes, edges, owned); err != nil {
		t.Fatalf("ReplaceFileGraph initial: %v", err)
	}
	fp, ok, err := s.GetFingerprint(path)
	if err != nil || !ok || fp != "fp-1" {
		t.Fatalf("GetFingerprint = (%q, %v, %v), want fp-1", fp, ok, err)
	}
	gotOwned, err := s.GetOwnedNodes(path)
	if err != nil || len(gotOwned) != count {
		t.Fatalf("GetOwnedNodes count = %d (err=%v), want %d", len(gotOwned), err, count)
	}
	if _, err := s.GetNode(owned[count-1]); err != nil {
		t.Fatalf("GetNode last owned: %v", err)
	}

	// Replace with a single node to force deletion of the large prior batch across split transactions.
	onlyID := graph.NodeID("func:" + path + "#Only")
	replacement := []graph.Node{{
		ID:   onlyID,
		Kind: "function",
		Name: "Only",
		Path: path,
	}}
	if err := s.ReplaceFileGraph(path, "fp-2", replacement, nil, []graph.NodeID{onlyID}); err != nil {
		t.Fatalf("ReplaceFileGraph replacement: %v", err)
	}
	gotAfterReplace, err := s.GetOwnedNodes(path)
	if err != nil || len(gotAfterReplace) != 1 || gotAfterReplace[0] != onlyID {
		t.Fatalf("GetOwnedNodes after replacement = %+v (err=%v)", gotAfterReplace, err)
	}
	if _, err := s.GetNode(owned[0]); err != graph.ErrNotFound {
		t.Fatalf("expected old node deleted after replacement, got err=%v", err)
	}

	// Re-populate and test DeleteFileGraph across split transactions.
	if err := s.ReplaceFileGraph(path, "fp-3", nodes, edges, owned); err != nil {
		t.Fatalf("ReplaceFileGraph re-populate: %v", err)
	}
	if err := s.DeleteFileGraph(path); err != nil {
		t.Fatalf("DeleteFileGraph: %v", err)
	}
	gotAfterDelete, err := s.GetOwnedNodes(path)
	if err != nil || len(gotAfterDelete) != 0 {
		t.Fatalf("GetOwnedNodes after delete = %d (err=%v), want 0", len(gotAfterDelete), err)
	}
	if _, err := s.GetNode(owned[count-1]); err != graph.ErrNotFound {
		t.Fatalf("expected node deleted after DeleteFileGraph, got err=%v", err)
	}
}

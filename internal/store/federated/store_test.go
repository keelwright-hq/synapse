package federated_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/taricsa/synapse/internal/graph"
	"github.com/taricsa/synapse/internal/index"
	"github.com/taricsa/synapse/internal/rank"
	"github.com/taricsa/synapse/internal/store/federated"
	"github.com/taricsa/synapse/internal/store/memory"
	"github.com/taricsa/synapse/internal/uri"
)

func writeGo(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func indexTwinRepos(t *testing.T) (api, worker *memory.Store, root string) {
	t.Helper()
	root = t.TempDir()
	writeGo(t, filepath.Join(root, "svc", "handler.go"), "package svc\n\nfunc Handle() {}\n")

	api = memory.New()
	worker = memory.New()
	if _, err := index.New(api).Run(root, index.Options{Repo: "api", Workers: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := index.New(worker).Run(root, index.Options{Repo: "worker", Workers: 1}); err != nil {
		t.Fatal(err)
	}
	return api, worker, root
}

func TestFederatedURIScopedVsAll(t *testing.T) {
	api, worker, _ := indexTwinRepos(t)
	fed, err := federated.New([]federated.Member{
		{Name: "api", Store: api},
		{Name: "worker", Store: worker},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer fed.Close()

	uAPI := "repo://api/svc/handler.go#func:Handle"
	uWorker := "repo://worker/svc/handler.go#func:Handle"

	// Ambiguous Phase-1 id across members (before any URI pin).
	id := graph.NodeID("func:svc/handler.go#Handle")
	if _, err := fed.GetNode(id); !errors.Is(err, graph.ErrConflict) {
		t.Fatalf("want conflict for shared Phase-1 id, got %v", err)
	}

	if _, err := fed.GetNodeByURI(uAPI); err != nil {
		t.Fatal(err)
	}
	if _, err := fed.GetNodeByURI(uWorker); err != nil {
		t.Fatal(err)
	}

	seed, err := rank.ResolveSeed(fed, uAPI)
	if err != nil {
		t.Fatal(err)
	}
	res, err := rank.Neighborhood(fed, seed, rank.Options{Depth: 1, MaxNodes: 16})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) == 0 {
		t.Fatal("expected neighborhood hits after URI pin")
	}
}

func TestFederatedAmbiguousName(t *testing.T) {
	api, worker, _ := indexTwinRepos(t)
	fed, err := federated.New([]federated.Member{
		{Name: "api", Store: api},
		{Name: "worker", Store: worker},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer fed.Close()

	_, err = rank.ResolveSeed(fed, "Handle")
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("want ambiguous name error, got %v", err)
	}
}

func TestFederatedForEachMerges(t *testing.T) {
	api, worker, _ := indexTwinRepos(t)
	fed, err := federated.New([]federated.Member{
		{Name: "api", Store: api},
		{Name: "worker", Store: worker},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer fed.Close()

	uris := map[string]bool{}
	err = fed.ForEachNode(func(n graph.Node) bool {
		if u := n.Props[uri.PropKey]; u != "" {
			uris[u] = true
		}
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	if !uris["repo://api/svc/handler.go#func:Handle"] || !uris["repo://worker/svc/handler.go#func:Handle"] {
		t.Fatalf("uris=%v", uris)
	}
}

func TestFederatedWritesRejected(t *testing.T) {
	s := memory.New()
	fed, err := federated.New([]federated.Member{{Name: "api", Store: s}})
	if err != nil {
		t.Fatal(err)
	}
	if err := fed.PutNode(graph.Node{ID: "x"}); err == nil {
		t.Fatal("expected write error")
	}
}

func TestSessionIsolatesPins(t *testing.T) {
	api, worker, _ := indexTwinRepos(t)
	base, err := federated.New([]federated.Member{
		{Name: "api", Store: api},
		{Name: "worker", Store: worker},
	})
	if err != nil {
		t.Fatal(err)
	}

	uAPI := "repo://api/svc/handler.go#func:Handle"
	uWorker := "repo://worker/svc/handler.go#func:Handle"
	id := graph.NodeID("func:svc/handler.go#Handle")

	s1 := base.Session()
	if _, err := s1.GetNodeByURI(uAPI); err != nil {
		t.Fatal(err)
	}
	// s1 pinned Handle → api; same Phase-1 id still works on s1.
	if n, err := s1.GetNode(id); err != nil || n.Props[uri.PropKey] != uAPI {
		t.Fatalf("s1 GetNode: %+v %v", n, err)
	}

	// Fresh session has no pins — ambiguous id still conflicts.
	s2 := base.Session()
	if _, err := s2.GetNode(id); !errors.Is(err, graph.ErrConflict) {
		t.Fatalf("s2 want conflict, got %v", err)
	}
	if _, err := s2.GetNodeByURI(uWorker); err != nil {
		t.Fatal(err)
	}
	if n, err := s2.GetNode(id); err != nil || n.Props[uri.PropKey] != uWorker {
		t.Fatalf("s2 GetNode: %+v %v", n, err)
	}

	// Concurrent sessions against long-lived members must not interfere.
	const n = 32
	errCh := make(chan error, n*2)
	for i := 0; i < n; i++ {
		go func() {
			s := base.Session()
			seed, err := rank.ResolveSeed(s, uAPI)
			if err != nil {
				errCh <- err
				return
			}
			_, err = rank.Neighborhood(s, seed, rank.Options{Depth: 1, MaxNodes: 16})
			errCh <- err
		}()
		go func() {
			s := base.Session()
			seed, err := rank.ResolveSeed(s, uWorker)
			if err != nil {
				errCh <- err
				return
			}
			_, err = rank.Neighborhood(s, seed, rank.Options{Depth: 1, MaxNodes: 16})
			errCh <- err
		}()
	}
	for i := 0; i < n*2; i++ {
		if err := <-errCh; err != nil {
			t.Fatal(err)
		}
	}
}

func TestMaxShardsWarnsAndTruncates(t *testing.T) {
	members := make([]federated.Member, 0, 3)
	for _, name := range []string{"a", "b", "c"} {
		s := memory.New()
		_ = s.PutNode(graph.Node{ID: graph.NodeID("file:" + name), Kind: "file", Name: name,
			Props: map[string]string{uri.PropKey: "repo://" + name + "/" + name + "#file"}})
		members = append(members, federated.Member{Name: name, Store: s})
	}
	fed, err := federated.NewWithOptions(members, federated.Options{MaxShards: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer fed.Close()
	if got := fed.MemberNames(); len(got) != 2 {
		t.Fatalf("members=%v", got)
	}
	warns := fed.TakeWarnings()
	if len(warns) == 0 || !strings.Contains(warns[0], "max shards") {
		t.Fatalf("warnings=%v", warns)
	}
}

type slowStore struct {
	graph.Store
	delay time.Duration
}

func (s slowStore) GetNodeByURI(repoURI string) (graph.Node, error) {
	time.Sleep(s.delay)
	return s.Store.GetNodeByURI(repoURI)
}

func (s slowStore) GetNode(id graph.NodeID) (graph.Node, error) {
	time.Sleep(s.delay)
	return s.Store.GetNode(id)
}

func TestLookupTimeout(t *testing.T) {
	inner := memory.New()
	_ = inner.PutNode(graph.Node{ID: "file:x", Kind: "file", Name: "x",
		Props: map[string]string{uri.PropKey: "repo://slow/x#file"}})
	fed, err := federated.NewWithOptions([]federated.Member{
		{Name: "slow", Store: slowStore{Store: inner, delay: 50 * time.Millisecond}},
		{Name: "other", Store: slowStore{Store: memory.New(), delay: 50 * time.Millisecond}},
	}, federated.Options{LookupTimeout: 20 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer fed.Close()
	_, err = fed.GetNodeByURI("repo://missing/x#file")
	if err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("want timeout, got %v", err)
	}
}


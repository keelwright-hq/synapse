package federated_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

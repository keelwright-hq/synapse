package rank_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/keelwright-hq/synapse/internal/contract/bind"
	"github.com/keelwright-hq/synapse/internal/index"
	"github.com/keelwright-hq/synapse/internal/parse"
	"github.com/keelwright-hq/synapse/internal/rank"
	"github.com/keelwright-hq/synapse/internal/store/federated"
	"github.com/keelwright-hq/synapse/internal/store/memory"
)

func workspaceFixtureRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "testdata", "fixtures", "workspace")
}

func indexWorkspaceFixture(t *testing.T) *federated.Store {
	t.Helper()
	root := workspaceFixtureRoot(t)
	apiRoot := filepath.Join(root, "api")
	workerRoot := filepath.Join(root, "worker")

	apiStore := memory.New()
	workerStore := memory.New()
	overlay := memory.New()

	if _, err := index.New(apiStore).Run(apiRoot, index.Options{Repo: "api", Workers: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := index.New(workerStore).Run(workerRoot, index.Options{Repo: "worker", Workers: 1}); err != nil {
		t.Fatal(err)
	}
	if err := bind.Bind(bind.Options{
		Members: []bind.Member{
			{Name: "api", Root: apiRoot, Store: apiStore},
			{Name: "worker", Root: workerRoot, Store: workerStore},
		},
		Overlay: overlay,
	}); err != nil {
		t.Fatal(err)
	}

	fed, err := federated.NewWithOverlay([]federated.Member{
		{Name: "api", Store: apiStore},
		{Name: "worker", Store: workerStore},
	}, overlay)
	if err != nil {
		t.Fatal(err)
	}
	return fed
}

func TestResolveAPIOpenAPIAcrossRepos(t *testing.T) {
	fed := indexWorkspaceFixture(t)
	res, err := rank.ResolveAPI(fed, "GET /users")
	if err != nil {
		t.Fatal(err)
	}
	if res.Operation.Kind != parse.KindOperation {
		t.Fatalf("kind: got %q", res.Operation.Kind)
	}
	if res.RepoURI == "" {
		t.Fatal("expected operation repo_uri")
	}

	repos := map[string]bool{}
	for _, p := range res.Providers {
		if p.RepoURI != "" {
			repos[repoName(p.RepoURI)] = true
		}
		if p.Note == "" {
			t.Fatal("provider missing note")
		}
		if p.Match != bind.MatchOperationID && p.Match != "" {
			t.Fatalf("unexpected match %q", p.Match)
		}
	}
	for _, c := range res.Consumers {
		if c.RepoURI != "" {
			repos[repoName(c.RepoURI)] = true
		}
		if c.Note == "" {
			t.Fatal("consumer missing note")
		}
	}
	if len(res.Providers) == 0 {
		t.Fatal("expected at least one provider")
	}
	if len(res.Consumers) == 0 {
		t.Fatal("expected at least one consumer")
	}
	if len(repos) < 2 {
		t.Fatalf("expected ≥2 repos in results, got %v (providers=%d consumers=%d)", repos, len(res.Providers), len(res.Consumers))
	}
}

func TestListProvidersConsumersGRPC(t *testing.T) {
	fed := indexWorkspaceFixture(t)
	op, err := fed.GetNodeByURI("repo://api/users.proto#operation:UserService.ListUsers")
	if err != nil {
		t.Fatal(err)
	}
	providers, err := rank.ListProviders(fed, op.ID)
	if err != nil {
		t.Fatal(err)
	}
	consumers, err := rank.ListConsumers(fed, op.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(providers) == 0 {
		t.Fatal("expected gRPC providers")
	}
	foundPathLiteral := false
	for _, c := range consumers {
		if c.Match == bind.MatchPathLiteral {
			foundPathLiteral = true
		}
	}
	if !foundPathLiteral && len(consumers) == 0 {
		t.Fatal("expected gRPC consumers (path literal or name fold)")
	}
	_ = foundPathLiteral
}

func repoName(repoURI string) string {
	// repo://name/...
	const prefix = "repo://"
	if len(repoURI) < len(prefix) {
		return ""
	}
	rest := repoURI[len(prefix):]
	for i := 0; i < len(rest); i++ {
		if rest[i] == '/' {
			return rest[:i]
		}
	}
	return rest
}

package rank_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/taricsa/synapse/internal/rank"
)

// Stable projection for golden files (omit volatile node ids / line props).
type apiGoldenHit struct {
	RepoURI string `json:"repo_uri"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Match   string `json:"match,omitempty"`
	Note    string `json:"note"`
	Edge    string `json:"edge"`
}

type apiGoldenCase struct {
	Query     string         `json:"query"`
	OpURI     string         `json:"op_uri"`
	OpName    string         `json:"op_name"`
	Providers []apiGoldenHit `json:"providers"`
	Consumers []apiGoldenHit `json:"consumers"`
}

func TestResolveAPIGoldenOpenAPIandGRPC(t *testing.T) {
	fed := indexWorkspaceFixture(t)

	cases := []string{
		"GET /users",
		"repo://api/users.proto#operation:UserService.ListUsers",
	}
	out := make([]apiGoldenCase, 0, len(cases))
	reposSeen := map[string]bool{}

	for _, q := range cases {
		res, err := rank.ResolveAPI(fed, q)
		if err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		gc := apiGoldenCase{
			Query:  q,
			OpURI:  res.RepoURI,
			OpName: res.Operation.Name,
		}
		for _, p := range res.Providers {
			gc.Providers = append(gc.Providers, projectHit(p))
			if p.RepoURI != "" {
				reposSeen[repoName(p.RepoURI)] = true
			}
		}
		for _, c := range res.Consumers {
			gc.Consumers = append(gc.Consumers, projectHit(c))
			if c.RepoURI != "" {
				reposSeen[repoName(c.RepoURI)] = true
			}
		}
		if res.RepoURI != "" {
			reposSeen[repoName(res.RepoURI)] = true
		}
		out = append(out, gc)
	}

	if len(reposSeen) < 2 {
		t.Fatalf("expected ≥2 repos across golden cases, got %v", reposSeen)
	}

	got, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')

	golden := filepath.Join("testdata", "api_resolve.golden.json")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden (run UPDATE_GOLDEN=1): %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("golden mismatch\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func projectHit(h rank.ContractHit) apiGoldenHit {
	return apiGoldenHit{
		RepoURI: h.RepoURI,
		Name:    h.Node.Name,
		Kind:    h.Node.Kind,
		Match:   h.Match,
		Note:    h.Note,
		Edge:    string(h.Edge),
	}
}

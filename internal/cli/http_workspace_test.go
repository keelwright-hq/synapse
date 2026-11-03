package cli_test

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/keelwright-hq/synapse/internal/cli"
	"github.com/keelwright-hq/synapse/internal/rank"
)

func httpWorkspaceFixtureDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(filepath.Dir(workspaceFixtureDir(t)), "workspace-http")
}

func runCLI(t *testing.T, args ...string) []byte {
	t.Helper()
	cmd := cli.RootCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	resetPersistentFlags(t, cmd)
	for _, sub := range [][]string{{"query", "services"}, {"query", "affected"}, {"query", "dependencies"}} {
		if c, _, err := cmd.Find(sub); err == nil {
			for _, f := range []string{"consumers-of", "calls-from", "json", "repo"} {
				if f == "json" {
					_ = c.Flags().Set(f, "false")
				} else {
					_ = c.Flags().Set(f, "")
				}
			}
		}
	}
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("%v: %v\n%s", args, err, buf.String())
	}
	return buf.Bytes()
}

func TestWorkspaceHTTPLinking(t *testing.T) {
	ws := httpWorkspaceFixtureDir(t)
	data := t.TempDir()
	runCLI(t, "index", "--workspace", ws, "--data-dir", data)

	services := func(args ...string) rank.ServiceResult {
		t.Helper()
		out := runCLI(t, append([]string{"query", "services", "--workspace", ws, "--data-dir", data, "--json"}, args...)...)
		var res rank.ServiceResult
		if err := json.Unmarshal(out, &res); err != nil {
			t.Fatalf("decode: %v\n%s", err, out)
		}
		return res
	}
	sites := func(l rank.ServiceLink) []string {
		var out []string
		for _, s := range l.Sites {
			if s.Route != "" {
				out = append(out, s.Repo+":"+s.Path+" "+s.Symbol+" -> "+s.Route+" "+s.RouteFile+" "+s.Match)
			}
		}
		sort.Strings(out)
		return out
	}

	// Declared env (API_URL belongs to api): a TS wrapper chain and a Python
	// client whose base URL is a settings field; the gateway prefix the
	// Python client writes is stripped before matching.
	consumers := services("--consumers-of", "api")
	if len(consumers.Services) != 2 || consumers.Services[0].Repo != "web" || consumers.Services[1].Repo != "worker" {
		t.Fatalf("api consumers = %+v", consumers.Services)
	}
	wantWeb := []string{
		"web:src/api.ts loadOrders -> GET /v1/users/{}/orders routes/users.js declared_env",
		"web:src/api.ts loadUsers -> GET /v1/users routes/users.js declared_env",
	}
	if got := sites(consumers.Services[0]); strings.Join(got, "\n") != strings.Join(wantWeb, "\n") {
		t.Fatalf("web sites:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(wantWeb, "\n"))
	}
	wantWorker := []string{"worker:client.py ApiClient.user_orders -> GET /v1/users/{}/orders routes/users.js declared_env"}
	if got := sites(consumers.Services[1]); strings.Join(got, "\n") != strings.Join(wantWorker, "\n") {
		t.Fatalf("worker sites: %v", got)
	}
	if env := consumers.Services[1].EnvReads; len(env) != 1 || env[0] != "API_URL" {
		t.Fatalf("worker env reads = %v (pydantic field api_url loads API_URL)", env)
	}

	// No declared target: linked by a unique path match, with the FastAPI
	// router's prefix and the include_router prefix (through a package
	// re-export) composed into the route path.
	calls := services("--calls-from", "web")
	if len(calls.Services) != 2 || calls.Services[1].Repo != "scores" {
		t.Fatalf("web calls = %+v", calls.Services)
	}
	wantScores := []string{"web:src/scores.ts playerScore -> GET /api/v1/scores/player/{} app/routes/player_scores.py path_suffix"}
	if got := sites(calls.Services[1]); strings.Join(got, "\n") != strings.Join(wantScores, "\n") {
		t.Fatalf("scores sites: %v", got)
	}

	// affected crosses the contract: the handler's clients in another repo.
	out := runCLI(t, "query", "affected", "listUsers", "--workspace", ws, "--data-dir", data, "--repo", "api", "--json")
	var res rank.ImpactResult
	if err := json.Unmarshal(out, &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Files) != 1 || res.Files[0].Repo != "web" || res.Files[0].Path != "src/api.ts" ||
		res.Files[0].Symbols[0].Name != "loadUsers" || res.Files[0].Symbols[0].Edge != rank.EdgeCallsAPI {
		t.Fatalf("affected listUsers = %+v", res.Files)
	}

	// dependencies crosses it the other way: a client reaches the route's handler.
	out = runCLI(t, "query", "dependencies", "loadOrders", "--workspace", ws, "--data-dir", data, "--repo", "web", "--json")
	res = rank.ImpactResult{}
	if err := json.Unmarshal(out, &res); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, g := range res.Files {
		if g.Repo == "api" && g.Path == "routes/users.js" {
			for _, s := range g.Symbols {
				if s.Edge == rank.EdgeHandledBy {
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatalf("dependencies loadOrders = %+v, want api:routes/users.js handled_by", res.Files)
	}
}

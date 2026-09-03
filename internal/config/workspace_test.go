package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keelwright-hq/synapse/internal/config"
)

func writeWorkspace(t *testing.T, dir, body string) string {
	t.Helper()
	path := filepath.Join(dir, "synapse.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadHappyPath(t *testing.T) {
	root := t.TempDir()
	api := filepath.Join(root, "api")
	worker := filepath.Join(root, "worker")
	if err := os.MkdirAll(api, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(worker, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgDir := filepath.Join(root, "ws")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeWorkspace(t, cfgDir, `
version: 1
repos:
  - name: api
    path: ../api
  - name: worker
    path: ../worker
`)

	ws, err := config.Load(cfgDir)
	if err != nil {
		t.Fatal(err)
	}
	if ws.Version != 1 || len(ws.Repos) != 2 {
		t.Fatalf("got %+v", ws)
	}
	if ws.Repos[0].Name != "api" || ws.Repos[0].Path != api {
		t.Fatalf("api member: %+v want path %s", ws.Repos[0], api)
	}
	if ws.Repos[1].Name != "worker" || ws.Repos[1].Path != worker {
		t.Fatalf("worker member: %+v want path %s", ws.Repos[1], worker)
	}
	roots := ws.RepoRoots()
	if roots["api"] != api || roots["worker"] != worker {
		t.Fatalf("RepoRoots: %v", roots)
	}
}

func TestLoadDuplicateNames(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeWorkspace(t, root, `
version: 1
repos:
  - name: api
    path: a
  - name: api
    path: a
`)
	_, err := config.Load(root)
	if err == nil || !strings.Contains(err.Error(), "duplicate repo name") {
		t.Fatalf("want duplicate name error, got %v", err)
	}
}

func TestLoadMissingPath(t *testing.T) {
	root := t.TempDir()
	writeWorkspace(t, root, `
version: 1
repos:
  - name: api
    path: does-not-exist
`)
	_, err := config.Load(root)
	if err == nil || !strings.Contains(err.Error(), "does-not-exist") {
		t.Fatalf("want missing path error, got %v", err)
	}
}

func TestLoadInvalidName(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeWorkspace(t, root, `
version: 1
repos:
  - name: bad/name
    path: a
`)
	_, err := config.Load(root)
	if err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("want invalid name error, got %v", err)
	}
}

func TestLoadBadVersion(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeWorkspace(t, root, `
version: 99
repos:
  - name: api
    path: a
`)
	_, err := config.Load(root)
	if err == nil || !strings.Contains(err.Error(), "unsupported version") {
		t.Fatalf("want version error, got %v", err)
	}
}

func TestLoadEmptyRepos(t *testing.T) {
	root := t.TempDir()
	writeWorkspace(t, root, "version: 1\nrepos: []\n")
	_, err := config.Load(root)
	if err == nil || !strings.Contains(err.Error(), "at least one repo") {
		t.Fatalf("want empty repos error, got %v", err)
	}
}

func TestLoadFilePath(t *testing.T) {
	root := t.TempDir()
	api := filepath.Join(root, "api")
	if err := os.MkdirAll(api, 0o755); err != nil {
		t.Fatal(err)
	}
	file := writeWorkspace(t, root, `
version: 1
repos:
  - name: api
    path: api
`)
	ws, err := config.Load(file)
	if err != nil {
		t.Fatal(err)
	}
	if ws.Repos[0].Path != api {
		t.Fatalf("path=%s want %s", ws.Repos[0].Path, api)
	}
}

func TestLookup(t *testing.T) {
	root := t.TempDir()
	api := filepath.Join(root, "api")
	if err := os.MkdirAll(api, 0o755); err != nil {
		t.Fatal(err)
	}
	writeWorkspace(t, root, `
version: 1
repos:
  - name: api
    path: api
`)
	ws, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	r, err := ws.Lookup("api")
	if err != nil || r.Path != api {
		t.Fatalf("Lookup: %+v %v", r, err)
	}
	if _, err := ws.Lookup("missing"); err == nil {
		t.Fatal("expected missing member error")
	}
}

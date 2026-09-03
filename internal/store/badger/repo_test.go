package badger_test

import (
	"path/filepath"
	"testing"

	"github.com/keelwright-hq/synapse/internal/store/badger"
)

func TestRepoDirAndOpenRepo(t *testing.T) {
	dataDir := t.TempDir()
	dir := badger.RepoDir(dataDir, "api")
	want := filepath.Join(dataDir, "repos", "api")
	if dir != want {
		t.Fatalf("RepoDir=%s want %s", dir, want)
	}
	s, err := badger.OpenRepo(dataDir, "api")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.GetNode("missing"); err == nil {
		t.Fatal("expected not found on empty store")
	}
}

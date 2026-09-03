package uri_test

import (
	"testing"

	"github.com/taricsa/synapse/internal/uri"
)

func TestEdgeCasesBranchesMonorepoConflicts(t *testing.T) {
	t.Run("monorepo deep path", func(t *testing.T) {
		got, err := uri.Build("apps", "packages/foo/src/bar.ts", uri.KindFunc, "run")
		if err != nil {
			t.Fatal(err)
		}
		want := "repo://apps/packages/foo/src/bar.ts#func:run"
		if got != want {
			t.Fatalf("got %q want %q", got, want)
		}
	})

	t.Run("encoding in import symbol", func(t *testing.T) {
		raw, err := uri.Build("r", "a.go", uri.KindImport, "path with space")
		if err != nil {
			t.Fatal(err)
		}
		u, err := uri.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if u.Symbol != "path with space" {
			t.Fatalf("symbol %q", u.Symbol)
		}
	})

	t.Run("same entity identity", func(t *testing.T) {
		a, _ := uri.Build("r", "x.go", uri.KindFunc, "F")
		b, _ := uri.Normalize("repo://r/x.go#func:F")
		if a != b {
			t.Fatalf("%q != %q", a, b)
		}
	})

	t.Run("reject query", func(t *testing.T) {
		if err := uri.Validate("repo://r/x.go#file?x=1"); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("file fragment has no symbol", func(t *testing.T) {
		_, err := uri.Build("r", "x.go", uri.KindFile, "nope")
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("cross-repo same path", func(t *testing.T) {
		a, _ := uri.Build("api", "svc/h.go", uri.KindFunc, "H")
		b, _ := uri.Build("worker", "svc/h.go", uri.KindFunc, "H")
		if a == b {
			t.Fatal("expected different uris")
		}
	})
}

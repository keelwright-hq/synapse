package uri

import (
	"errors"
	"strings"
	"testing"
)

func TestBuildParseRoundTrip(t *testing.T) {
	cases := []struct {
		repo, path, kind, sym, want string
	}{
		{"synapse", "internal/parse/builder.go", KindFile, "", "repo://synapse/internal/parse/builder.go#file"},
		{"synapse", "internal/parse/builder.go", KindPackage, "parse", "repo://synapse/internal/parse/builder.go#package:parse"},
		{"synapse", "internal/parse/builder.go", KindFunc, "newBuilder", "repo://synapse/internal/parse/builder.go#func:newBuilder"},
		{"synapse", "internal/parse/builder.go", KindMethod, "Builder.Put", "repo://synapse/internal/parse/builder.go#method:Builder.Put"},
		{"synapse", "internal/parse/builder.go", KindType, "Builder", "repo://synapse/internal/parse/builder.go#type:Builder"},
		{"synapse", "internal/parse/builder.go", KindImport, "github.com/keelwright-hq/synapse/internal/graph", "repo://synapse/internal/parse/builder.go#import:github.com/keelwright-hq/synapse/internal/graph"},
		{"synapse", "internal/parse/builder.go", KindSymbol, "Name", "repo://synapse/internal/parse/builder.go#symbol:Name"},
	}
	for _, tc := range cases {
		got, err := Build(tc.repo, tc.path, tc.kind, tc.sym)
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		if got != tc.want {
			t.Fatalf("Build got %q want %q", got, tc.want)
		}
		u, err := Parse(got)
		if err != nil {
			t.Fatalf("Parse(%q): %v", got, err)
		}
		if u.Repo != tc.repo || u.Path != tc.path || u.Kind != tc.kind || u.Symbol != tc.sym {
			t.Fatalf("Parse mismatch: %+v", u)
		}
		norm, err := Normalize(got)
		if err != nil || norm != tc.want {
			t.Fatalf("Normalize: %q %v", norm, err)
		}
	}
}

func TestInvalidForms(t *testing.T) {
	bad := []string{
		"",
		"http://synapse/x#file",
		"repo://synapse/x#file?x=1",
		"repo://synapse/x",
		"repo:///x#file",
		"repo://bad repo/x#file",
		"repo://synapse/#file",
		"repo://synapse/x#",
		"repo://synapse/x#file:extra",
		"repo://synapse/../etc#file",
	}
	for _, s := range bad {
		if err := Validate(s); err == nil {
			t.Fatalf("expected error for %q", s)
		}
	}
}

func TestNormalizeSlashesAndEncoding(t *testing.T) {
	got, err := Build("synapse", `packages\foo\bar.ts`, KindFunc, "Hello")
	if err != nil {
		t.Fatal(err)
	}
	want := "repo://synapse/packages/foo/bar.ts#func:Hello"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}

	// Symbol with space / hash
	got, err = Build("synapse", "a.go", KindImport, "foo bar#baz")
	if err != nil {
		t.Fatal(err)
	}
	u, err := Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	if u.Symbol != "foo bar#baz" {
		t.Fatalf("symbol round-trip: %q", u.Symbol)
	}
}

func TestMonorepoDeepPath(t *testing.T) {
	got, err := Build("monorepo", "packages/foo/src/bar.ts", KindFunc, "Baz")
	if err != nil {
		t.Fatal(err)
	}
	want := "repo://monorepo/packages/foo/src/bar.ts#func:Baz"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestKindTokenMapping(t *testing.T) {
	tok, err := KindToken("function")
	if err != nil || tok != KindFunc {
		t.Fatalf("function -> %q %v", tok, err)
	}
	nk, err := NodeKind(KindFunc)
	if err != nil || nk != "function" {
		t.Fatalf("func -> %q %v", nk, err)
	}
}

func TestParseLegacyAndFromLegacy(t *testing.T) {
	cases := []struct {
		id   string
		want string
		ok   bool
	}{
		{"file:internal/parse/builder.go", "repo://synapse/internal/parse/builder.go#file", true},
		{"func:a.go#Alpha", "repo://synapse/a.go#func:Alpha", true},
		{"method:a.go#Greeter.Greet", "repo://synapse/a.go#method:Greeter.Greet", true},
		{"symbol:Printf", "", false},
		{"package:a.go#main", "repo://synapse/a.go#package:main", true},
	}
	for _, tc := range cases {
		got, ok, err := FromLegacy("synapse", tc.id)
		if err != nil {
			t.Fatalf("%s: %v", tc.id, err)
		}
		if ok != tc.ok || got != tc.want {
			t.Fatalf("%s: got (%q,%v) want (%q,%v)", tc.id, got, ok, tc.want, tc.ok)
		}
	}
}

func TestAssignMethodUsesReceiver(t *testing.T) {
	got, ok, err := Assign("synapse", "a.go", "method", "Greet", "method:a.go#Greeter.Greet")
	if err != nil || !ok {
		t.Fatalf("err=%v ok=%v", err, ok)
	}
	if !strings.Contains(got, "#method:Greeter.Greet") {
		t.Fatalf("got %q", got)
	}
}

func TestParseLiteralPercentInSymbol(t *testing.T) {
	raw, err := Build("r", "a.go", KindImport, "100%_pure")
	if err != nil {
		t.Fatal(err)
	}
	u, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse(%q): %v", raw, err)
	}
	if u.Symbol != "100%_pure" {
		t.Fatalf("symbol %q", u.Symbol)
	}
	// Encoded form in the wire string should survive a second Parse without
	// treating a decoded percent as an escape sequence.
	again, err := Parse(u.String())
	if err != nil || again.Symbol != "100%_pure" {
		t.Fatalf("round-trip: %+v %v", again, err)
	}
}

func TestRewriteRepo(t *testing.T) {
	got, err := RewriteRepo("repo://api/svc/handler.go#func:ListUsers", "api", "renamed")
	if err != nil {
		t.Fatal(err)
	}
	want := "repo://renamed/svc/handler.go#func:ListUsers"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	plain, err := RewriteRepo("func:a.go#A", "api", "renamed")
	if err != nil || plain != "func:a.go#A" {
		t.Fatalf("phase-1: %q %v", plain, err)
	}
	same, err := RewriteRepo("repo://api/a.go#file", "other", "renamed")
	if err != nil || same != "repo://api/a.go#file" {
		t.Fatalf("other repo: %q %v", same, err)
	}
}

func TestErrorsAreInvalid(t *testing.T) {
	_, err := Parse("not-a-uri")
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("want ErrInvalid, got %v", err)
	}
}

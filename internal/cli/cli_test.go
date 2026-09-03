package cli_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/taricsa/synapse/internal/buildinfo"
	"github.com/taricsa/synapse/internal/cli"
)

func TestRootHelpListsCommands(t *testing.T) {
	cmd := cli.RootCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("help: %v", err)
	}
	out := buf.String()
	for _, name := range []string{"version", "index", "mcp", "query", "graph"} {
		if !strings.Contains(out, name) {
			t.Errorf("help missing command %q; got:\n%s", name, out)
		}
	}
}

func TestVersionCommand(t *testing.T) {
	cmd := cli.RootCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"version"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("version: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "synapse") {
		t.Fatalf("expected synapse in version output, got %q", out)
	}
	if !strings.Contains(out, buildinfo.Version) {
		t.Fatalf("expected version %q in output, got %q", buildinfo.Version, out)
	}
}

func TestBuildinfoDefaults(t *testing.T) {
	if buildinfo.Version == "" {
		t.Fatal("Version must be non-empty")
	}
	if buildinfo.Commit == "" {
		t.Fatal("Commit must be non-empty")
	}
	if buildinfo.Date == "" {
		t.Fatal("Date must be non-empty")
	}
}

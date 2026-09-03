package cli_test

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/taricsa/synapse/internal/cli"
	"github.com/taricsa/synapse/internal/parse"
	"github.com/taricsa/synapse/internal/store/badger"
	"github.com/taricsa/synapse/internal/store/federated"
)

func TestWorkspaceOpenAPIContractEdges(t *testing.T) {
	wsDir := workspaceFixtureDir(t)
	dataDir := t.TempDir()

	cmd := cli.RootCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	resetPersistentFlags(t, cmd)
	cmd.SetArgs([]string{
		"index",
		"--workspace", wsDir,
		"--data-dir", dataDir,
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("index --workspace: %v\n%s", err, buf.String())
	}

	apiStore, err := badger.OpenRepo(dataDir, "api")
	if err != nil {
		t.Fatal(err)
	}
	defer apiStore.Close()

	opURI := "repo://api/openapi.yaml#operation:GET /users"
	op, err := apiStore.GetNodeByURI(opURI)
	if err != nil {
		t.Fatalf("operation uri: %v", err)
	}
	if op.Kind != parse.KindOperation {
		t.Fatalf("kind: %s", op.Kind)
	}
	if _, err := apiStore.GetNodeByURI("repo://api/openapi.yaml#schema:User"); err != nil {
		t.Fatalf("schema uri: %v", err)
	}

	handler, err := apiStore.GetNodeByURI("repo://api/svc/handler.go#func:ListUsers")
	if err != nil {
		t.Fatal(err)
	}
	implEdges, err := apiStore.OutEdges(handler.ID, parse.EdgeImplements)
	if err != nil {
		t.Fatal(err)
	}
	foundImpl := false
	for _, e := range implEdges {
		if e.To == op.ID {
			foundImpl = true
		}
	}
	if !foundImpl {
		t.Fatalf("expected ListUsers --implements→ GET /users, edges=%+v", implEdges)
	}

	workerStore, err := badger.OpenRepo(dataDir, "worker")
	if err != nil {
		t.Fatal(err)
	}
	defer workerStore.Close()

	overlay, err := badger.OpenOverlay(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer overlay.Close()

	fed, err := federated.NewWithOverlay([]federated.Member{
		{Name: "api", Store: apiStore},
		{Name: "worker", Store: workerStore},
	}, overlay)
	if err != nil {
		t.Fatal(err)
	}
	defer fed.Close()

	client, err := fed.GetNodeByURI("repo://worker/svc/handler.go#func:FetchUsers")
	if err != nil {
		t.Fatal(err)
	}
	consumeEdges, err := fed.OutEdges(client.ID, parse.EdgeConsumes)
	if err != nil {
		t.Fatal(err)
	}
	foundConsume := false
	for _, e := range consumeEdges {
		if e.To == op.ID {
			foundConsume = true
		}
	}
	if !foundConsume {
		t.Fatalf("expected FetchUsers --consumes→ GET /users via overlay, edges=%+v overlayDir=%s",
			consumeEdges, filepath.Join(dataDir, "overlay"))
	}

	inEdges, err := fed.InEdges(op.ID, parse.EdgeConsumes)
	if err != nil {
		t.Fatal(err)
	}
	foundIn := false
	for _, e := range inEdges {
		if e.From == client.ID {
			foundIn = true
		}
	}
	if !foundIn {
		t.Fatalf("expected InEdges consumes from FetchUsers, edges=%+v", inEdges)
	}
}

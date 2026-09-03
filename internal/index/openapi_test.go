package index_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/keelwright-hq/synapse/internal/index"
	"github.com/keelwright-hq/synapse/internal/parse"
	"github.com/keelwright-hq/synapse/internal/store/memory"
)

func TestIndexerIndexesOpenAPISpec(t *testing.T) {
	root := t.TempDir()
	spec := filepath.Join(root, "openapi.yaml")
	const sampleYAML = `openapi: 3.0.3
info:
  title: Sample API
  version: 1.0.0
paths:
  /users:
    get:
      operationId: ListUsers
      responses:
        "200":
          description: ok
components:
  schemas:
    User:
      type: object
`
	if err := os.WriteFile(spec, []byte(sampleYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	// Non-OpenAPI yaml should be ignored.
	if err := os.WriteFile(filepath.Join(root, "compose.yaml"), []byte("version: '3'\nservices: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	store := memory.New()
	stats, err := index.New(store).Run(root, index.Options{Repo: "demo", Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Processed < 1 {
		t.Fatalf("expected processed openapi, stats=%+v", stats)
	}

	op, err := store.GetNodeByURI("repo://demo/openapi.yaml#operation:GET /users")
	if err != nil {
		t.Fatal(err)
	}
	if op.Kind != parse.KindOperation || op.Props["operation_id"] != "ListUsers" {
		t.Fatalf("operation: %+v", op)
	}
	if _, err := store.GetNodeByURI("repo://demo/openapi.yaml#schema:User"); err != nil {
		t.Fatal(err)
	}
}

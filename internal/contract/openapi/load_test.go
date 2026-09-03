package openapi_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/keelwright-hq/synapse/internal/contract/openapi"
)

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
      properties:
        id:
          type: string
`

const sampleJSON = `{
  "openapi": "3.0.3",
  "info": {"title": "Sample API", "version": "1.0.0"},
  "paths": {
    "/users": {
      "get": {
        "operationId": "ListUsers",
        "responses": {"200": {"description": "ok"}}
      }
    }
  }
}`

func TestLoadBytesYAML(t *testing.T) {
	doc, err := openapi.LoadBytes([]byte(sampleYAML))
	if err != nil {
		t.Fatal(err)
	}
	if doc.OpenAPI != "3.0.3" {
		t.Fatalf("version: got %q", doc.OpenAPI)
	}
	if doc.Paths == nil || doc.Paths.Find("/users") == nil || doc.Paths.Find("/users").Get == nil {
		t.Fatal("expected GET /users")
	}
}

func TestLoadBytesJSON(t *testing.T) {
	doc, err := openapi.LoadBytes([]byte(sampleJSON))
	if err != nil {
		t.Fatal(err)
	}
	if doc.Paths.Find("/users").Get.OperationID != "ListUsers" {
		t.Fatalf("operationId: %q", doc.Paths.Find("/users").Get.OperationID)
	}
}

func TestLoadFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "openapi.yaml")
	if err := os.WriteFile(path, []byte(sampleYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	doc, err := openapi.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Info.Title != "Sample API" {
		t.Fatalf("title: %q", doc.Info.Title)
	}
}

func TestLooksLikeOpenAPIRejectsCompose(t *testing.T) {
	compose := []byte("version: '3'\nservices:\n  web:\n    image: nginx\n")
	if openapi.LooksLikeOpenAPI(compose) {
		t.Fatal("docker-compose should not look like OpenAPI")
	}
	if _, err := openapi.LoadBytes(compose); err == nil {
		t.Fatal("expected load error")
	}
}

# OpenAPI contract parsing

Synapse indexes OpenAPI 3.x YAML/JSON into the code graph so frontend clients
and backend handlers can link across repos (SYN-13).

## Discovery

During `synapse index`, after the tree-sitter source walk, Synapse content-sniffs
`.yaml` / `.yml` / `.json` files for an `openapi: 3.` version field. Ordinary
config files (for example Compose) are ignored.

## Graph IR

| Node kind | Phase-1 id | URI token | Notes |
|-----------|------------|-----------|--------|
| `operation` | `operation:{spec}#{METHOD} {path}` | `operation` | Props: `method`, `path`, `operation_id` |
| `schema` | `schema:{spec}#{Name}` | `schema` | From `components.schemas` |

The spec file node `--contains→` each operation and schema. Example:

```
repo://api/openapi.yaml#operation:GET%20/users
repo://api/openapi.yaml#schema:User
```

(Spaces in operation symbols are percent-encoded in the canonical URI; lookups
accept either form.)

## Binding edges

After indexing (including every workspace member), a heuristic binder writes:

| Edge | Meaning |
|------|---------|
| `implements` | Handler/function → operation (same repo as the spec, `operationId` ≈ symbol name) |
| `consumes` | Client call site → operation (`operationId` match in another repo, or path string literal in source) |

Same-repo edges live in the member graph. Cross-repo edges live in the workspace
**overlay** store at `{data-dir}/overlay/`, with stub node IDs equal to each
endpoint’s `repo://` URI. Federated query merges overlay edges and resolves far
ends via `GetNodeByURI`.

## Workspace fixture

`testdata/fixtures/workspace/` pairs:

- `api/openapi.yaml` + `ListUsers` handler (`implements`)
- `worker` `FetchUsers` with `"/users"` literal (`consumes` via overlay)

```bash
./synapse index --workspace testdata/fixtures/workspace --data-dir /tmp/synapse-ws
./synapse query neighborhood 'repo://worker/svc/handler.go#func:FetchUsers' \
  --workspace testdata/fixtures/workspace --data-dir /tmp/synapse-ws --json
```

## Loader

Parsing uses [kin-openapi](https://github.com/getkin/kin-openapi). Package:
`internal/contract/openapi`. Binding: `internal/contract/bind`.

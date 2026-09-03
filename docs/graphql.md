# GraphQL contract parsing

Synapse indexes GraphQL SDL schemas into the code graph so resolvers and
clients can link across repos (SYN-9).

## Discovery

During `synapse index`, after the tree-sitter source walk and OpenAPI sniff,
Synapse walks `.graphql` / `.gql` / `.graphqls` files and content-sniffs for
SDL keywords (`type`, `schema`, `interface`, `enum`, `input`, `union`,
`scalar`, `directive`, `extend`). Ordinary text files with those extensions
are ignored.

## Graph IR

| Node kind | Phase-1 id | URI token | Notes |
|-----------|------------|-----------|--------|
| `type` | `type:{spec}#{Name}` | `type` | Named types; props: `gql_kind` |
| `field` | `field:{spec}#{Type.field}` | `field` | Non-root fields; props: `parent`, `return_type` |
| `operation` | `operation:{spec}#{query\|mutation\|subscription} {Field}` | `operation` | Root fields; props: `operation_id`, `gql_root` |

The spec file node `--contains→` each type and operation. Types
`--contains→` their non-root fields. Built-in scalars from the GraphQL
prelude are omitted. Example:

```
repo://api/schema.graphql#type:User
repo://api/schema.graphql#field:User.name
repo://api/schema.graphql#operation:query%20users
```

(Spaces in operation symbols are percent-encoded in the canonical URI; lookups
accept either form.)

## Binding edges

After indexing (including every workspace member), the shared heuristic binder
writes the same edges as OpenAPI:

| Edge | Meaning |
|------|---------|
| `implements` | Resolver/function → operation (same repo as the schema) |
| `consumes` | Client symbol → operation (name match in another repo) |

Name matching folds alphanumerics and lowercases. For GraphQL operations it
also tries `Resolve{Field}`, `Get{Field}`, and `{Root}_{Field}` (e.g.
`Query_users`).

Same-repo edges live in the member graph. Cross-repo edges live in the
workspace **overlay** store at `{data-dir}/overlay/`, with stub node IDs equal
to each endpoint’s `repo://` URI.

## Workspace fixture

`testdata/fixtures/workspace/` pairs:

- `api/schema.graphql` + `Users` resolver (`implements`)
- `worker` `Users` by name match (`consumes` via overlay)

```bash
./synapse index --workspace testdata/fixtures/workspace --data-dir /tmp/synapse-ws
./synapse query neighborhood 'repo://api/schema.graphql#operation:query users' \
  --workspace testdata/fixtures/workspace --data-dir /tmp/synapse-ws --json
```

## Loader

Parsing uses [gqlparser](https://github.com/vektah/gqlparser) **`parser.ParseSchema`**
(syntax only). Synapse does **not** run full schema validation via
`gqlparser.LoadSchema`, so split schemas (types referenced across files) still
index file-by-file. Package: `internal/contract/graphql`. Binding:
`internal/contract/bind`.

# Protobuf / gRPC contract parsing

Synapse indexes Protobuf (proto3) sources into the code graph so gRPC clients
and servers can link across repos (SYN-15).

## Discovery

During `synapse index`, after OpenAPI and GraphQL discovery, Synapse walks
`.proto` files and content-sniffs for Protobuf cues (`syntax =`, `package`,
`import`, `service`, `message`, `enum`). Ordinary text files with a `.proto`
extension are ignored.

## Graph IR

| Node kind | Phase-1 id | URI token | Notes |
|-----------|------------|-----------|--------|
| `service` | `service:{spec}#{Name}` | `service` | gRPC service |
| `operation` | `operation:{spec}#{Service.Method}` | `operation` | RPC; props: `operation_id`, `service`, `grpc_path` |
| `schema` | `schema:{spec}#{Message}` | `schema` | Messages |
| `field` | `field:{spec}#{Message.field}` | `field` | Message fields |

The spec file `--contains→` each service, operation, and message. Services
`--contains→` their RPCs. Messages `--contains→` their fields. Example:

```
repo://api/users.proto#service:UserService
repo://api/users.proto#operation:UserService.ListUsers
repo://api/messages.proto#schema:User
repo://api/messages.proto#field:User.name
```

`grpc_path` is the conventional method path `/{package}.{Service}/{Method}`
(also mirrored as `path` for binder literal matching).

## Imports

Parsing uses [protocompile](https://github.com/bufbuild/protocompile) with
include paths defaulting to the indexed repo root. Imports such as
`import "messages.proto";` resolve against those roots (plus well-known types).

If an import is missing, Synapse **soft-fails**: the primary file is still
parsed syntactically so services/rpcs/messages in that file are indexed.

## Binding edges

| Edge | Meaning |
|------|---------|
| `implements` | Stub/handler → RPC (same repo; method name or `{Service}_{Method}`) |
| `consumes` | Client → RPC (cross-repo name match, or `grpc_path` string literal) |

Same-repo edges live in the member graph. Cross-repo edges live in the
workspace **overlay** store.

## Workspace fixture

`testdata/fixtures/workspace/` pairs:

- `api/messages.proto` + `api/users.proto` (import) + `ListUsers` (`implements`)
- `worker` `CallListUsers` with `"/users.UserService/ListUsers"` (`consumes` via overlay)

```bash
./synapse index --workspace testdata/fixtures/workspace --data-dir /tmp/synapse-ws
./synapse query neighborhood 'repo://api/users.proto#operation:UserService.ListUsers' \
  --workspace testdata/fixtures/workspace --data-dir /tmp/synapse-ws --json
```

## Loader

Package: `internal/contract/protobuf`. Binding: `internal/contract/bind`.

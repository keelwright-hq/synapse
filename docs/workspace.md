# Polyrepo workspace setup

Synapse can index and query multiple repositories as one workspace (SYN-12).
Each member keeps its own Badger database under `--data-dir`; queries either
**scope** to one repo or **federate** across all.

## `synapse.yaml`

Place a config file at the workspace root (or pass its path to `--workspace`):

```yaml
version: 1
repos:
  - name: api      # logical {repo} in repo:// URIs
    path: ./api   # relative to this file’s directory
  - name: worker
    path: ./worker
```

Rules:

- `version` must be `1`
- Every repo needs a non-empty `name` and `path`
- Names must be unique and URL-safe (`[A-Za-z0-9._-]+`)
- Paths must exist and be directories (resolved relative to the yaml file)
- Duplicate logical names are invalid — pick distinct names (aliases) if two checkouts would otherwise collide

## Index

```bash
./synapse index --workspace path/to/synapse.yaml --data-dir .synapse
```

This writes:

```
.synapse/repos/api/graph/
.synapse/repos/worker/graph/
.synapse/overlay/              # cross-repo implements/consumes (OpenAPI binder)
```

After each member is indexed, Synapse runs a contract binder. Same-repo edges
land in the member graph; cross-repo OpenAPI links go in the overlay (see
[openapi.md](openapi.md)).

Each member is its own Badger database. Federated queries wrap those stores in a
**per-query** `federated.Store` (isolated Phase-1 ID pins); do not share one
federated wrapper across concurrent requests — keep the Badger members open and
call `New` / `Session` once per query.

Single-repo `synapse index .` still uses `.synapse/graph/` unchanged.

Do not pass a positional path together with `--workspace`.

## Query

Federate (default with `--workspace`):

```bash
./synapse query neighborhood 'repo://api/svc/handler.go#func:Handle' \
  --workspace path/to/synapse.yaml --data-dir .synapse --json
```

Scope to one member with `--repo`:

```bash
./synapse query neighborhood Handle \
  --workspace path/to/synapse.yaml --data-dir .synapse --repo api --json
```

Bare symbol names that match in more than one repo are rejected — use `--repo`
or a `repo://` URI.

## See also

- [`repo-uri.md`](repo-uri.md) — `repo://` grammar
- Fixture: `testdata/fixtures/workspace/`

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

A member can also declare how other members reach it over HTTP, so client
calls link to its routes without a spec file (see
[http-linking.md](http-linking.md)):

```yaml
  - name: api
    path: ./api
    http:
      base_env: [API_URL]       # env vars other members read to reach api
      gateway_prefix: /api-svc  # optional: stripped from client paths
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
land in the member graph; cross-repo links go in the overlay. Spec operations
bind as described in [openapi.md](openapi.md); routes and HTTP clients found in
code bind as described in [http-linking.md](http-linking.md).

Each member is its own Badger database. Federated queries wrap those stores in a
**per-query** `federated.Store` (isolated Phase-1 ID pins); do not share one
federated wrapper across concurrent requests — keep the Badger members open and
call `New` / `Session` once per query.

Single-repo `synapse index .` (or `synapse index /path/to/repo`) defaults
`--data-dir` to **`<target-repo>/.synapse`** when the flag is omitted—the
embedded Badger graph database for that repository, not a human-readable
report. Pass `--report` for readable artifacts under `.synapse-out/`
([report.md](report.md)). Workspace `--data-dir` layout below is unchanged
(out of scope for SYN-98 / SYN-99).

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

`affected` and `dependencies` always open the whole workspace, so their walk
can cross from a route handler to its clients in other members; there `--repo`
only picks the member the seed is in. `query services` lists HTTP
dependencies between members:

```bash
./synapse query services --workspace path/to/synapse.yaml --consumers-of api --sites 5
```

## See also

- [`repo-uri.md`](repo-uri.md) — `repo://` grammar
- [`federation.md`](federation.md) — NDJSON snapshots, soft-fail missing shards, guardrails
- [`mcp.md`](mcp.md) — IDE MCP setup, including workspace `resolve_api` tools
- Fixture: `testdata/fixtures/workspace/`

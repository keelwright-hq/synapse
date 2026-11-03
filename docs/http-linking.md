# HTTP route and client linking

Synapse links HTTP client calls in one workspace member to the route handlers
that serve them in another, without OpenAPI, GraphQL or protobuf files. The
links feed `synapse query services`, and `affected` / `dependencies` follow
them across repos.

## What is extracted

At parse time, JS/TS and Python files record routes, router mounts and client
calls as props on their module and function nodes. At index time Synapse
resolves them into graph facts.

**Routes** become `operation` nodes with `source=code`, at their full path:

| Framework | Recognized |
|-----------|------------|
| Express, Hono, Koa | `x.get/post/put/patch/delete/all(path, …handlers)`, `router.route(path).get(h)`, `app.use(prefix, router)`, `app.route(prefix, sub)`; routers from `express()`, `express.Router()`, `new Hono()` |
| FastAPI, Flask | `@router.get(path)`, `@app.route(path, methods=[…])`, `APIRouter(prefix=…)`, `include_router(r, prefix=…)`, `Blueprint(url_prefix=…)`, `register_blueprint` |
| Next.js | `app/**/route.ts` exporting `GET`/`POST`/…, and `pages/api/**` |

- Mount prefixes are composed across files: a mount names an imported router
  (`require('./routes/users')`, `from app.routes import users_router`), which
  is followed to the module that creates it, including through a package
  `__init__.py` that re-exports it.
- A same-file helper that mounts its arguments
  (`function mount(suffix, ...h) { app.use(`/v1${suffix}`, ...h) }`) is
  expanded at its call sites.
- The handler is the named function passed to the route, or the route file's
  module for an inline callback. It `implements` the route.

**Client calls** are recorded on the function (or module) that makes them:

- `fetch`, injected fetches (`fetchImpl`), `axios` and `axios.create`
  instances, `got`, `ky`, `httpx` / `requests` and `httpx.Client(base_url=…)`
  instances.
- Calls through wrapper functions: a function whose path parameter reaches a
  client (`apiGet(path)`, `request(method, path)`, `BackendClient.get(endpoint)`)
  is a wrapper, and calls to it are client calls. Wrappers of wrappers are
  followed up to five levels.
- A method named like an HTTP verb on an unresolved receiver
  (`clients.insights.get('/x')`) still counts.

**Strings are evaluated statically**, only as far as literals, templates,
`+`, f-strings, same-file constants, locals and the enclosing function's
parameters go. A URL is read as *base + tail*:

- The leading dynamic part is the base. The identifiers it is built from are
  its *tokens*: env var names (`process.env.X`, `import.meta.env.X`,
  `os.environ["X"]`, `os.getenv("X")`, `config.X`), pydantic settings fields
  (`settings.api_url` is `API_URL`, including `validation_alias` names), and
  helper functions, which stand for the env vars they read even from another
  file.
- The rest, from the first literal, is the tail. Parameters become `{}` and the
  query string is dropped.

Normalizers such as `BASE.replace(/\/api$/, '')` are never evaluated, because
only the tail is matched.

## Matching

Calls are matched in `synapse index --workspace`, by the binder.

1. **Declared:** if the call's tokens name a `base_env` variable of exactly one
   member, the call targets that member. Its `gateway_prefix` is stripped from
   the tail. The edge has `provenance=EXTRACTED`, `match=declared_env`.
2. **Inferred:** if no member is declared for the tokens, the tail is matched
   against every member's routes.
   - It needs at least two specific path segments (`api`, `v1`, `health` and the
     like don't count).
   - Exactly one other member must have a match.
   - If the caller's own member also matches (a test calling its own API), the
     call is not linked.
   - The edge has `provenance=INFERRED`, `match=path_suffix`.

A route matches when its path ends with the tail, segment by segment, with
`{}` matching anything. The part of the route before the tail must be a prefix
that many of the member's routes share (`/api/v1`): that is the part a base URL
supplies. So `/athletes` matches `/api/v1/athletes` but not
`/api/v1/teams/{}/athletes`.

The best match wins. A tie is linked only when it is one declared route mounted
at several prefixes; any other tie leaves the call unlinked.

Members that read another member's declared variable also get an
`env_read` edge from the reading file to that member's service node. That way
`query services` lists the dependency even where no call site matched a route.

## Configuration

```yaml
version: 1
repos:
  - name: api
    path: ./api
    http:
      base_env: [API_URL, API_BASE_URL]  # env vars other members read to reach api
      gateway_prefix: /api-svc           # stripped from client paths (a gateway in front of api)
  - name: web
    path: ./web
```

`http` is optional. Without it, only inferred links are made.

## Queries

```bash
synapse query services --workspace synapse.yaml --consumers-of api --sites 5
synapse query services --workspace synapse.yaml --consumers-of api --depth 3   # callers of callers
synapse query services --workspace synapse.yaml --calls-from web
synapse query affected listUsers --workspace synapse.yaml --repo api           # reaches web's clients
synapse query dependencies loadUsers --workspace synapse.yaml --repo web       # reaches api's handler
```

In workspace mode, `affected` and `dependencies` open every member. `--repo`
only says which member the seed is in. Results print as `repo:path`.

## Limits

These are not linked:

- Base URLs or paths built in another file: a constant imported from another
  module, or a `base` key looked up in a map of base URLs.
- Base URLs taken from request headers or other runtime input.
- Pub/Sub push subscriptions and scheduler jobs that call routes.
- gRPC and GraphQL clients without spec files ([openapi.md](openapi.md),
  [graphql.md](graphql.md), [protobuf.md](protobuf.md) cover specs).

Two other limits apply:

- Ambiguous matches are dropped, never guessed, so recall is below 100% by
  design.
- A route mounted with a prefix built from an unresolved import keeps that
  prefix as a `{}` segment.

# Global `repo://` URI schema

Stable identifiers for graph entities across repositories (SYN-11). Phase-1
`Node.ID` values (`file:…`, `func:…#…`, …) remain the primary store keys;
`repo://` is the canonical public identifier stored as a unique secondary index
and on each node as `props.repo_uri`.

## Grammar

```
repo://{repo}/{path}#{kind}:{symbol}
```

| Component | Rules |
|-----------|--------|
| `repo` | Normalized, URL-safe simple name (`[A-Za-z0-9._-]+`). Chosen via `--repo` / config; defaults to the basename of the index root. Later workspace mode may map names to paths (SYN-12). |
| `path` | Repo-relative, slash-normalized (`/`), percent-encoded where needed. No leading `/` in the canonical string after `repo://{repo}/`. |
| fragment | `{kind}` for files, otherwise `{kind}:{symbol}`. Fragment content is percent-encoded where needed. |
| query | **Not allowed** in SYN-11. |

### Kind tokens

| Node kind (`Node.Kind`) | URI token |
|-------------------------|-----------|
| `file` | `file` |
| `package` | `package` |
| `module` | `module` |
| `function` | `func` |
| `method` | `method` |
| `type` | `type` |
| `import` | `import` |
| `symbol` | `symbol` |

### Examples

| Kind | URI |
|------|-----|
| file | `repo://synapse/internal/parse/builder.go#file` |
| package | `repo://synapse/internal/parse/builder.go#package:parse` |
| function | `repo://synapse/internal/parse/builder.go#func:newBuilder` |
| method | `repo://synapse/internal/parse/builder.go#method:Builder.Put` |
| type | `repo://synapse/internal/parse/builder.go#type:Builder` |
| import | `repo://synapse/internal/parse/builder.go#import:github.com/taricsa/synapse/internal/graph` |
| symbol (file-scoped) | `repo://synapse/internal/parse/builder.go#symbol:Name` |

Unresolved call targets without an owning file keep the Phase-1 id `symbol:Name`
and **do not** receive a `repo://` URI until they can be scoped.

## Conflict rules

- Same `{repo, path, kind, symbol}` denotes the **same** entity.
- Duplicate repo names in a multi-repo workspace are **invalid** unless explicitly aliased (SYN-12).
- The same relative path in different repos is fine: `{repo}` disambiguates.
- Within one store, the URI secondary index is unique: two different Phase-1 node IDs must not share a URI.

## Persistence and migration

- Primary key: Phase-1 `Node.ID` (unchanged).
- Canonical URI: `props["repo_uri"]` plus Badger/memory index key `ru\x00{uri}` → `Node.ID`.
- Schema version `2` stores require URIs (where applicable). Opening an older index runs an upgrade: parse Phase-1 IDs, assign URIs using `--repo` or the index-root basename, write the secondary index, bump schema version. Fingerprints and ownership are preserved.
- MCP tools accept legacy Phase-1 IDs **or** `repo://` URIs and return nodes with canonical `repo_uri` set.

## CLI

```bash
./synapse index . --data-dir .synapse --repo synapse
./synapse mcp --data-dir .synapse --root . --repo synapse
```

If `--repo` is omitted, Synapse uses `filepath.Base` of the absolute index/query root.

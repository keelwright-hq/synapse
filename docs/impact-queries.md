# Impact queries: affected, dependencies, path

Three queries answer "what uses this", "what does this use" and "how does A
reach B". They walk `calls`, `triggers`, `references`, `inherits` and `implements` edges,
and group results by file.

```bash
synapse query affected Store --root .            # what uses Store
synapse query dependencies Indexer --root .      # what Indexer uses
synapse query path Run resolveCalls --root .     # how Run reaches resolveCalls
```

The MCP tools `affected`, `dependencies` and `shortest_path` return the same
results ([mcp.md](mcp.md)). In a workspace, the walk crosses repos
([workspace.md](workspace.md), [http-linking.md](http-linking.md)).

## What a seed stands for

| Seed | Covers |
|------|--------|
| A function or method | Itself, plus the functions and closures defined inside it |
| A type | Itself and its members, including members declared in Swift extensions in other files |
| A file (`file:path` or a basename) | Everything the file declares |

A name that matches several declarations is rejected with the `repo://` IDs to
choose from. Pass one of them as the seed.

## Nested functions and closures

A function defined inside another belongs to the function that defines it
(Python, JavaScript, TypeScript, Swift).

```python
def make_node():
    def node(state):
        return run(state)
    return node
```

- `dependencies make_node` lists `run`: what the nested function calls is part
  of what `make_node` does.
- `affected run` lists both `node` and `make_node`.
- `path build run` steps from `make_node` into `node` with a `contains` step,
  so a chain through a returned closure is found.

A local helper that the outer function calls is still listed by name in
`dependencies`.

## Swift calls through typed values

A call through a value goes to the method of the type the value is declared
with:

```swift
static func run(store: (any StoreProtocol)? = nil) {
    let cache = Cache()
    store?.save(1)     // StoreProtocol.save
    cache.warm()       // Cache.warm
}
```

- **Where the type comes from:** a parameter's type, a local's annotation or
  its initializer (`let cache = Cache()`), or a stored property of the enclosing
  type (`store.save()` and `self.store.save()`).
- **What is seen through:** optionals, `any`, `some` and parentheses, so
  `(any StoreProtocol)?` is `StoreProtocol`.
- **Protocol types link to the requirement.** A protocol's method
  declarations are method nodes marked `requirement=true`. A call through a
  protocol-typed value links to the requirement, even when several types
  implement it. If exactly one implementation of that name exists, the call
  links to it as well.
- **When nothing is linked:** the name has two different declared types in the
  same function, the type is an array, dictionary, closure or tuple, or the
  type does not declare exactly one method of that name.

Because requirements are nodes, a bare name that a protocol and its
implementations share is ambiguous as a seed. Use the `repo://` ID from the
error.

## Events (Inngest)

Code that sends an event is linked to the functions that run on it by a
`triggers` edge, so `path` from a route handler finds the background function
it starts, and `affected` on that function lists its senders (JavaScript,
TypeScript).

```ts
// route.ts
export async function POST(req: Request) {
  await inngest.send({ name: "campaign/publish-post", data });   // send
}

// approve.ts
await insertOutboxEvent({ eventName: "campaign/publish-post", payload, tx }); // outbox

// functions.ts
export const publishPostFn = inngest.createFunction(
  { id: "publish-post", triggers: [{ event: "campaign/publish-post" }] },
  async ({ step }) => { … },
);
```

```text
path func:src/app/api/approve/route.ts#POST -> func:src/inngest/functions.ts#publishPostFn (2 hops, edge type per step)
  start      POST src/app/api/approve/route.ts:4
  calls      decideApproval src/lib/approve.ts:3
  triggers   publishPostFn src/inngest/functions.ts:3
```

- **Senders:** `<client>.send(payload)` where the receiver's name contains
  `inngest` (`inngest.send`, `this.inngestClient.send`); `step.sendEvent(id,
  payload)`; and any call whose name mentions `outbox`
  (`insertOutboxEvent(...)`, `outbox.insert(...)`) with an `eventName` property.
  The edge starts at the function the call is in.
- **Payloads:** an object, an array of objects, or objects built by a callback
  (`ids.map((id) => ({ name: "x/y", data }))`). Only top-level `name` keys count,
  so `data: { name }` is not an event.
- **Receivers:** `createFunction({ triggers: [{ event }] }, handler)`,
  `createFunction({ trigger: { event } }, handler)` and the v2 form
  `createFunction(config, { event }, handler)`. Cron triggers have no sender.
- **Names must be static:** a string literal, a template without substitutions
  or a same-file string constant, with `as const` looked through. A name built
  at runtime (`` `campaign/${kind}` ``) or passed in from elsewhere
  (`inngest.send(decision.events)`) is not linked; the code that builds the
  event with a static name is, when it is in a function.
- A function that sends its own trigger event (a requeue) has no edge to
  itself. Edge props: `event` (the event names), `via` (`send`, `step`,
  `outbox`), provenance `INFERRED`.

## Output

```text
seed=func:shop/pricing.py#round_money files=4 symbols=4 truncated=false
  shop/pricing.py: line_total [calls] L17
  shop/receipt.py: format_receipt [calls] L6
  shop/tax.py: tax_for [calls] L8
  tests/test_pricing.py: test_round_money [calls] L6
```

- One line per file. Every symbol is listed with the edge it was reached by
  and its line; nothing is cut to "+N more".
- `--limit` caps the number of files (default 100); `truncated=true` means the
  cap was hit.
- `--depth` follows more hops (default 1). A file reached through an import at
  depth 2 or more counts only its imports and top-level code.
- `--json` gives the same data with node IDs and depths.

`path` prints one step per line with the edge type. Hops count every edge on
the chain, including `contains` steps from a type into a method or from a
function into a nested function, so "3 hops" is not always three calls.

## Limits

- **No general type inference.** Outside Swift's declared types, a call through
  a variable links only when the name is unique or the qualifier names the
  type. Chained receivers (`a.b.c()`) are not typed.
- **Callers through a protocol are not folded into an implementation.**
  `affected DiskStore.save` does not list a caller that only reaches
  `StoreProtocol.save`. Query the requirement for those.
- **Nested functions with the same name in one file share a node,** so two
  outer functions that each define `inner` both contain it.
- **Events link by name within one repo.** A sender in one workspace member
  does not reach a function in another.
- **Type edges** (`references`, `inherits`) exist for Swift, JavaScript,
  TypeScript and Python only.

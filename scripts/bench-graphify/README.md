# bench-graphify

Scripted token-cost benchmark comparing three ways an agent can answer code
questions: plain `grep`/`git`/file reads, Graphify, and Synapse. Results and
methodology are in [docs/benchmarks-graphify.md](../../docs/benchmarks-graphify.md).

## Requirements

- Python 3, standard library only
- `graphify` (from `graphifyy` on PyPI) on `PATH`, with a built `graphify-out/graph.json`
- A `synapse` binary and an index of the target repo (`synapse --repo NAME --data-dir DIR index REPO`)

## Run

```bash
python3 scripts/bench-graphify/run.py \
  --repo /path/to/repo --repo-name NAME \
  --graph /path/to/repo/graphify-out/graph.json \
  --synapse /path/to/synapse --data-dir DIR \
  [--tasks tasks.json] [--out results.json] [--raw-dir /tmp/raw]
```

It prints a table and writes `results.json`. `--raw-dir` saves each approach's
full output for inspection. Those files can contain source code, so keep them
out of the repo for private code.

## Writing tasks

`tasks.json` targets aeroflux at `3c04fb4`. For another repo, write your own
file in the same shape:

```json
{
  "repo": "myrepo",
  "repo_commit": "abc1234",
  "tasks": [
    {
      "id": "callers",
      "question": "What calls Store.save?",
      "expected": [["handleSubmit"], ["SyncWorker.flush", "flush"]],
      "approaches": {
        "grep":     ["grep -rn -C 30 '\\.save(' --include='*.go' ."],
        "graphify": ["graphify affected 'save' --depth 1 --graph {graph}"],
        "synapse":  ["{syn} query neighborhood save --root {root}"]
      }
    }
  ]
}
```

- **`expected`** lists the items a correct answer must contain. Each item is a
  list of aliases, and any one of them appearing in the output counts.
- **Commands** run with the target repo as the working directory.
- **Placeholders:** `{graph}` is the Graphify graph path, `{syn}` is the Synapse
  binary with its `--repo`/`--data-dir` flags, and `{root}` is the repo path.
- **Failed attempts:** keep the commands an agent would really try, including
  ones that fail with an ambiguity error. Their output is part of the cost.

Token counts are estimates (characters ÷ 4) over each command's text plus its
full output.

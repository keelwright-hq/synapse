# Benchmark harness

This is the harness behind the Synapse benchmark write-ups: grep and file reads,
Graphify and Synapse answering the same scripted code questions, scored against
hand-written answer keys. It is published so you can rerun the protocol on
your own repos, or on the small synthetic fixture here.

| File | What it is |
|---|---|
| `run.py` | The runner. Python 3 standard library only. |
| `tasks.example.json` | 9 synthetic tasks against the fixture, each with an answer key |
| `fixture/` | A tiny Python shop: products, a cart, discount rules, tax, receipts, a test and a doc |
| `make_fixture.sh` | Copies `fixture/` to a new folder and replays a fixed git history, so co-change questions work |
| `synapse.example.yaml` | Example workspace file for the cross-service round, with placeholder names |

## Run it on the fixture

Prerequisites: `git`, `bash`, Python 3, Graphify (`uv tool install graphifyy`, the
write-ups used 0.9.45) and a `synapse` binary on `PATH`.

```bash
cd bench/harness
./make_fixture.sh /tmp/bench/shop

(cd /tmp/bench/shop && graphify . --code-only)
synapse --repo shop --data-dir /tmp/bench/syn index /tmp/bench/shop

python3 run.py --repo /tmp/bench/shop --repo-name shop \
  --graph /tmp/bench/shop/graphify-out/graph.json \
  --synapse "$(command -v synapse)" --data-dir /tmp/bench/syn \
  --tasks tasks.example.json --out /tmp/bench/results.json --raw-dir /tmp/bench/raw
```

`make_fixture.sh` uses a fixed author and fixed dates, so the fixture's HEAD is
`fbb0c2f` on every machine. The fixture's `.gitignore` covers `graphify-out/`,
so Synapse does not index Graphify's output.

`run.py` prints a table of estimated tokens and items found per task and
approach, and writes the details to `--out`. `--raw-dir` keeps each approach's
full output, which can contain source code; `results.json` never does.

To check that a rerun is identical, run it twice and compare:

```bash
python3 run.py ... --out /tmp/bench/results-2.json
diff /tmp/bench/results.json /tmp/bench/results-2.json && echo identical
```

## Run it on your own repos

Write a `tasks.json` in the same shape as `tasks.example.json`, index the repo
with both tools, and pass the repo and task file to `run.py`.

- **Working directory:** each command runs with the target repo as its working directory.
- **Placeholders:** `{graph}` is the Graphify `graph.json`, `{syn}` is the Synapse
  binary with its `--repo` (or `--workspace`) and `--data-dir` flags, and `{root}` is the repo path.
- **Answer keys:** `expected` is a list of items. Each item is a list of aliases,
  and the item counts as found when any alias appears in the approach's output.

For the cross-service round, list the repos in a workspace file (start from
`synapse.example.yaml`), index it, and pass `--workspace`:

```bash
synapse index --workspace synapse.yaml --data-dir /tmp/bench/ws
python3 run.py --repo /path/to/one-member --workspace synapse.yaml \
  --graph /path/to/merged-graph.json --synapse "$(command -v synapse)" \
  --data-dir /tmp/bench/ws --tasks tasks-multi.json --out results-multi.json
```

With `--workspace`, `{syn}` runs Synapse over the whole workspace instead of one
repo. The `http` block tells Synapse which env vars reach each member; see
[docs/http-linking.md](../../docs/http-linking.md). No cross-service tasks ship here,
because the fixture is a single repo.

## Protocol

- **Hand-written answer keys.** Every task has a key written by hand from the
  source and git history at a fixed commit.
- **Complete means every item.** An answer is complete only when every expected
  item is found. Extra items in the output are not penalized.
- **Scripted command sequences.** Each approach runs a fixed list of commands,
  the ones an agent would run. No live agent chooses them.
- **Failed attempts are kept.** When a lookup fails or comes back ambiguous, the
  next command uses what the tool suggested, and the failed call is still paid for.
- **Token estimate.** Tokens = characters / 4, over each command's text and its
  full stdout and stderr. No tokenizer is used and no LLM is called.
- **Reruns are byte-identical.** With the same repo commit and tool versions,
  `results.json` is the same on every run.
- **Fallback cost.** When a graph tool's answer is incomplete, "tokens to answer"
  adds the grep approach's cost, because an agent would still have to finish
  the job with grep.

## Limits

- **One author.** The same person wrote the questions, the answer keys and the
  Synapse fixes. An independent task set would be a better test.
- **grep is a best case.** The grep commands were written knowing the answers:
  the right file, the right line range, the right `git log` pipeline.
- **The published numbers cannot be reproduced from this folder.** The original
  runs used one private Swift app and nine private repos. Their task files hold
  real names, so they are not published. This folder reproduces the protocol, not
  those results.
- **No held-out run yet.** The Synapse fixes were written after seeing the
  questions. A run on questions written after the fixes is still to do.
- **grep's callers cost in the Swift run is inflated.** That run's grep callers
  command used `grep -C 30`, which prints 30 lines around every match, so grep's
  token count on that question is higher than a tighter command would need.
  The fixture's callers task uses `-B 5`.
- **Matching is by name.** An item counts as found if any alias appears anywhere
  in the output, including the command text. A tool that mentions a file for an
  unrelated reason still gets credit, and a command that spells out an expected
  name credits itself. Keep expected names out of commands where you can.
- **Small fixture.** The synthetic tasks show how the harness works. Results on
  a 12-file repo say little about results on a real one.

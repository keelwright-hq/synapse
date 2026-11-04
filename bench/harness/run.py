#!/usr/bin/env python3
"""Token-cost benchmark: grep/read vs Graphify vs Synapse on scripted code questions.

Each task in tasks.json lists the fixed command sequence an agent would run for
each approach. The harness runs every command in the target repo, captures
stdout+stderr, and estimates the tokens an agent would read: the command text
plus its output, at 4 characters per token. No LLM is called, so runs are
deterministic for a given repo commit and tool versions.

An approach "answers" a task when every expected item (any alias) appears in
its combined output. When it does not, the grep/read approach's cost is added
as the fallback an agent would need to finish the job.

Usage:
  run.py --repo PATH --graph PATH/graphify-out/graph.json \
         --synapse PATH/synapse --data-dir PATH [--tasks tasks.json] \
         [--out results.json] [--raw-dir DIR]

Raw command outputs may contain source code; they are written only to
--raw-dir (if given), never to results.json.
"""
import argparse
import json
import math
import os
import shlex
import subprocess
import sys

CHARS_PER_TOKEN = 4
APPROACHES = ("grep", "graphify", "synapse")


def est_tokens(text):
    return math.ceil(len(text) / CHARS_PER_TOKEN)


def run_step(cmd, cwd):
    p = subprocess.run(["bash", "-c", cmd], cwd=cwd, capture_output=True, text=True, errors="replace")
    return p.stdout + p.stderr, p.returncode


def score(expected, text):
    hit = [any(a in text for a in aliases) for aliases in expected]
    return sum(hit), len(hit), [aliases[0] for aliases, ok in zip(expected, hit) if not ok]


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--repo", required=True)
    ap.add_argument("--graph", required=True)
    ap.add_argument("--synapse", required=True)
    ap.add_argument("--data-dir", required=True)
    ap.add_argument("--repo-name", default=None, help="synapse --repo name (default: tasks.json repo)")
    ap.add_argument("--tasks", default=os.path.join(os.path.dirname(os.path.abspath(__file__)), "tasks.json"))
    ap.add_argument("--out", default="results.json")
    ap.add_argument("--raw-dir", default=None)
    ap.add_argument("--workspace", default=None, help="synapse.yaml: run Synapse federated over a workspace instead of one repo")
    args = ap.parse_args()

    spec = json.load(open(args.tasks))
    repo = os.path.abspath(args.repo)
    name = args.repo_name or spec["repo"]
    scope = ["--workspace", os.path.abspath(args.workspace)] if args.workspace else ["--repo", name]
    syn = " ".join(shlex.quote(x) for x in [os.path.abspath(args.synapse), *scope, "--data-dir", os.path.abspath(args.data_dir)])
    subs = {"{graph}": shlex.quote(os.path.abspath(args.graph)), "{syn}": syn, "{root}": shlex.quote(repo)}
    if args.raw_dir:
        os.makedirs(args.raw_dir, exist_ok=True)

    results = []
    for task in spec["tasks"]:
        row = {"id": task["id"], "question": task["question"], "approaches": {}}
        for appr in APPROACHES:
            text, chars, steps = "", 0, []
            for tmpl in task["approaches"][appr]:
                cmd = tmpl
                for k, v in subs.items():
                    cmd = cmd.replace(k, v)
                out, rc = run_step(cmd, repo)
                # The agent reads the command it wrote (as the template, not
                # machine-specific paths) and the full output.
                read = tmpl + "\n" + out
                text += read
                chars += len(read)
                steps.append({"command": tmpl, "exit": rc, "output_bytes": len(out.encode()), "output_lines": out.count("\n")})
            found, total, missing = score(task["expected"], text)
            row["approaches"][appr] = {
                "steps": steps,
                "tool_calls": len(steps),
                "chars_read": chars,
                "est_tokens": math.ceil(chars / CHARS_PER_TOKEN),
                "found": found,
                "expected": total,
                "missing": missing,
                "complete": found == total,
            }
            if args.raw_dir:
                with open(os.path.join(args.raw_dir, f"{task['id']}.{appr}.txt"), "w") as f:
                    f.write(text)
        base = row["approaches"]["grep"]
        for appr in APPROACHES:
            a = row["approaches"][appr]
            a["fallback_to_grep"] = not a["complete"] and appr != "grep"
            a["est_tokens_to_answer"] = a["est_tokens"] + (base["est_tokens"] if a["fallback_to_grep"] else 0)
            a["tool_calls_to_answer"] = a["tool_calls"] + (base["tool_calls"] if a["fallback_to_grep"] else 0)
        results.append(row)

    totals = {}
    for appr in APPROACHES:
        rows = [r["approaches"][appr] for r in results]
        totals[appr] = {
            "est_tokens": sum(a["est_tokens"] for a in rows),
            "est_tokens_to_answer": sum(a["est_tokens_to_answer"] for a in rows),
            "tool_calls_to_answer": sum(a["tool_calls_to_answer"] for a in rows),
            "tasks_complete": sum(1 for a in rows if a["complete"]),
            "items_found": sum(a["found"] for a in rows),
            "items_expected": sum(a["expected"] for a in rows),
        }

    out = {
        "repo": spec["repo"],
        "repo_commit": spec["repo_commit"],
        "token_estimate": f"ceil(characters / {CHARS_PER_TOKEN}); command text + full stdout/stderr",
        "tasks": results,
        "totals": totals,
    }
    with open(args.out, "w") as f:
        json.dump(out, f, indent=1)
        f.write("\n")

    w = sys.stdout.write
    w(f"{'task':<13}" + "".join(f"{a + ' tok':>14}{'found':>8}" for a in APPROACHES) + "\n")
    for r in results:
        w(f"{r['id']:<13}")
        for appr in APPROACHES:
            a = r["approaches"][appr]
            mark = "" if a["complete"] else "*"
            w(f"{a['est_tokens']:>14,}{str(a['found']) + '/' + str(a['expected']) + mark:>8}")
        w("\n")
    w(f"{'total':<13}" + "".join(f"{totals[a]['est_tokens']:>14,}{str(totals[a]['tasks_complete']) + '/' + str(len(results)):>8}" for a in APPROACHES) + "\n")
    w(f"{'to answer':<13}" + "".join(f"{totals[a]['est_tokens_to_answer']:>14,}{'':>8}" for a in APPROACHES) + "\n")
    w("* incomplete answer; 'to answer' adds the grep/read cost as fallback\n")


if __name__ == "__main__":
    main()

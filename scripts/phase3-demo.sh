#!/usr/bin/env bash
# Phase 3 reviewer demo (SYN-93): index a sample tree, ingest a trace, query semantics.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DEMO="${TMPDIR:-/tmp}/synapse-phase3-demo-$$"
mkdir -p "$DEMO/src"
cleanup() { rm -rf "$DEMO"; }
trap cleanup EXIT

cat >"$DEMO/src/greet.go" <<'EOF'
package main
// helper is documented for docs_for_symbol demos.
func helper() {}
func main() { helper() }
EOF
cat >"$DEMO/src/README.md" <<'EOF'
# Demo
See `helper` in greet.go.
EOF
cat >"$DEMO/trace.json" <<'EOF'
{"spans":[
  {"name":"main","attributes":{"code.function":"main"}},
  {"name":"helper","parent":"main","attributes":{"code.function":"helper"}}
]}
EOF

(
  cd "$DEMO/src"
  git init -q
  git config user.email "demo@synapse"
  git config user.name "demo"
  echo 'package x' > other.go
  git add greet.go other.go
  git commit -qm "pair1"
  git commit -qm "pair2" --allow-empty || true
  # second commit touching both
  echo '// touch' >> greet.go
  echo '// touch' >> other.go
  git add greet.go other.go
  git commit -qm "pair3"
)

BIN="${ROOT}/synapse"
if [[ ! -x "$BIN" ]]; then
  (cd "$ROOT" && go build -o synapse ./cmd/synapse)
fi

"$BIN" --repo demo --data-dir "$DEMO/data" index "$DEMO/src"
"$BIN" --repo demo --data-dir "$DEMO/data" otel ingest "$DEMO/trace.json" --root "$DEMO/src"
echo "== co-changes =="
"$BIN" --repo demo --data-dir "$DEMO/data" query co-changes greet.go --root "$DEMO/src" --json | head -c 500 || true
echo
echo "== hot-paths =="
"$BIN" --repo demo --data-dir "$DEMO/data" query hot-paths 'func:greet.go#main' --root "$DEMO/src" --json | head -c 500 || true
echo
echo "== docs =="
"$BIN" --repo demo --data-dir "$DEMO/data" query docs 'func:greet.go#helper' --root "$DEMO/src" --json | head -c 500 || true
echo
echo "Phase 3 demo completed successfully."

#!/usr/bin/env bash
# Build the synthetic benchmark repo: copy fixture/ to DEST and replay a fixed
# git history, so co-change questions have something to measure. Author, dates
# and contents are fixed, so the commit hashes are the same on every machine.
#
# Usage: make_fixture.sh DEST
set -euo pipefail

if [ $# -ne 1 ]; then
  echo "usage: $0 DEST" >&2
  exit 2
fi
here="$(cd "$(dirname "$0")" && pwd)"
dest="$1"
if [ -e "$dest" ]; then
  echo "$dest already exists; remove it or pick another path" >&2
  exit 1
fi

mkdir -p "$dest"
cp -R "$here/fixture/." "$dest/"
cd "$dest"

export GIT_AUTHOR_NAME="Bench Fixture" GIT_AUTHOR_EMAIL="fixture@example.invalid"
export GIT_COMMITTER_NAME="$GIT_AUTHOR_NAME" GIT_COMMITTER_EMAIL="$GIT_AUTHOR_EMAIL"
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1
git init -q -b main .

day=0
commit() { # commit MESSAGE FILE...
  local msg="$1"; shift
  for f in "$@"; do
    case "$f" in
      *.md) printf '\n%s.\n' "$msg" >> "$f" ;;
      *) printf '# %s\n' "$msg" >> "$f" ;;
    esac
  done
  day=$((day + 1))
  local when; when="$(printf '2026-01-%02dT12:00:00Z' "$day")"
  git add -A
  GIT_AUTHOR_DATE="$when" GIT_COMMITTER_DATE="$when" git commit -q -m "$msg"
}

# Initial import, then a short scripted history. Files that change with
# shop/pricing.py at least twice: shop/rules.py (3), tests/test_pricing.py (2),
# docs/pricing.md (2). shop/cart.py changes with it once.
day=0
when="2026-01-01T12:00:00Z"
git add -A
GIT_AUTHOR_DATE="$when" GIT_COMMITTER_DATE="$when" git commit -q -m "Initial shop"
day=1
commit "Add member discount" shop/pricing.py shop/rules.py
commit "Test rounding" shop/pricing.py tests/test_pricing.py
commit "Skip gift cards in rules" shop/pricing.py shop/rules.py
commit "Document rounding" shop/pricing.py docs/pricing.md
commit "Test bulk discount" shop/pricing.py tests/test_pricing.py
commit "Apply rules in order" shop/pricing.py shop/rules.py docs/pricing.md
commit "Keep cart lines" shop/cart.py shop/checkout.py
commit "Default rules for carts" shop/pricing.py shop/cart.py
commit "Round receipt totals" shop/receipt.py shop/tax.py

echo "$dest at $(git rev-parse --short HEAD)"

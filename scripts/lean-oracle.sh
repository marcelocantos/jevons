#!/usr/bin/env bash
# Thin Lean oracle for 🎯T863 — lake build + sorry inventory.
set -euo pipefail
ROOT="$(CDPATH= cd "$(dirname "$0")/.." && pwd)"
LEAN_DIR="$ROOT/lean"
LIB_GLOB="$LEAN_DIR/Invariants"

usage() {
  echo "usage: scripts/lean-oracle.sh {build|sorries|check}" >&2
  exit 2
}

cmd="${1:-}"
case "$cmd" in
  build)
    cd "$LEAN_DIR"
    lake build
    ;;
  sorries)
    count=0
    while IFS= read -r line; do
      printf '%s\n' "$line"
      count=$((count + 1))
    done < <(grep -Rn '\bsorry\b' "$LIB_GLOB" 2>/dev/null || true)
    echo "SORRY_COUNT=$count"
    ;;
  check)
    cd "$LEAN_DIR"
    lake build
    count=$( (grep -R '\bsorry\b' "$LIB_GLOB" 2>/dev/null || true) | wc -l | tr -d ' ')
    if [ "$count" != "0" ]; then
      echo "lean-oracle: FAIL $count sorry in $LIB_GLOB" >&2
      grep -Rn '\bsorry\b' "$LIB_GLOB" >&2 || true
      exit 1
    fi
    echo "lean-oracle: OK build green, sorry_count=0"
    ;;
  *)
    usage
    ;;
esac

#!/bin/sh
# make gate / make gate-full (🎯T1279, 🎯T1299): run the product net on a
# commit in a clean checkout and, only on a GREEN verdict, attest that commit's
# tree for the pre-push hook (scripts/hooks/pre-push).
#
#   gate-attest.sh fast   make test-fast — every layer and journey except the
#                         live-fleet journeys (profile jevons-fast-v1)
#   gate-attest.sh full   make test — everything (profile jevons-full-v1)
#
# bin/gate -clean measures the committed tree, never the shared clone's dirty
# working copy, so the attestation always names a tree with no uncommitted
# changes. bin/gate exits non-zero for every verdict but GREEN (RED, SUSPECT,
# EMPTY, …); the GATE line is checked as well, so a zero exit with any other
# verdict still attests nothing.
#
#   GATE_COMMIT    commit to measure (default HEAD)
#   GATE_PROVIDER  journey provider (default openai-codex)
set -eu
cd "$(git rev-parse --show-toplevel)"

case "${1:-}" in
    fast) target=test-fast; profile=jevons-fast-v1 ;;
    full) target=test; profile=jevons-full-v1 ;;
    *) echo "usage: gate-attest.sh fast|full" >&2; exit 2 ;;
esac

commit=$(git rev-parse --verify "${GATE_COMMIT:-HEAD}^{commit}")
provider=${GATE_PROVIDER:-openai-codex}
att="$(git rev-parse --git-dir)/gate-attestation"
out=$(mktemp)
trap 'rm -f "$out"' EXIT

status=0
bin/gate -clean -commit "$commit" -name "owner-gate-$1" -- \
    make "$target" PROVIDER="$provider" 2>&1 | tee "$out" || true
# tee hides bin/gate's status; read the verdict line it printed instead.
line=$(grep -E '^GATE ' "$out" | tail -n 1 || true)
case "$line" in
    *" exit=0 GREEN "*) ;;
    *) status=1 ;;
esac
if [ "$status" != 0 ]; then
    echo "make gate: not GREEN — no attestation written (${line:-no GATE line})" >&2
    exit 1
fi

id=$(printf '%s\n' "$line" | sed -n 's/.* id=\([^ ]*\).*/\1/p')
tmp="$att.tmp.$$"
{
    echo "tree=$(git rev-parse "$commit^{tree}")"
    echo "commit=$commit"
    echo "dirty=0"
    echo "at=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    echo "profile=$profile"
    echo "gate=$id"
} > "$tmp"
mv "$tmp" "$att"
echo "✓ gate ($1) attested tree of $(git rev-parse --short "$commit") (gate $id)"

#!/usr/bin/env bash
# 🎯T63 journey: jevonsd restart under an open mission against the host
# claudia daemon, using a jevonsd built from a clean tree that depends on
# the published claudia module (no local replace).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

if grep -q 'replace github.com/marcelocantos/claudia' go.mod; then
	echo "t63-daemon-reclaim: go.mod still has a local claudia replace" >&2
	exit 1
fi

SOCK="${CLAUDIA_BROKER_SOCKET:-$HOME/.local/state/claudia/broker.sock}"
if [[ ! -S "$SOCK" ]]; then
	echo "t63-daemon-reclaim: no daemon at $SOCK (brew services start claudia)" >&2
	exit 1
fi
export CLAUDIA_BROKER_SOCKET="$SOCK"
unset CLAUDIA_NO_BROKER || true

if ! /opt/homebrew/bin/claudia broker status >/dev/null 2>&1 && ! claudia broker status >/dev/null 2>&1; then
	echo "t63-daemon-reclaim: claudia broker status failed" >&2
	exit 1
fi

WT="$(mktemp -d /tmp/jevons-t63.XXXXXX)"
cleanup() { git -C "$ROOT" worktree remove --force "$WT" >/dev/null 2>&1 || rm -rf "$WT"; }
trap cleanup EXIT
git worktree add --detach "$WT" HEAD
cd "$WT"

export GOWORK=off
go test -count=1 -timeout 15m -run TestT63DaemonReclaimJourney ./cmd/jevonsd

#!/bin/sh
# Start daily jevonsd for supervisord (vellum run-view.sh shape).
# Repo-root bin/jevonsd only — never Homebrew Cellar (🎯T553.3 Cellar reclaim).
set -e

# ROOT is the workdir this daemon serves from; the binary is chosen below
# and is not necessarily from this tree.
ROOT="$(CDPATH= cd "$(dirname "$0")/.." && pwd)"

if [ -z "${HOME:-}" ]; then
  HOME="$(eval echo ~"$(id -un)")"
  export HOME
fi
export USER="${USER:-$(id -un)}"
export PATH="/opt/homebrew/bin:/opt/homebrew/sbin:/usr/local/bin:${HOME}/.cargo/bin:${HOME}/.local/bin:${HOME}/.py/bin:${HOME}/go/bin:/usr/bin:/bin:/usr/sbin:/sbin"

# Which jevonsd does supervisor run (🎯T594)?
#
#   1. JEVONS_BIN        explicit pin, for a one-off bisect or rollback.
#   2. JEVONS_DEV_REPO   this machine develops jevons, so its build wins.
#   3. the Homebrew install — the default on every other machine.
#
# A dev machine that has not built is a HARD failure, never a quiet fall
# back to the release: the whole point of the override is that the repo is
# the truth here, and silently serving an older Cellar build instead is the
# stale-binary failure 🎯T552 / 🎯T553 exist to catch. Better a
# supervisor that says why it will not start than a daemon serving code
# nobody is looking at.
if [ -n "${JEVONS_BIN:-}" ]; then
  BIN="$JEVONS_BIN"
  if [ ! -x "$BIN" ]; then
    echo "jevonsd: JEVONS_BIN=$BIN is not executable" >&2
    exit 1
  fi
elif [ -n "${JEVONS_DEV_REPO:-}" ]; then
  BIN="$JEVONS_DEV_REPO/bin/jevonsd"
  if [ ! -x "$BIN" ]; then
    echo "jevonsd: dev repo $JEVONS_DEV_REPO is configured but $BIN is missing." >&2
    echo "jevonsd: build it (make jevonsd). Refusing to serve the Homebrew release" >&2
    echo "jevonsd: from a machine whose repo is meant to be the truth." >&2
    exit 1
  fi
else
  BIN="$(command -v jevonsd 2>/dev/null || true)"
  if [ -z "$BIN" ] && command -v brew >/dev/null 2>&1; then
    BIN="$(brew --prefix)/bin/jevonsd"
  fi
  if [ -z "$BIN" ] || [ ! -x "$BIN" ]; then
    echo "jevonsd: no jevonsd on PATH and no Homebrew install found." >&2
    echo "jevonsd: brew install marcelocantos/tap/jevons, or set JEVONS_DEV_REPO." >&2
    exit 1
  fi
fi
# --print-bin resolves and stops. Choosing the binary is the interesting
# behaviour here and it must be testable without starting a daemon: a test
# that ran the winner to find out which one it picked would bind :13705 the
# moment a release accepts the flags it is handed.
if [ "${1:-}" = "--print-bin" ]; then
  echo "$BIN"
  exit 0
fi
echo "jevonsd: running $BIN" >&2
exec "$BIN" -port 13705 -vanilla-port 0 -workdir "$ROOT"

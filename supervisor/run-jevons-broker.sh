#!/bin/sh
# Start development jevons-broker for supervisord (🎯T866.7).
# Repo-root bin/jevons-broker only. A jevonsd bounce must not kill the
# Oh My Pi sidecar this process Ensure'd.
set -e

ROOT="$(CDPATH= cd "$(dirname "$0")/.." && pwd)"

if [ -z "${HOME:-}" ]; then
  HOME="$(eval echo ~"$(id -un)")"
  export HOME
fi
export USER="${USER:-$(id -un)}"
export PATH="/opt/homebrew/bin:/opt/homebrew/sbin:/usr/local/bin:${HOME}/.cargo/bin:${HOME}/.local/bin:${HOME}/.py/bin:${HOME}/go/bin:/usr/bin:/bin:/usr/sbin:/sbin"

if [ -n "${JEVONS_BROKER_BIN:-}" ]; then
  BIN="$JEVONS_BROKER_BIN"
elif [ -n "${JEVONS_DEV_REPO:-}" ]; then
  BIN="$JEVONS_DEV_REPO/bin/jevons-broker"
else
  BIN="$ROOT/bin/jevons-broker"
fi
if [ ! -x "$BIN" ]; then
  echo "jevons-broker: $BIN is missing. Build it (make jevons-broker)." >&2
  exit 1
fi
if [ "${1:-}" = "--print-bin" ]; then
  echo "$BIN"
  exit 0
fi
echo "jevons-broker: running $BIN serve" >&2
exec "$BIN" serve

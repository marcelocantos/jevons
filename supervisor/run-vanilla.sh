#!/bin/sh
# Start the UI-only vanilla reference on :13706 for supervisord.
# Proxies API/WS to daily :13705; does not open a second ~/.jevons (🎯T540.4).
set -e

ROOT="$(CDPATH= cd "$(dirname "$0")/.." && pwd)"

if [ -z "${HOME:-}" ]; then
  HOME="$(eval echo ~"$(id -un)")"
  export HOME
fi
export USER="${USER:-$(id -un)}"
export PATH="/opt/homebrew/bin:/opt/homebrew/sbin:/usr/local/bin:${HOME}/.cargo/bin:${HOME}/.local/bin:${HOME}/.py/bin:${HOME}/go/bin:/usr/bin:/bin:/usr/sbin:/sbin"

BIN="$ROOT/bin/jevonsd"
if [ ! -x "$BIN" ]; then
  echo "jevons-vanilla: missing $BIN — build with make jevonsd" >&2
  exit 1
fi
exec "$BIN" -ui vanilla -port 13706 -upstream 127.0.0.1:13705

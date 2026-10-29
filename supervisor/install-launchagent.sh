#!/bin/sh
# 🎯T779: install homebrew.mxcl.supervisor as an Interactive LaunchAgent so
# supervisord and its children are not scheduled below fleet work.
set -e

REPO="$(CDPATH= cd "$(dirname "$0")/.." && pwd)"
LABEL="homebrew.mxcl.supervisor"
TEMPLATE="$REPO/supervisor/homebrew.mxcl.supervisor.plist"

if [ "$(uname -s)" != "Darwin" ]; then
  exit 0
fi
if [ "${SUPERVISOR_SKIP_LAUNCHAGENT:-}" = "1" ]; then
  exit 0
fi
if [ ! -f "$TEMPLATE" ]; then
  echo "supervisor/install-launchagent.sh: missing template $TEMPLATE" >&2
  exit 1
fi

if [ -z "${HOME:-}" ]; then
  HOME="$(eval echo ~"$(id -un)")"
  export HOME
fi

DEST="$HOME/Library/LaunchAgents/${LABEL}.plist"
mkdir -p "$HOME/Library/LaunchAgents"

CELLAR_PLIST=""
if command -v brew >/dev/null 2>&1; then
  prefix="$(brew --prefix supervisor 2>/dev/null || true)"
  if [ -n "$prefix" ] && [ -f "$prefix/homebrew.mxcl.supervisor.plist" ]; then
    CELLAR_PLIST="$prefix/homebrew.mxcl.supervisor.plist"
  fi
fi

rendered="$(mktemp "${TMPDIR:-/tmp}/jevons-supervisor-launchagent.XXXXXX")"
cp "$TEMPLATE" "$rendered"
if [ -n "$CELLAR_PLIST" ] && command -v plutil >/dev/null 2>&1; then
  args="$(plutil -extract ProgramArguments json -o - "$CELLAR_PLIST" 2>/dev/null || true)"
  if [ -n "$args" ]; then
    plutil -replace ProgramArguments -json "$args" "$rendered" 2>/dev/null || true
  fi
fi
if ! plutil -lint "$rendered" >/dev/null 2>&1; then
  echo "supervisor/install-launchagent.sh: rendered plist failed lint" >&2
  rm -f "$rendered"
  exit 1
fi
ptype="$(plutil -extract ProcessType raw -o - "$rendered" 2>/dev/null || true)"
if [ "$ptype" != "Interactive" ]; then
  echo "supervisor/install-launchagent.sh: ProcessType must be Interactive, got ${ptype:-<unset>}" >&2
  rm -f "$rendered"
  exit 1
fi

reload=0
if [ ! -f "$DEST" ]; then
  reload=1
elif ! cmp -s "$rendered" "$DEST"; then
  reload=1
fi
cp "$rendered" "$DEST"
rm -f "$rendered"
chmod 644 "$DEST"
echo "rendered $DEST (🎯T779 ProcessType=Interactive)"

if [ "$reload" = 0 ]; then
  echo "launchagent unchanged; not reloading $LABEL"
  exit 0
fi

if ! command -v launchctl >/dev/null 2>&1; then
  echo "launchctl not on PATH — plist written; load $LABEL manually" >&2
  exit 0
fi

domain="gui/$(id -u)"
launchctl bootout "$domain/$LABEL" 2>/dev/null || true
if launchctl bootstrap "$domain" "$DEST"; then
  echo "reloaded $domain/$LABEL"
else
  echo "supervisor/install-launchagent.sh: launchctl bootstrap failed; plist is at $DEST" >&2
  exit 1
fi

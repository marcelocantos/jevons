#!/bin/bash
# One unattended supervision pass (🎯T704 / 🎯T700 / 🎯T405).
#
# A headless `claude -p` cannot answer a permission prompt. Without the
# flags below the pass is blind: it cannot read ~/.jevons/supervisor/notes.md,
# cannot open a cited gate under ~/.jevons/gates, and cannot raise the owner
# via blurter — then ends "quiet" and looks like a healthy fleet. Observed
# 2026-09-20T20:27 (🎯T704).
#
# --add-dir $HOME/.jevons     notes, gates, state live outside the repo
# --permission-mode bypassPermissions
#                             no interactive approval; blurter/Bash work (🎯T700)
# --disallowedTools Edit Write NotebookEdit
#                             "work goes through the fleet" is harness-enforced
#
# stdin is /dev/null: supervisord's never-closing stdin otherwise makes
# claude -p wait three seconds every tick.
set -eu
export PATH="/opt/homebrew/bin:/opt/homebrew/sbin:${HOME}/.local/bin:/usr/bin:/bin:/usr/sbin:/sbin"

HERE="$(CDPATH= cd "$(dirname "$0")" && pwd)"
REPO="${JEVONS_TICK_REPO:-$(CDPATH= cd "$HERE/.." && pwd)}"
STATE="${JEVONS_SUPERVISOR_STATE:-$HOME/.jevons/supervisor}"
BRIEF="${JEVONS_TICK_BRIEF:-$STATE/brief.md}"
TIMEOUT_S="${JEVONS_TICK_TIMEOUT:-900}"

mkdir -p "$STATE"

if [ ! -f "$BRIEF" ]; then
  echo "run-tick: brief missing at $BRIEF" >&2
  exit 2
fi

# Prefer the tracked binary when present so a development checkout is the
# source of truth; fall through to PATH claude otherwise.
CLAUDE_BIN="${JEVONS_CLAUDE_BIN:-$(command -v claude || true)}"
if [ -z "$CLAUDE_BIN" ]; then
  echo "run-tick: claude not on PATH" >&2
  exit 127
fi

cd "$REPO"
# shellcheck disable=SC2086
exec timeout "$TIMEOUT_S" "$CLAUDE_BIN" -p "$(cat "$BRIEF")" \
  --add-dir "$HOME/.jevons" \
  --permission-mode bypassPermissions \
  --disallowedTools Edit Write NotebookEdit \
  </dev/null

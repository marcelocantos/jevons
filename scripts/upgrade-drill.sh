#!/usr/bin/env bash
# 🎯T40 upgrade-without-drain drill.
#
# Proves coordinator-side scaffolding (upgrade mode skips StopAll; handles
# snapshot by session_id) and documents the residual: process survival still
# requires claudia connect-mode. Until that lands, this drill must NOT claim
# full T40 achievement.
#
# Usage (from repo root):
#   ./scripts/upgrade-drill.sh           # hermetic unit + residual report
#   ./scripts/upgrade-drill.sh --live    # optional live daemon drill (needs Grok)
#
# Exit codes:
#   0  — scaffolding oracles green AND process survival proven (not yet possible)
#   1  — scaffolding test failure
#   2  — residual: process durability not proven (expected until connect-mode)

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

LIVE=0
if [[ "${1:-}" == "--live" ]]; then
  LIVE=1
fi

echo "=== 🎯T40 upgrade drill ==="
echo "repo: $ROOT"
echo

echo "--- 1. Hermetic: upgrade mode does not StopAll ---"
if ! go test ./internal/upgrade/ -count=1; then
  echo "FAIL: internal/upgrade tests"
  exit 1
fi
echo "PASS: ModeUpgrade.StopAgents()=false; handles round-trip; env/SIGHUP policy"
echo

echo "--- 2. Residual gate: process durability ---"
cat <<'EOF'
RESIDUAL (blocks full 🎯T40 achieve):
  Grok ACP agents are stdio children of jevonsd (claudia startGrokACP).
  Skipping registry.StopAll() on SIGHUP avoids an explicit Kill, but when
  the coordinator exits, OS closes the child's stdin/stdout pipes and the
  agent process dies. Conversation reattach (same session_id via
  agents.json + session/load) already works; *process* reattach does not.

  Required next: claudia connect-mode (external process + reattach by
  session_id/PID/socket), then re-run this drill and prove same PID.

Documented upgrade path (brew/launchd) — coordinator-only intent:
  # Prefer SIGHUP so jevonsd skips StopAll and writes ~/.jevons/upgrade-handles.json
  kill -HUP "$(pgrep -x jevonsd | head -1)"
  # Install new binary, then start:
  brew services start jevons
  # Or env override when only SIGTERM is available:
  #   JEVONS_UPGRADE_EXIT=1 brew services stop jevons
  # Note: until connect-mode, agents still exit when pipes close — this path
  # only avoids the deliberate StopAll SIGKILL path; drill still fails step 2.
EOF
echo

if [[ "$LIVE" -eq 1 ]]; then
  echo "--- 3. Live (optional): process survival check ---"
  if ! command -v jevonsd >/dev/null 2>&1 && [[ ! -x ./bin/jevonsd ]]; then
    echo "FAIL: no jevonsd binary for live drill"
    exit 1
  fi
  echo "Live process-survival check is not automated yet: spawn overseer,"
  echo "record PID, SIGHUP coordinator, assert same PID still alive."
  echo "Until connect-mode, expect PID gone after coordinator exit."
fi

echo
echo "DRILL RESULT: scaffolding green; process survival RESIDUAL"
echo "Do not achieve 🎯T40 until same-PID reattach is proven."
exit 2

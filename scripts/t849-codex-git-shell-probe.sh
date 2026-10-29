#!/usr/bin/env bash
# Copyright 2026 Marcelo Cantos
# SPDX-License-Identifier: Apache-2.0
#
# 🎯T849 live oracle: a codex work seat must create index state and a
# gate-clean-shaped worktree via its own shell. Run inside the seat workdir
# (including T1151.1 linked worktrees). Exit 0 prints PROBE_OK.
set -euo pipefail
cd "${1:-.}"
SCRATCH="$(mktemp -d)"
cleanup() {
  git worktree remove -f "$SCRATCH/tree" 2>/dev/null || true
  rm -rf "$SCRATCH"
}
trap cleanup EXIT
git worktree add --detach "$SCRATCH/tree" HEAD
MARKER="t849-probe-$$"
touch "$MARKER"
git add --intent-to-add "$MARKER"
rm -f "$MARKER"
echo PROBE_OK

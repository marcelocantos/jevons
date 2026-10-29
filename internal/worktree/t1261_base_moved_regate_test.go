// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package worktree_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/worktree"
)

// 🎯T1261: a ledger commit lands on the shared branch without the landing
// lock, so it can arrive while a ten-minute post-merge gate runs. That is not
// the worker tip moving (🎯T1256 refuses that); the landing recomputes from
// the new tip, gates that merge, and lands. On 2026-10-28 jevons-po's
// "achieve T1254" did this to jv-t783-phase-agree and the landing was refused.
func TestT1261SharedBranchMovingUnderTheGateRecomputesAndLands(t *testing.T) {
	base := sharedClone(t)
	workerCommit(t, base, "jv-t1260", "a.txt", "worker\n")
	writeFile(t, filepath.Join(base, "b.txt"), "trunk\n")
	git(t, base, "commit", "-q", "-am", "move trunk")
	// First gate run: a PO's ledger commit lands meanwhile. Later runs pass.
	gateScript := `set -eu
if [ ! -e "$1/.gate-ran" ]; then
  : > "$1/.gate-ran"
  git -C "$1" commit -q --allow-empty -m "bullseye: achieve T1 (concurrent PO)"
fi
echo "$((1 + $(cat "$1/.gate-runs" 2>/dev/null || echo 0)))" > "$1/.gate-runs"`
	res, err := worktree.Integrate(&worktree.IntegrateArgs{
		BaseWorkdir: base, AgentName: "jv-t1260", GateStoreRoot: t.TempDir(),
		PostMergeGateCommand: []string{"sh", "-c", gateScript, "gate", base},
	})
	if err != nil {
		t.Fatalf("a landing whose shared branch moved under the gate was refused: %v", err)
	}
	if res.Attempts < 2 || res.Merge == "" {
		t.Fatalf("expected a recomputed merge on a second attempt, got attempts=%d merge=%q", res.Attempts, res.Merge)
	}
	if runs := strings.TrimSpace(readFile(t, filepath.Join(base, ".gate-runs"))); runs != "2" {
		t.Fatalf("the recomputed merge was not gated again: gate ran %s time(s)", runs)
	}
	log := git(t, base, "log", "--format=%s", "master")
	if !strings.Contains(log, "concurrent PO") || git(t, base, "show", "master:a.txt") != "worker" {
		t.Fatalf("master lacks the concurrent commit or the worker change:\n%s", log)
	}
}

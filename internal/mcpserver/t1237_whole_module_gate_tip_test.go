// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"os/exec"
	"regexp"
	"testing"

	"github.com/marcelocantos/jevons/internal/worktree"
)

// The tip the sentinel hands the standing gate must be a SHA git resolves,
// not rev-parse's raw stdout (🎯T1237). The newline-bearing tip failed
// every tick on the development daemon from the day the gate landed.
func TestT1237WholeModuleGateTipIsABareResolvableSHA(t *testing.T) {
	repo := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "seed"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	tip := wholeModuleGateTip(repo)
	if !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(tip) {
		t.Fatalf("tip = %q, want a bare 40-hex SHA", tip)
	}
	if out, err := exec.Command("git", "-C", repo, "rev-parse", "--verify", tip+"^{commit}").CombinedOutput(); err != nil {
		t.Fatalf("git cannot resolve the tip the gate would be given: %v\n%s", err, out)
	}
	// The debounce sees the same bare tip on the next tick and settles.
	if settled, err := worktree.SettleWholeModuleGateTip(repo, tip); err != nil || settled {
		t.Fatalf("first sight: settled=%v err=%v, want candidate only", settled, err)
	}
	if settled, err := worktree.SettleWholeModuleGateTip(repo, wholeModuleGateTip(repo)); err != nil || !settled {
		t.Fatalf("second sight: settled=%v err=%v, want settled", settled, err)
	}
}

// standing_gate.enabled is off by default (owner pause, 2026-10-30): the
// sentinel's tick must not even record a candidate tip, so nothing settles
// and no `go test ./...` starts.
func TestT1237StandingGateIsOffUntilEnabled(t *testing.T) {
	repo := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "seed"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	s := &Server{}
	s.tickWholeModuleGate(repo)
	s.tickWholeModuleGate(repo)
	tip := wholeModuleGateTip(repo)
	if settled, err := worktree.SettleWholeModuleGateTip(repo, tip); err != nil || settled {
		t.Fatalf("a disabled standing gate recorded a candidate: settled=%v err=%v", settled, err)
	}
}

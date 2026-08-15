// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package worktreereap

import (
	"strings"
	"testing"
	"time"
)

// The policy is pure, so these cases are the specification: what each signal
// is worth, and which way every unknown falls.
func TestDecide(t *testing.T) {
	const (
		livePID  = 4711
		deadPID  = 4712
		liveSess = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
		deadSess = "11111111-2222-3333-4444-555555555555"
	)
	live := Liveness{Cmds: map[int]string{
		livePID: "go test ./scripts/docratchet",
		99:      "claude --session-id " + liveSess,
	}}
	pol := Policy{
		RepoRoot:     "/repo",
		Protected:    []string{"/home/.jevons/build-snapshot"},
		SessionGrace: 20 * time.Minute,
		IdleGrace:    60 * time.Minute,
	}

	detached := func(path string) Entry {
		return Entry{Path: path, Detached: true, Head: "abc123"}
	}

	cases := []struct {
		name   string
		obs    Observation
		want   Action
		reason string // substring the reason must carry
	}{{
		name: "the clone itself is never touched",
		obs:  Observation{Entry: Entry{Path: "/repo", Main: true, Branch: "refs/heads/master"}, Exists: true},
		want: ActionKeep, reason: "clone itself",
	}, {
		name: "a protected path survives a dead owner",
		obs: Observation{
			Entry: detached("/home/.jevons/build-snapshot"),
			Owner: Owner{Kind: OwnerMarker, PID: deadPID},
			Idle:  9 * time.Hour, Exists: true,
		},
		want: ActionKeep, reason: "protected",
	}, {
		name: "a missing directory is removed whatever its owner",
		obs:  Observation{Entry: detached("/tmp/gone"), Exists: false},
		want: ActionRemove, reason: "no longer exists",
	}, {
		name: "a live marker pid keeps its worktree however idle",
		obs: Observation{
			Entry: detached("/tmp/wt"),
			Owner: Owner{Kind: OwnerMarker, PID: livePID, Cmd: "go test ./scripts/docratchet"},
			Idle:  9 * time.Hour, Exists: true,
		},
		want: ActionKeep, reason: "is running",
	}, {
		// The point of recording the command line: pid 4711 exists, but it
		// is not the process that took the worktree out.
		name: "a recycled pid running something else is not the owner",
		obs: Observation{
			Entry: detached("/tmp/wt"),
			Owner: Owner{Kind: OwnerMarker, PID: livePID, Cmd: "go test ./internal/gate"},
			Idle:  time.Second, Exists: true,
		},
		want: ActionRemove, reason: "is gone",
	}, {
		name: "a dead marker pid is reaped with no waiting period",
		obs: Observation{
			Entry: detached("/tmp/wt"),
			Owner: Owner{Kind: OwnerMarker, PID: deadPID, Cmd: "go test ./scripts/docratchet"},
			Idle:  time.Second, Exists: true,
		},
		want: ActionRemove, reason: "is gone",
	}, {
		name: "modified tracked files hold a removal and say so",
		obs: Observation{
			Entry: detached("/tmp/wt"), Exists: true, DirtyTracked: true,
			Owner: Owner{Kind: OwnerMarker, PID: deadPID},
			Idle:  9 * time.Hour,
		},
		want: ActionHold, reason: "modified tracked files",
	}, {
		// The ratchet's corpse: t.TempDir()'s removal was killed partway
		// through, so every tracked file reads as changed and none of the
		// content is anywhere but HEAD. Held on that evidence it pins its
		// commit forever, which is the leak 🎯T440 exists to end.
		name: "a tree whose tracked files are all merely missing is not work",
		obs: Observation{
			Entry: detached("/tmp/wt"), Exists: true, DirtyTracked: true, Hollow: true,
			Owner: Owner{Kind: OwnerMarker, PID: deadPID},
			Idle:  9 * time.Hour,
		},
		want: ActionRemove, reason: "every tracked file in it is simply missing",
	}, {
		// One real edit among the deletions and the guard is back: the
		// hollow case is an exception for a corpse, not a way around the
		// guard for any tree with a lot of deletions in it.
		name: "one real edit among the deletions still holds the tree",
		obs: Observation{
			Entry: detached("/tmp/wt"), Exists: true, DirtyTracked: true, Hollow: false,
			Owner: Owner{Kind: OwnerMarker, PID: deadPID},
			Idle:  9 * time.Hour,
		},
		want: ActionHold, reason: "modified tracked files",
	}, {
		name: "a live session keeps its worktree",
		obs: Observation{
			Entry: detached("/tmp/" + liveSess + "/scratchpad/wt"),
			Owner: Owner{Kind: OwnerSession, Session: liveSess},
			Idle:  9 * time.Hour, Exists: true,
		},
		want: ActionKeep, reason: "is live",
	}, {
		// A session with no live process may be a parked worker: the butler
		// stops idle agent processes on purpose and rehydrates them later.
		name: "a dead session inside the grace period is held, not removed",
		obs: Observation{
			Entry: detached("/tmp/" + deadSess + "/scratchpad/wt"),
			Owner: Owner{Kind: OwnerSession, Session: deadSess},
			Idle:  5 * time.Minute, Exists: true,
		},
		want: ActionHold, reason: "rehydrates",
	}, {
		name: "a dead session past the grace period is removed",
		obs: Observation{
			Entry: detached("/tmp/" + deadSess + "/scratchpad/wt"),
			Owner: Owner{Kind: OwnerSession, Session: deadSess},
			Idle:  90 * time.Minute, Exists: true,
		},
		want: ActionRemove, reason: "is gone and the tree has been idle",
	}, {
		name: "an unidentified worktree waits out the longer idle grace",
		obs: Observation{
			Entry: detached("/tmp/mystery"), Exists: true,
			Idle: 30 * time.Minute,
		},
		want: ActionHold, reason: "no owner could be identified",
	}, {
		name: "an unidentified worktree idle past that grace is removed",
		obs: Observation{
			Entry: detached("/tmp/mystery"), Exists: true,
			Idle: 3 * time.Hour,
		},
		want: ActionRemove, reason: "no owner could be identified",
	}, {
		name: "unknown idle time is never enough to remove",
		obs: Observation{
			Entry: detached("/tmp/mystery"), Exists: true, Idle: -1,
		},
		want: ActionHold, reason: "idle time is unknown",
	}, {
		name: "a branch checkout is somebody's work, not a verification tree",
		obs: Observation{
			Entry:  Entry{Path: "/tmp/feature", Branch: "refs/heads/feature"},
			Exists: true, Idle: 9 * time.Hour,
		},
		want: ActionHold, reason: "not a detached verification worktree",
	}, {
		name: "a locked worktree is held",
		obs: Observation{
			Entry:  Entry{Path: "/tmp/wt", Detached: true, Locked: true},
			Exists: true, Idle: 9 * time.Hour,
		},
		want: ActionHold, reason: "locked",
	}}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Decide(c.obs, live, pol)
			if got.Action != c.want {
				t.Fatalf("action = %s, want %s (reason: %s)", got.Action, c.want, got.Reason)
			}
			if !strings.Contains(got.Reason, c.reason) {
				t.Fatalf("reason %q does not mention %q", got.Reason, c.reason)
			}
		})
	}
}

// An unconfigured Policy must be the conservative one. The failure mode of the
// other convention — zero meaning "no waiting period" — is deleting a live
// worker's tree, which is the one outcome this package must never produce.
func TestZeroPolicyIsConservative(t *testing.T) {
	pol := Policy{}.WithDefaults()
	if pol.SessionGrace != DefaultSessionGrace || pol.IdleGrace != DefaultIdleGrace {
		t.Fatalf("zero policy = %+v, want the defaults", pol)
	}
	obs := Observation{
		Entry:  Entry{Path: "/tmp/wt", Detached: true},
		Owner:  Owner{Kind: OwnerSession, Session: "11111111-2222-3333-4444-555555555555"},
		Exists: true,
		Idle:   10 * time.Minute,
	}
	if d := Decide(obs, Liveness{}, Policy{}); d.Action != ActionHold {
		t.Fatalf("an unconfigured policy removed a tree idle 10m: %s (%s)", d.Action, d.Reason)
	}
}

func TestSessionFromPath(t *testing.T) {
	cases := map[string]string{
		"/private/tmp/claude-501/-Users-x-jevons/0e27f442-96ff-40f0-bb79-886009f183a0/scratchpad/t426wt": "0e27f442-96ff-40f0-bb79-886009f183a0",
		"/var/folders/fn/T/TestT360CleanCheckoutBuilds763361753/001/head":                                "",
		"/Users/marcelo/work/github.com/marcelocantos/jevons":                                            "",
		// The last uuid component wins: a session's scratchpad nested under
		// another session's directory belongs to the inner one.
		"/tmp/11111111-2222-3333-4444-555555555555/x/AAAAAAAA-BBBB-CCCC-DDDD-EEEEEEEEEEEE/wt": "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
	}
	for path, want := range cases {
		if got := SessionFromPath(path); got != want {
			t.Errorf("SessionFromPath(%q) = %q, want %q", path, got, want)
		}
	}
}

// classifyStatus is where the hollow-tree exception is decided, so it is worth
// its own table: everything downstream of it trusts the two booleans.
func TestClassifyStatus(t *testing.T) {
	cases := []struct {
		name        string
		out         string
		dirty, holl bool
	}{
		{"a clean tree", "", false, false},
		{"trailing newline only", "\n", false, false},
		{"a half-deleted corpse", " D Makefile\n D docs/x.md\n D internal/a/b.go\n", true, true},
		{"one edit among the deletions", " D Makefile\n M web/index.html\n D docs/x.md\n", true, false},
		{"a staged deletion is a decision, not a corpse", "D  Makefile\n", true, false},
		{"a rename", "R  old.go -> new.go\n", true, false},
		{"a conflict", "UU internal/a/b.go\n", true, false},
		{"a staged add whose file then vanished", "AD scratch.go\n", true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dirty, holl := classifyStatus(c.out)
			if dirty != c.dirty || holl != c.holl {
				t.Fatalf("classifyStatus(%q) = (%v, %v), want (%v, %v)", c.out, dirty, holl, c.dirty, c.holl)
			}
		})
	}
}

func TestPIDAlive(t *testing.T) {
	live := Liveness{Cmds: map[int]string{10: "go test ./scripts/docratchet -run TestT398"}}
	cases := []struct {
		pid  int
		cmd  string
		want bool
	}{
		{10, "go test ./scripts/docratchet", true}, // prefix match: args may grow
		{10, "go test ./internal/gate", false},     // recycled pid, other command
		{10, "", true},                             // no recorded command: existence alone
		{11, "go test ./scripts/docratchet", false},
		{0, "", false},
		{-1, "", false},
	}
	for _, c := range cases {
		if got := live.PIDAlive(c.pid, c.cmd); got != c.want {
			t.Errorf("PIDAlive(%d, %q) = %v, want %v", c.pid, c.cmd, got, c.want)
		}
	}
}

func TestSessionAlive(t *testing.T) {
	const sess = "0e27f442-96ff-40f0-bb79-886009f183a0"
	live := Liveness{Cmds: map[int]string{
		7: "/bin/zsh -c source /Users/x/.claude/shell-snapshots/snap.sh && cd /tmp/" + strings.ToUpper(sess) + "/scratchpad",
	}}
	if !live.SessionAlive(sess) {
		t.Error("a session named in a live command line reported dead")
	}
	if live.SessionAlive("11111111-2222-3333-4444-555555555555") {
		t.Error("a session named nowhere reported live")
	}
	if live.SessionAlive("") {
		t.Error("the empty session reported live")
	}
}

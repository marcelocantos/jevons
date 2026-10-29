// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
)

// t956Harness registers a parent plus n claude-CLI seats under it, wires a
// fake sender for every name so deliverByName (and the round-close notice)
// resolves without a live process, and returns the env plus seat names.
func t956Harness(t *testing.T, n int) (*Server, *fakeSender, []string) {
	t.Helper()
	dir := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	work := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	const parentName = "jevons-po"
	if err := reg.Register(claudia.AgentDef{
		Name: parentName, WorkDir: work, SessionID: "s-po",
		Purpose: claudia.PurposeWork, Provider: claudia.ProviderClaude,
	}); err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, n)
	senders := map[string]*fakeSender{}
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("jv-t956-seat-%d", i)
		if err := reg.Register(claudia.AgentDef{
			Name: name, WorkDir: work, SessionID: fmt.Sprintf("s-%d", i),
			Purpose: claudia.PurposeWork, Parent: parentName, Provider: claudia.ProviderClaude,
		}); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
		senders[name] = &fakeSender{alive: true}
	}
	parent := &fakeSender{alive: true}
	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	s.SetSendQueueDir(dir)
	setObservedSenderResolver(s, func(resolve string) (agentSender, bool, error) {
		if resolve == parentName {
			return parent, false, nil
		}
		if sender, ok := senders[resolve]; ok {
			return sender, false, nil
		}
		return nil, false, fmt.Errorf("unknown %s", resolve)
	})
	return s, parent, names
}

// waitForBirthSettled blocks until s's birth ledger has recorded (and
// attempted to persist) an accepted-prompt entry for parentName, or the
// deadline passes.
//
// closeMintRound's notify runs on a time.AfterFunc goroutine, independent of
// the test's own goroutine. deliverByName appends to the fake sender's sent
// slice partway through its body, then — via a deferred call that still
// runs before that goroutine's call returns — observeBirthAcceptance writes
// an entry to the birth ledger and persists it to disk (🎯T679.2). Polling
// only parent.sentSnapshot() observes the append but races the deferred
// persist: the test can see the notice, finish, and let t.TempDir() remove
// the state directory while the SAME goroutine is still recreating files in
// it (persistLocked does MkdirAll + WriteFile), which fails RemoveAll with
// "directory not empty" (the temp-dir cleanup error this guards against).
// The birth ledger's own map entry is written immediately before the file
// write, under the same lock, so observing it here is a true signal that
// the trailing persist attempt has already run — not a blind sleep.
func waitForBirthSettled(t *testing.T, s *Server, parentName string, timeout time.Duration) {
	t.Helper()
	l := s.births()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		l.mu.Lock()
		found := false
		for _, rec := range l.births {
			if rec.Name == parentName {
				found = true
				break
			}
		}
		l.mu.Unlock()
		if found {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("birth ledger never recorded an accepted prompt for %s within %s", parentName, timeout)
}

// TestT956MintRoundLossNotifiesParentOnce is acceptance 3: when every seat
// of a mint round is released unbriefed, the parent gets one message naming
// the round, the seats, and the stop reasons.
func TestT956MintRoundLossNotifiesParentOnce(t *testing.T) {
	// 300ms used to stand in for the round's quiet period, but each
	// releaseUnbriefedSeat call below does real synchronous work (registry
	// Stop, a RemovalAccount file write, noteSeatFailure) between the three
	// seats, and under fleet-load host contention that work can itself take
	// longer than 300ms — closing the round between the first and second
	// seat and failing on a notice that only names one of three (observed
	// under -count=50 against the host's own ambient fleet load). 1s gives
	// the loop several times the headroom a few synchronous calls need even
	// under contention, while staying well below the 10s production
	// default this environment variable overrides for test time, and far
	// enough below it that a -count=100 run still finishes promptly.
	t.Setenv(MintRoundQuietEnv, "1s")
	s, parent, names := t956Harness(t, 3)

	for i, name := range names {
		// existed=false: these are freshly-minted rows this round's starts
		// created, not seats that predate the call (🎯T518 distinction).
		released := s.releaseUnbriefedSeat(name, false,
			fmt.Errorf("opening brief proven undelivered (seat %d)", i))
		if !released {
			t.Fatalf("expected %s to be released", name)
		}
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && len(parent.sentSnapshot()) == 0 {
		time.Sleep(10 * time.Millisecond)
	}

	if len(parent.sentSnapshot()) != 1 {
		t.Fatalf("want exactly one parent notice for the whole round, got %d: %v", len(parent.sentSnapshot()), parent.sentSnapshot())
	}
	notice := parent.sentSnapshot()[0]
	for _, name := range names {
		if !strings.Contains(notice, name) {
			t.Fatalf("notice must name every lost seat; missing %s in %q", name, notice)
		}
	}
	if !strings.Contains(notice, "opening brief proven undelivered") {
		t.Fatalf("notice must name the stop reason: %q", notice)
	}
	if !strings.Contains(notice, "mint round lost every seat") {
		t.Fatalf("notice must name the round as a total loss: %q", notice)
	}
	// The notice landing in sentSnapshot does not mean the firing goroutine
	// is done: wait for its trailing birth-ledger persist too, or this
	// test's own t.TempDir() cleanup can race it (see waitForBirthSettled).
	waitForBirthSettled(t, s, "jevons-po", 2*time.Second)
}

// TestT956MintRoundSurvivorSuppressesNotice proves the other direction: a
// round with even one confirmed brief is not reported as a total loss, even
// though other seats in the same round were released unbriefed.
func TestT956MintRoundSurvivorSuppressesNotice(t *testing.T) {
	t.Setenv(MintRoundQuietEnv, "1s")
	s, parent, names := t956Harness(t, 2)

	s.noteMintFailed("jevons-po", names[0], "opening brief proven undelivered")
	// The survivor's confirmed brief clears the round before it closes.
	s.noteMintSucceeded("jevons-po")

	// noteMintSucceeded stops the timer outright, so this margin only needs
	// to outlast any residual fire already in flight; it does not need to
	// scale with the quiet window above.
	time.Sleep(200 * time.Millisecond)

	if len(parent.sentSnapshot()) != 0 {
		t.Fatalf("a round with a survivor must not be reported as a total loss, got %v", parent.sentSnapshot())
	}
}

// TestT956MintRoundGrowingRoundExtendsDebounce proves a round that keeps
// losing seats keeps extending the close rather than firing (and missing
// the rest of the round) on the first loss alone.
func TestT956MintRoundGrowingRoundExtendsDebounce(t *testing.T) {
	// 400ms (with 200ms margins either side) left almost no headroom: under
	// fleet-load host contention a plain time.Sleep can run well past its
	// requested duration, and a sleep that overruns by more than the gap to
	// the window boundary turns "assert not fired yet" into a false
	// failure on a round that legitimately closed late rather than closed
	// early. 1.5s with 400ms margins gives a ratio ordinary scheduling
	// delay on a loaded host should not erode, while keeping a -count=100
	// run's total wall time practical.
	t.Setenv(MintRoundQuietEnv, "1500ms")
	s, parent, _ := t956Harness(t, 0)

	s.noteMintFailed("jevons-po", "jv-t956-seat-a", "opening brief proven undelivered")
	time.Sleep(400 * time.Millisecond) // inside the quiet window
	s.noteMintFailed("jevons-po", "jv-t956-seat-b", "opening brief proven undelivered")

	// At 400ms from the SECOND loss, nothing should have fired yet even
	// though 800ms have passed since the first (less than the 1.5s window
	// the first loss alone would have closed on).
	time.Sleep(400 * time.Millisecond)
	if len(parent.sentSnapshot()) != 0 {
		t.Fatalf("second loss must have extended the debounce; fired early: %v", parent.sentSnapshot())
	}

	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) && len(parent.sentSnapshot()) == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if len(parent.sentSnapshot()) != 1 {
		t.Fatalf("want one notice naming both losses, got %d: %v", len(parent.sentSnapshot()), parent.sentSnapshot())
	}
	if !strings.Contains(parent.sentSnapshot()[0], "jv-t956-seat-a") || !strings.Contains(parent.sentSnapshot()[0], "jv-t956-seat-b") {
		t.Fatalf("notice must name both seats from the extended round: %q", parent.sentSnapshot()[0])
	}
	waitForBirthSettled(t, s, "jevons-po", 2*time.Second)
}

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

// TestT956MintRoundLossNotifiesParentOnce is acceptance 3: when every seat
// of a mint round is released unbriefed, the parent gets one message naming
// the round, the seats, and the stop reasons.
func TestT956MintRoundLossNotifiesParentOnce(t *testing.T) {
	t.Setenv(MintRoundQuietEnv, "300ms")
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

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && len(parent.sent) == 0 {
		time.Sleep(10 * time.Millisecond)
	}

	if len(parent.sent) != 1 {
		t.Fatalf("want exactly one parent notice for the whole round, got %d: %v", len(parent.sent), parent.sent)
	}
	notice := parent.sent[0]
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
}

// TestT956MintRoundSurvivorSuppressesNotice proves the other direction: a
// round with even one confirmed brief is not reported as a total loss, even
// though other seats in the same round were released unbriefed.
func TestT956MintRoundSurvivorSuppressesNotice(t *testing.T) {
	t.Setenv(MintRoundQuietEnv, "300ms")
	s, parent, names := t956Harness(t, 2)

	s.noteMintFailed("jevons-po", names[0], "opening brief proven undelivered")
	// The survivor's confirmed brief clears the round before it closes.
	s.noteMintSucceeded("jevons-po")

	time.Sleep(200 * time.Millisecond)

	if len(parent.sent) != 0 {
		t.Fatalf("a round with a survivor must not be reported as a total loss, got %v", parent.sent)
	}
}

// TestT956MintRoundGrowingRoundExtendsDebounce proves a round that keeps
// losing seats keeps extending the close rather than firing (and missing
// the rest of the round) on the first loss alone.
func TestT956MintRoundGrowingRoundExtendsDebounce(t *testing.T) {
	t.Setenv(MintRoundQuietEnv, "400ms")
	s, parent, _ := t956Harness(t, 0)

	s.noteMintFailed("jevons-po", "jv-t956-seat-a", "opening brief proven undelivered")
	time.Sleep(200 * time.Millisecond) // inside the quiet window
	s.noteMintFailed("jevons-po", "jv-t956-seat-b", "opening brief proven undelivered")

	// At 200ms from the SECOND loss, nothing should have fired yet even
	// though 200ms have passed since the first (300ms > the original 400ms
	// window would have closed on the first loss alone).
	time.Sleep(200 * time.Millisecond)
	if len(parent.sent) != 0 {
		t.Fatalf("second loss must have extended the debounce; fired early: %v", parent.sent)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && len(parent.sent) == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if len(parent.sent) != 1 {
		t.Fatalf("want one notice naming both losses, got %d: %v", len(parent.sent), parent.sent)
	}
	if !strings.Contains(parent.sent[0], "jv-t956-seat-a") || !strings.Contains(parent.sent[0], "jv-t956-seat-b") {
		t.Fatalf("notice must name both seats from the extended round: %q", parent.sent[0])
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/converge"
)

// fakeOverseerArm is a test double for DeliverToOverseerAs: records each
// (text, origin) the sink would push through the real overseer arm.
type fakeOverseerArm struct {
	texts   []string
	origins []string
}

func (f *fakeOverseerArm) deliver(text, origin string) error {
	f.texts = append(f.texts, text)
	f.origins = append(f.origins, origin)
	return nil
}

// sinkOverArm builds the production sink shape over a fake arm without a
// full Server — same deliver(text, origin) contract as DeliverToOverseerAs.
func sinkOverArm(arm *fakeOverseerArm) *PostmortemSink {
	return &PostmortemSink{deliver: arm.deliver}
}

// 🎯T319: each closed incident is delivered exactly once through the
// overseer arm. Replay of the same episode must not re-fire the arm.
func TestPostmortemSinkExactlyOncePerIncident(t *testing.T) {
	arm := &fakeOverseerArm{}
	sink := sinkOverArm(arm)
	j := converge.NewPostmortemJournal()

	opened := time.Date(2026, 8, 8, 4, 0, 0, 0, time.UTC)
	inc := converge.Incident{
		Agent:   "jv-t319",
		Mission: "T319",
		Opened:  opened,
		Closed:  opened.Add(46 * time.Minute),
		Dwell:   46 * time.Minute,
		Rungs:   []converge.Rung{converge.RungHumanAlert},
		Cause:   converge.ClosedBySatisfaction,
	}

	if _, err := j.Flush([]converge.Incident{inc}, sink); err != nil {
		t.Fatalf("first flush: %v", err)
	}
	if len(arm.texts) != 1 {
		t.Fatalf("want exactly one arm delivery, got %d: %v", len(arm.texts), arm.texts)
	}
	if arm.origins[0] != sendOriginAgent {
		t.Fatalf("origin=%q, want agent (root/system, not owner bubble)", arm.origins[0])
	}
	if !strings.Contains(arm.texts[0], "jv-t319") {
		t.Fatalf("delivery missing agent:\n%s", arm.texts[0])
	}

	// Replayed close of the same episode must not re-deliver.
	if _, err := j.Flush([]converge.Incident{inc}, sink); err != nil {
		t.Fatalf("replay flush: %v", err)
	}
	if len(arm.texts) != 1 {
		t.Fatalf("incident redelivered through arm: %d deliveries", len(arm.texts))
	}

	// Direct DeliverPostmortem is still one arm call per invocation (the
	// journal owns once-per-incident; the sink is a thin pass-through).
	if err := sink.DeliverPostmortem("second report"); err != nil {
		t.Fatalf("direct: %v", err)
	}
	if len(arm.texts) != 2 {
		t.Fatalf("direct deliver did not reach arm: %d", len(arm.texts))
	}
}

// NewPostmortemSink wires the real Server arm; a nil server leaves the
// seam honest (nil sink).
func TestNewPostmortemSinkBindsDeliverToOverseerAs(t *testing.T) {
	if NewPostmortemSink(nil) != nil {
		t.Fatal("nil server must yield nil sink")
	}

	s := overseerFamilyServer(t)
	var delivered []string
	s.notifySender = func(text string) error {
		delivered = append(delivered, text)
		return nil
	}

	sink := NewPostmortemSink(s)
	if sink == nil {
		t.Fatal("want non-nil sink")
	}
	const body = "**Impatience incident closed — jv-x**"
	if err := sink.DeliverPostmortem(body); err != nil {
		t.Fatalf("DeliverPostmortem: %v", err)
	}
	if len(delivered) != 1 {
		t.Fatalf("want one notify, got %d", len(delivered))
	}
	// Agent origin: unmarked, no userTurnPrefix (root report, not owner speech).
	if strings.HasPrefix(delivered[0], userTurnPrefix) {
		t.Fatalf("postmortem must not carry owner marker: %q", delivered[0])
	}
	if delivered[0] != body {
		t.Fatalf("delivered=%q, want body passthrough", delivered[0])
	}
}

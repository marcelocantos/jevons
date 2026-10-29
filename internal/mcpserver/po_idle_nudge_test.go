// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/ownerquestionview"
	"github.com/marcelocantos/jevons/internal/poidlenudge"
)

// t1266Ledger carries three frontier leaves: T66.2 (open, unblocked, no
// worker — the stalled-target OR-branch), T66.3 (bound to a stopped worker
// holding a queued message — the stopped-worker OR-branch), and T66.4
// (parked behind T66.1's open owner question — must never fire).
const t1266Ledger = `
schema_version: 1
project: pigeon
targets:
  T66.1:
    name: decide approach for T66 family
    status: identified
    acceptance: ["owner decides"]
  T66.2:
    name: open unblocked, nobody engaged
    status: identified
    acceptance: ["land"]
  T66.3:
    name: worker stopped holding a queued message
    status: converging
    acceptance: ["land"]
  T66.4:
    name: waits on T66.1's owner question
    status: identified
    acceptance: ["land"]
    depends_on: ["T66.1"]
`

func t1266Fixture(t *testing.T) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	state := filepath.Join(dir, "state")
	if err := os.MkdirAll(state, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bullseye.yaml"), []byte(t1266Ledger), 0o644); err != nil {
		t.Fatal(err)
	}
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	s.SetAgentReportDir(state)
	s.idleActivity = NewIdleActivityTracker()
	if err := s.OpenRoleAssignments(dir); err != nil {
		t.Fatal(err)
	}
	for _, d := range []claudia.AgentDef{
		{Name: "pigeon-po", WorkDir: dir, SessionID: "po-1", Parent: "jevons", Purpose: purposeProductOwner},
		{Name: "jv-t66.3-auto", WorkDir: dir, SessionID: "w-663", Parent: "pigeon-po", Purpose: claudia.PurposeWork, TargetID: "T66.3"},
	} {
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}
	setPhase(s, "pigeon-po", "idle", time.Time{})
	// Establish the registry's SessionID baseline before any send-queue
	// observation (🎯T766.2): the seatstate remint guard treats a queue
	// observation preceding the first registry-fed SessionID sighting as a
	// new incarnation and discards queue depth. Production always runs
	// ObserveRegistry continuously from boot; a hermetic fixture must seed
	// it once itself.
	s.observeRegistryLiveness()

	// T66.1's open owner question — T66.4 depends on it, but the suppression
	// this test cares about is the question itself never firing a nudge/fault
	// for the target it is recorded against.
	st := ownerquestionview.New(state)
	if err := st.Record(ownerquestionview.Question{
		Identity:    ownerquestionview.Identity{Repo: dir, Target: "T66.1", ID: "approach", Version: "1"},
		Text:        "which approach for T66?",
		Asker:       "pigeon-po",
		AnswerRoute: "chat",
		Default:     "wait",
	}); err != nil {
		t.Fatal(err)
	}
	return s, dir
}

// TestT1266TickPOIdleNudgeIntegration drives the real production call site
// (tickPOIdleNudge) end to end against a real registry + ledger +
// owner-question-store fixture — not just the pure poidlenudge classifier.
// It proves: (1) an open unblocked target with nobody engaged and (2) a
// stopped worker holding a 1h-queued message both produce exactly one
// nudge, then (3) one owner-visible fault once the symptom persists past
// the fault grace, while (4) T66.1 (open owner question) and T66.4 (depends
// on it) are never named.
func TestT1266TickPOIdleNudgeIntegration(t *testing.T) {
	s, dir := t1266Fixture(t)

	var mu sync.Mutex
	var nudges []string
	s.notifyJevon = func(text string) {
		mu.Lock()
		defer mu.Unlock()
		nudges = append(nudges, text)
	}
	var faults []string
	s.SetOwnerNotifier(NewOwnerNotifier(func(subject, kind, text string) bool {
		mu.Lock()
		defer mu.Unlock()
		faults = append(faults, text)
		return true
	}))

	// jv-t66.3-auto is registered but not alive, with a message queued 1h
	// ago — the stopped-worker queue-age OR-branch.
	queuedAt := time.Now().Add(-time.Hour)
	if _, _, err := s.sendQueue().Append("jv-t66.3-auto", "please resume", queuedAt); err != nil {
		t.Fatal(err)
	}

	// PO is alive and idle; its stopped worker jv-t66.3-auto is not.
	running := func(name string) bool { return name == "pigeon-po" }

	now := time.Now().UTC()

	// Cycle 1: symptom freshly observed → exactly one nudge, no fault yet.
	s.tickPOIdleNudge("jevons", dir, now, poidlenudge.DefaultQueueThreshold, poidlenudge.DefaultFaultGrace, false, running)

	mu.Lock()
	gotNudges, gotFaults := len(nudges), len(faults)
	lastNudge := ""
	if gotNudges > 0 {
		lastNudge = nudges[gotNudges-1]
	}
	mu.Unlock()
	if gotNudges != 1 {
		t.Fatalf("cycle 1: nudges=%d, want 1 (%v)", gotNudges, nudges)
	}
	if gotFaults != 0 {
		t.Fatalf("cycle 1: faults=%d, want 0", gotFaults)
	}
	if !strings.Contains(lastNudge, "T66.2") {
		t.Fatalf("nudge missing stalled target T66.2:\n%s", lastNudge)
	}
	if !strings.Contains(lastNudge, "jv-t66.3-auto") {
		t.Fatalf("nudge missing stopped worker jv-t66.3-auto:\n%s", lastNudge)
	}
	if strings.Contains(lastNudge, "T66.1") || strings.Contains(lastNudge, "T66.4") {
		t.Fatalf("nudge must never name T66.1/T66.4 (open owner question): %s", lastNudge)
	}

	// Cycle 2: immediately after — nudged, not yet past fault grace. No
	// second nudge, no fault.
	s.tickPOIdleNudge("jevons", dir, now.Add(time.Minute), poidlenudge.DefaultQueueThreshold, poidlenudge.DefaultFaultGrace, false, running)
	mu.Lock()
	gotNudges, gotFaults = len(nudges), len(faults)
	mu.Unlock()
	if gotNudges != 1 {
		t.Fatalf("cycle 2: nudges=%d, want still 1", gotNudges)
	}
	if gotFaults != 0 {
		t.Fatalf("cycle 2: faults=%d, want 0", gotFaults)
	}

	// Cycle 3: well past the fault grace since the nudge, symptom persists
	// → exactly one owner-visible fault.
	s.tickPOIdleNudge("jevons", dir, now.Add(2*time.Hour), poidlenudge.DefaultQueueThreshold, poidlenudge.DefaultFaultGrace, false, running)
	mu.Lock()
	gotNudges, gotFaults = len(nudges), len(faults)
	lastFault := ""
	if gotFaults > 0 {
		lastFault = faults[gotFaults-1]
	}
	mu.Unlock()
	if gotNudges != 1 {
		t.Fatalf("cycle 3: nudges=%d, want still 1 (no second nudge)", gotNudges)
	}
	if gotFaults != 1 {
		t.Fatalf("cycle 3: faults=%d, want exactly 1", gotFaults)
	}
	if !strings.Contains(lastFault, "pigeon-po") {
		t.Fatalf("fault must name the PO: %s", lastFault)
	}
	if !strings.Contains(lastFault, "T66.2") || !strings.Contains(lastFault, "jv-t66.3-auto") {
		t.Fatalf("fault must name the stalled target and stopped worker: %s", lastFault)
	}
}

// TestT1266TickPOIdleNudgeOwnerQuestionOnlySuppressesNeither verifies a PO
// whose only open child waits on an owner question (or a dependency on one)
// gets neither a nudge nor a fault, driven through the real tick wiring.
func TestT1266TickPOIdleNudgeOwnerQuestionOnlySuppressesNeither(t *testing.T) {
	dir := t.TempDir()
	state := filepath.Join(dir, "state")
	if err := os.MkdirAll(state, 0o755); err != nil {
		t.Fatal(err)
	}
	ledger := `
schema_version: 1
project: claudia
targets:
  T1:
    name: decide approach
    status: identified
    acceptance: ["owner decides"]
  T2:
    name: depends on the open question
    status: identified
    acceptance: ["land"]
    depends_on: ["T1"]
`
	if err := os.WriteFile(filepath.Join(dir, "bullseye.yaml"), []byte(ledger), 0o644); err != nil {
		t.Fatal(err)
	}
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	s.SetAgentReportDir(state)
	s.idleActivity = NewIdleActivityTracker()
	if err := s.OpenRoleAssignments(dir); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "claudia-po", WorkDir: dir, SessionID: "po-1", Parent: "jevons", Purpose: purposeProductOwner,
	}); err != nil {
		t.Fatal(err)
	}
	st := ownerquestionview.New(state)
	if err := st.Record(ownerquestionview.Question{
		Identity:    ownerquestionview.Identity{Repo: dir, Target: "T1", ID: "approach", Version: "1"},
		Text:        "which approach?",
		Asker:       "claudia-po",
		AnswerRoute: "chat",
		Default:     "wait",
	}); err != nil {
		t.Fatal(err)
	}

	setPhase(s, "claudia-po", "idle", time.Time{})
	s.observeRegistryLiveness()

	var mu sync.Mutex
	var nudges, faults []string
	s.notifyJevon = func(text string) {
		mu.Lock()
		defer mu.Unlock()
		nudges = append(nudges, text)
	}
	s.SetOwnerNotifier(NewOwnerNotifier(func(subject, kind, text string) bool {
		mu.Lock()
		defer mu.Unlock()
		faults = append(faults, text)
		return true
	}))

	running := func(name string) bool { return name == "claudia-po" }
	now := time.Now().UTC()
	for i, dt := range []time.Duration{0, time.Minute, 2 * time.Hour} {
		s.tickPOIdleNudge("jevons", dir, now.Add(dt), poidlenudge.DefaultQueueThreshold, poidlenudge.DefaultFaultGrace, false, running)
		mu.Lock()
		gotNudges, gotFaults := len(nudges), len(faults)
		mu.Unlock()
		if gotNudges != 0 || gotFaults != 0 {
			t.Fatalf("cycle %d: nudges=%d faults=%d, want 0/0 (T1 open question, T2 depends on it)", i, gotNudges, gotFaults)
		}
	}
}

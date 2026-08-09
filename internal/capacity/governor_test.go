// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package capacity

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixedClock() func() time.Time {
	at := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	return func() time.Time { return at }
}

func TestGovernorHoldsAndReleasesSlots(t *testing.T) {
	snap := healthy()
	pol := DefaultPolicy()
	pol.MaxConcurrentBackground = 1
	g := NewGovernor(GovernorArgs{
		Snapshot: func() Snapshot { return snap },
		Policy:   func() *Policy { return pol },
		Now:      fixedClock(),
	})

	first, release := g.Begin("research", "schedule")
	if !first.Admitted() {
		t.Fatalf("first cycle refused: %s", first.Detail)
	}
	second, releaseSecond := g.Begin("rsi_coach", "schedule")
	if second.Verdict != VerdictDefer || second.Reason != ReasonBackgroundSlots {
		t.Fatalf("second cycle = %s/%s, want defer/%s", second.Verdict, second.Reason, ReasonBackgroundSlots)
	}
	releaseSecond() // no-op for deferred work
	release()

	third, release3 := g.Begin("rsi_coach", "schedule")
	defer release3()
	if !third.Admitted() {
		t.Fatalf("cycle after release refused: %s", third.Detail)
	}
	// Releasing twice must not underflow the counter into a phantom slot.
	release()
	if st := g.Status(); st.Classes[Rank(ClassCoach)].Running != 1 {
		t.Fatalf("coach running = %d, want 1", st.Classes[Rank(ClassCoach)].Running)
	}
}

func TestGovernorStickyOwnerNoticeFiresOnceAndClears(t *testing.T) {
	snap := healthy()
	var notices []string
	g := NewGovernor(GovernorArgs{
		Snapshot: func() Snapshot { return snap },
		Notify:   func(text string) { notices = append(notices, text) },
		Now:      fixedClock(),
	})

	snap.SpawnHalted = true
	for range 3 {
		if d, _ := g.Begin("research", "schedule"); d.Admitted() {
			t.Fatal("research admitted under critical pressure")
		}
	}
	if len(notices) != 1 {
		t.Fatalf("notices = %d, want exactly 1 sticky notice: %v", len(notices), notices)
	}
	if !strings.Contains(notices[0], "parked") {
		t.Errorf("notice does not say background is parked: %q", notices[0])
	}

	snap.SpawnHalted = false
	if d, release := g.Begin("research", "schedule"); !d.Admitted() {
		t.Fatalf("research still refused after recovery: %s", d.Detail)
	} else {
		release()
	}
	if len(notices) != 2 || !strings.Contains(notices[1], "cleared") {
		t.Fatalf("expected an all-clear notice, got %v", notices)
	}
}

func TestGovernorStatusIsSerialisableAndRanked(t *testing.T) {
	snap := healthy()
	g := NewGovernor(GovernorArgs{Snapshot: func() Snapshot { return snap }, Now: fixedClock()})
	_, release := g.Begin("research", "schedule")
	defer release()

	st := g.Status()
	if st.Assessment.Pressure != PressureNormal {
		t.Fatalf("pressure = %s, want normal", st.Assessment.Pressure)
	}
	if len(st.Classes) != len(Classes()) {
		t.Fatalf("classes = %d, want %d", len(st.Classes), len(Classes()))
	}
	for i, row := range st.Classes {
		if row.Rank != i {
			t.Fatalf("row %d has rank %d — status must be in priority order", i, row.Rank)
		}
	}
	research := st.Classes[Rank(ClassResearch)]
	if research.Running != 1 || research.Admitted != 1 || research.Last == nil {
		t.Fatalf("research row = %+v, want running/admitted 1 with a last decision", research)
	}
	// The plan covers every background class, so the cockpit can show what
	// would happen without waiting for a tick.
	if len(st.Plan) != len(Classes())-2 {
		t.Fatalf("plan covers %d classes, want %d background classes", len(st.Plan), len(Classes())-2)
	}
	data, err := json.Marshal(st)
	if err != nil {
		t.Fatalf("status does not marshal: %v", err)
	}
	if !strings.Contains(string(data), `"pressure":"normal"`) {
		t.Errorf("status JSON missing readable pressure: %s", data)
	}
}

func TestAskWithoutGateAdmits(t *testing.T) {
	d, release := Ask(nil, ClassResearch, "schedule")
	defer release()
	if !d.Admitted() || d.Tier != TierFull {
		t.Fatalf("nil gate = %s/%s, want admit/full", d.Verdict, d.Tier)
	}
}

func TestGovernorSatisfiesGate(t *testing.T) {
	var _ Gate = NewGovernor(GovernorArgs{})
}

func TestPolicyRoundTripAndRepair(t *testing.T) {
	dir := t.TempDir()
	path := ConfigPath(dir)

	pol, err := LoadPolicy(path)
	if err != nil {
		t.Fatalf("missing file must yield defaults: %v", err)
	}
	if pol.OwnerReserveFraction != DefaultPolicy().OwnerReserveFraction {
		t.Fatalf("missing file did not yield defaults: %+v", pol)
	}

	pol.MaxConcurrentBackground = 5
	pol.RetryAfter = Duration(90 * time.Second)
	if err := pol.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), `"retry_after": "1m30s"`) {
		t.Errorf("retry_after must be human-editable, got: %s", raw)
	}
	back, err := LoadPolicy(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if back.MaxConcurrentBackground != 5 || back.RetryAfter.Std() != 90*time.Second {
		t.Fatalf("round trip lost fields: %+v", back)
	}

	// A degrade line under the owner reserve would make degradation
	// unreachable; loading repairs it rather than shipping a dead rung.
	bad := filepath.Join(dir, "bad.json")
	os.WriteFile(bad, []byte(`{"owner_reserve_fraction":0.5,"degrade_fraction":0.1}`), 0o644)
	repaired, err := LoadPolicy(bad)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if repaired.DegradeFraction < repaired.OwnerReserveFraction {
		t.Fatalf("degrade %.2f below reserve %.2f", repaired.DegradeFraction, repaired.OwnerReserveFraction)
	}

	// Malformed policy is an error, never a silent fallback to defaults.
	broken := filepath.Join(dir, "broken.json")
	os.WriteFile(broken, []byte(`{`), 0o644)
	if _, err := LoadPolicy(broken); err == nil {
		t.Fatal("malformed policy must be an error")
	}
}

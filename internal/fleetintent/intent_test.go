// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleetintent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 🎯T414 acceptance 1: the intent is explicit, deliberate, and distinct from
// whatever the process happens to be doing. These are the policy's own tests;
// the cross of every control against every state lives in
// internal/mcpserver/t414_intent_oracle_test.go, because that is where the
// controls are.

func TestUnknownResolvesToWorking(t *testing.T) {
	if got := Resolve(Unknown); got != Working {
		t.Fatalf("Resolve(Unknown) = %q, want %q", got, Working)
	}
	if !Runnable(Unknown) {
		t.Fatal("Unknown must be runnable: an unstamped agent behaves as it did before this package existed")
	}
	// The mirror-image failure. A suppressing default would silently stop the
	// fleet reviving anything registered before intent existed.
	if Resolve(Unknown) == Parked {
		t.Fatal("Unknown must never default to a suppressing state")
	}
}

func TestValidRejectsUnknownAndJunk(t *testing.T) {
	for _, s := range AllStates() {
		if !Valid(s) {
			t.Errorf("Valid(%q) = false", s)
		}
	}
	if Valid(Unknown) {
		t.Error("Unknown is the absence of a stored value, not a storable one")
	}
	if Valid(State("asleep")) {
		t.Error("Valid accepted a state this package does not know")
	}
}

func TestParseAliasesAndRejection(t *testing.T) {
	cases := map[string]State{
		"":                    Working,
		"working":             Working,
		"unpark":              Working,
		"park":                Parked,
		"stood-down":          Parked,
		"provider blocked":    BlockedProvider,
		"blocked_on_owner":    BlockedOwner,
		"finished_and_reaped": Reaped,
	}
	for in, want := range cases {
		got, err := Parse(in)
		if err != nil {
			t.Errorf("Parse(%q): %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("Parse(%q) = %q, want %q", in, got, want)
		}
	}
	if _, err := Parse("maybe"); err == nil {
		t.Fatal("Parse accepted an unknown state — a mistyped park must fail loudly, not resolve to something")
	}
}

// TestAllowsIsBidirectional is the load-bearing property in both directions:
// no non-working intent authorises any control, and working intent declines
// none of them.
func TestAllowsIsBidirectional(t *testing.T) {
	for _, c := range AllControls() {
		if d := Allows(Working, Working, c); !d.Allow || d.Reason != ReasonCodeOK {
			t.Errorf("%s under working intent: %+v, want allow", c, d)
		}
		if d := Allows(Unknown, Unknown, c); !d.Allow {
			t.Errorf("%s under unstamped intent: %+v, want allow", c, d)
		}
		for _, s := range AllStates() {
			if s == Working {
				continue
			}
			if d := Allows(s, Working, c); d.Allow {
				t.Errorf("%s under fleet intent %q: allowed, want declined", c, s)
			} else if d.Reason != FleetReason(s) || d.Blocking != s {
				t.Errorf("%s under fleet intent %q: reason=%q blocking=%q", c, s, d.Reason, d.Blocking)
			}
			if d := Allows(Working, s, c); d.Allow {
				t.Errorf("%s under agent intent %q: allowed, want declined", c, s)
			} else if d.Reason != AgentReason(s) || d.Blocking != s {
				t.Errorf("%s under agent intent %q: reason=%q blocking=%q", c, s, d.Reason, d.Blocking)
			}
		}
	}
}

// TestFleetIntentOutranksAgentIntent: when both are set the fleet's answer is
// reported, because a provider wall is what the operator needs to hear.
func TestFleetIntentOutranksAgentIntent(t *testing.T) {
	d := Allows(BlockedProvider, Parked, ControlSpawn)
	if d.Allow {
		t.Fatal("allowed under two non-working intents")
	}
	if d.Blocking != BlockedProvider {
		t.Fatalf("Blocking = %q, want the fleet-wide %q", d.Blocking, BlockedProvider)
	}
	if !strings.Contains(d.String(), "provider") {
		t.Fatalf("decision %q does not name the provider wall", d.String())
	}
}

func TestSnapshotReadsAndSummarizes(t *testing.T) {
	snap := Snapshot{
		Agents: map[string]Record{
			"jv-a": {State: Parked, By: "jevons", Reason: "spend block"},
			"jv-b": {State: Working},
			"jv-c": {State: Reaped},
		},
	}
	if got := snap.AgentState("jv-a"); got != Parked {
		t.Errorf("AgentState(jv-a) = %q", got)
	}
	if got := snap.AgentState("never-seen"); got != Working {
		t.Errorf("AgentState of an agent with no record = %q, want working", got)
	}
	if got := snap.NotWorking(); len(got) != 2 || got[0] != "jv-a" || got[1] != "jv-c" {
		t.Errorf("NotWorking = %v, want [jv-a jv-c] sorted", got)
	}
	if !snap.AllowSpawn().Allow {
		t.Error("AllowSpawn must ignore per-agent intent: a new worker has no row yet")
	}
	sum := snap.Summarize()
	for _, want := range []string{"jv-a", "jv-c", "2 stood down"} {
		if !strings.Contains(sum, want) {
			t.Errorf("Summarize() = %q, missing %q", sum, want)
		}
	}
}

// TestStoreParkSurvivesReopen is the incident: a daemon restart resurrected
// deliberately parked workers, so an intent that lives only in memory would be
// erased by exactly the event it exists to survive.
func TestStoreParkSurvivesReopen(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetAgent("jv-t370-auto", Parked, "jevons", "anthropic spend block", time.Time{}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetFleet(BlockedProvider, "jevons", "anthropic spend block", time.Time{}); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.AgentState("jv-t370-auto"); got != Parked {
		t.Fatalf("after reopen agent intent = %q, want parked", got)
	}
	if got := reopened.FleetState(); got != BlockedProvider {
		t.Fatalf("after reopen fleet intent = %q, want blocked_provider", got)
	}
	if d := reopened.Allow("jv-t370-auto", ControlRevive); d.Allow {
		t.Fatal("a restart re-authorised revival of a parked worker — the 2026-08-10 incident")
	}
	if rec := reopened.Snapshot().Agents["jv-t370-auto"]; rec.By != "jevons" || rec.Reason != "anthropic spend block" {
		t.Fatalf("provenance lost across reopen: %+v", rec)
	}
}

func TestStoreLiftAndForget(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetAgent("jv-a", Parked, "jevons", "", time.Time{}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetAgent("jv-a", Working, "owner", "spend restored", time.Time{}); err != nil {
		t.Fatal(err)
	}
	if !st.Allow("jv-a", ControlNudge).Allow {
		t.Fatal("lifting a park did not re-authorise controls")
	}
	if err := st.Forget("jv-a"); err != nil {
		t.Fatal(err)
	}
	if _, ok := st.Snapshot().Agents["jv-a"]; ok {
		t.Fatal("Forget left a record behind")
	}
	if err := st.Forget("never-stored"); err != nil {
		t.Fatalf("Forget of an unknown name: %v", err)
	}
}

// TestStoreRejectsMalformed: malformed state is a hard error, never a silent
// reset — a store that quietly forgets a park is worse than one that refuses
// to start, because the forgetting is invisible until something is resurrected.
func TestStoreRejectsMalformed(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "fleet"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "fleet", StoreFileName)
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir); err == nil {
		t.Fatal("Open silently accepted a malformed intent file")
	}
	if _, err := Open(""); err == nil {
		t.Fatal("Open accepted an empty state dir")
	}
}

func TestStoreRejectsInvalidState(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetAgent("jv-a", State("asleep"), "jevons", "", time.Time{}); err == nil {
		t.Fatal("SetAgent stored a state no control knows how to read")
	}
	if err := st.SetAgent("  ", Parked, "jevons", "", time.Time{}); err == nil {
		t.Fatal("SetAgent accepted an empty name")
	}
}

// TestReapedStampsExpireButParksDoNot: Reaped is the one state that
// accumulates without bound. Parked and the blocked states are standing
// instructions — pruning one is the failure this package exists to prevent.
func TestReapedStampsExpireButParksDoNot(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-ReapedRetention - time.Hour)
	if err := st.SetAgent("jv-old-reap", Reaped, "product", "", old); err != nil {
		t.Fatal(err)
	}
	if err := st.SetAgent("jv-old-park", Parked, "jevons", "", old); err != nil {
		t.Fatal(err)
	}
	// Any later write runs the prune.
	if err := st.SetAgent("jv-fresh", Reaped, "product", "", time.Time{}); err != nil {
		t.Fatal(err)
	}
	agents := st.Snapshot().Agents
	if _, ok := agents["jv-old-reap"]; ok {
		t.Error("an expired reap stamp was kept")
	}
	if got := Resolve(agents["jv-old-park"].State); got != Parked {
		t.Errorf("an old park was pruned (state now %q) — a standing instruction was forgotten", got)
	}
	if _, ok := agents["jv-fresh"]; !ok {
		t.Error("a fresh reap stamp was pruned")
	}
}

// TestNilStoreIsAllWorking: a daemon started without a state dir behaves
// exactly as it did before this package existed, so no call site needs a nil
// check and none can be forgotten.
func TestNilStoreIsAllWorking(t *testing.T) {
	var st *Store
	if got := st.FleetState(); got != Working {
		t.Errorf("nil store FleetState = %q", got)
	}
	for _, c := range AllControls() {
		if !st.Allow("anyone", c).Allow {
			t.Errorf("nil store declined %s", c)
		}
	}
	if !st.AllowSpawn().Allow {
		t.Error("nil store declined spawn")
	}
	if err := st.SetAgent("jv-a", Parked, "jevons", "", time.Time{}); err != nil {
		t.Errorf("nil store SetAgent: %v", err)
	}
	if st.Path() != "" {
		t.Error("nil store reported a path")
	}
}

func TestStorePersistsValidJSON(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetAgent("jv-a", BlockedOwner, "jevons", "needs a keypress", time.Time{}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(st.Path())
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Agents map[string]Record `json:"agents"`
	}
	if err := json.Unmarshal(b, &file); err != nil {
		t.Fatalf("persisted file is not valid JSON: %v", err)
	}
	if file.Agents["jv-a"].State != BlockedOwner {
		t.Fatalf("persisted record = %+v", file.Agents["jv-a"])
	}
	if _, err := os.Stat(st.Path() + ".tmp"); !os.IsNotExist(err) {
		t.Error("write-and-rename left its temp file behind")
	}
}

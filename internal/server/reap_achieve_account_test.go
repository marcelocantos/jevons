// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/fleetlog"
)

// capturedEvent is one event the chokepoint emitted.
type capturedEvent struct {
	Component string
	Decision  string
	Fields    map[string]any
}

func captureAccount() (*fleetlog.Account, *[]capturedEvent) {
	var got []capturedEvent
	acct := fleetlog.New(func(component, decision string, fields map[string]any) {
		got = append(got, capturedEvent{Component: component, Decision: decision, Fields: fields})
	})
	return acct, &got
}

// TestT435AchieveReapIsNotSilent replays the 2026-08-10 20:55–20:57 sequence
// that produced the escalation this target was filed from (🎯T435 clause 3).
//
// That night two work agents left agents.json ninety seconds apart while
// their sessions were still appending turns. The reap was correct — 🎯T195
// hygiene on jevons-po's own achieves of 🎯T420 and 🎯T386 — but it emitted
// only slog.Info to daily-jevonsd.log, so two competent observers each read
// the registry diff as "a live worker orphaned out of the fleet with no
// record, its output uncollectable". The defect was the silence.
//
// The oracle is deliberately the one a monitor would run: take the registry
// before and after, and require the event log alone to explain every row
// that vanished, with a designed cause rather than a shrug.
func TestT435AchieveReapIsNotSilent(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []claudia.AgentDef{
		{Name: "jevons", WorkDir: dir, SessionID: "s-o", Purpose: claudia.PurposeOverseer, Materialized: true, Provider: "grok"},
		{Name: "jevons-po", WorkDir: dir, SessionID: "s-po", Purpose: claudia.PurposeWork, Parent: "jevons", Materialized: true, Provider: "grok"},
		{Name: "jv-t420-recovery-oracle", WorkDir: dir, SessionID: "s-420", Purpose: claudia.PurposeWork, Parent: "jevons-po", Materialized: true, Provider: "grok", TargetID: "T420"},
		{Name: "jv-t386-green-exit-status", WorkDir: dir, SessionID: "s-386", Purpose: claudia.PurposeWork, Parent: "jevons-po", Materialized: true, Provider: "grok", TargetID: "T386"},
		{Name: "jv-t416-send-confirm", WorkDir: dir, SessionID: "s-416", Purpose: claudia.PurposeWork, Parent: "jevons-po", Materialized: true, Provider: "grok", TargetID: "T416"},
	} {
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}
	before := registeredNames(reg)

	acct, events := captureAccount()
	isO := func(n string) bool { return n == "jevons" }
	// 20:55 achieve of 🎯T420, then 20:56 achieve of 🎯T386.
	for _, tid := range []string{"T420", "T386"} {
		if _, err := ReapWorkAgentsOnTargetAchieve(reg, acct, tid, dir, isO); err != nil {
			t.Fatalf("reap on achieve of %s: %v", tid, err)
		}
	}
	after := registeredNames(reg)

	// The diff the escalation was reconstructed from: two rows gone.
	vanished := map[string]bool{}
	for n := range before {
		if !after[n] {
			vanished[n] = true
		}
	}
	want := map[string]bool{"jv-t420-recovery-oracle": true, "jv-t386-green-exit-status": true}
	if len(vanished) != len(want) {
		t.Fatalf("registry diff = %v, want %v", vanished, want)
	}
	for n := range want {
		if !vanished[n] {
			t.Fatalf("registry diff = %v, want %v", vanished, want)
		}
	}

	// Every vanished row is accounted for by the log alone, with a reason
	// from the closed vocabulary and a sentence naming the achieve.
	accounted := map[string]capturedEvent{}
	for _, ev := range *events {
		if ev.Component != fleetlog.Component || ev.Decision != fleetlog.Decision {
			t.Fatalf("unexpected event %s/%s", ev.Component, ev.Decision)
		}
		name, _ := ev.Fields["name"].(string)
		accounted[name] = ev
	}
	for n := range vanished {
		ev, ok := accounted[n]
		if !ok {
			t.Fatalf("🎯T435 clause 3: %s left the registry with no event — "+
				"this is the silence the 2026-08-10 escalation read as an orphaning", n)
		}
		reason, _ := ev.Fields["reason"].(string)
		if reason != fleetlog.ReasonReapAchieve {
			t.Fatalf("%s reason = %q, want %q", n, reason, fleetlog.ReasonReapAchieve)
		}
		if !fleetlog.KnownReason(reason) {
			t.Fatalf("%s reason %q is outside the closed vocabulary", n, reason)
		}
		if got, _ := ev.Fields["parent"].(string); got != "jevons-po" {
			t.Fatalf("%s parent = %q, want jevons-po", n, got)
		}
		if got, _ := ev.Fields["level"].(string); got != "" {
			t.Fatalf("%s emitted at level %q — a designed teardown is not a warning", n, got)
		}
	}
	// And the sentence a reader gets is the designed teardown, per target.
	for name, tid := range map[string]string{
		"jv-t420-recovery-oracle":   "T420",
		"jv-t386-green-exit-status": "T386",
	} {
		msg, _ := accounted[name].Fields["msg"].(string)
		if !strings.Contains(msg, "reaped on achieve of 🎯"+tid) {
			t.Fatalf("%s msg = %q, want it to name the achieve of 🎯%s", name, msg, tid)
		}
		if got, _ := accounted[name].Fields["target_id"].(string); got != tid {
			t.Fatalf("%s target_id = %q, want %q", name, got, tid)
		}
	}
	// A worker on an unachieved target is untouched and unlogged: the account
	// covers the diff, and does not invent one.
	if _, ok := accounted["jv-t416-send-confirm"]; ok {
		t.Fatal("unrelated agent accounted for a removal that did not happen")
	}
	if reg.Def("jv-t416-send-confirm") == nil || reg.Def("jevons-po") == nil {
		t.Fatal("unrelated worker and PO must survive the achieve reap")
	}

	// Clause 2: the PO reads the cause off the fleet surface, not the log.
	notices := acct.Recent(0)
	if len(notices) != 2 {
		t.Fatalf("recent notices = %d, want 2", len(notices))
	}
	block := fleetlog.FormatNotices(notices)
	for _, name := range []string{"jv-t420-recovery-oracle", "jv-t386-green-exit-status"} {
		if !strings.Contains(block, name) {
			t.Fatalf("fleet-surface block omits %s:\n%s", name, block)
		}
	}
	if !strings.Contains(block, fleetlog.ReasonReapAchieve) {
		t.Fatalf("fleet-surface block omits the reason:\n%s", block)
	}
}

// TestT435ReplayIsRedOnThePreFixTree is the control for the test above
// (🎯T435 clause 4). It replays the same night through the code as it stood
// on 2026-08-10 — reg.Remove directly, slog.Info to the daemon log, nothing
// to the journal — and shows the oracle catches it. An oracle that passes on
// the tree that produced the incident is not evidence of anything.
func TestT435ReplayIsRedOnThePreFixTree(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []claudia.AgentDef{
		{Name: "jevons-po", WorkDir: dir, SessionID: "s-po", Purpose: claudia.PurposeWork, Parent: "jevons", Materialized: true, Provider: "grok"},
		{Name: "jv-t420-recovery-oracle", WorkDir: dir, SessionID: "s-420", Purpose: claudia.PurposeWork, Parent: "jevons-po", Materialized: true, Provider: "grok", TargetID: "T420"},
	} {
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}
	before := registeredNames(reg)

	_, events := captureAccount()
	// The pre-fix removal: straight to the registry, no account.
	if err := reg.Remove("jv-t420-recovery-oracle"); err != nil {
		t.Fatal(err)
	}
	after := registeredNames(reg)

	accounted := map[string]bool{}
	for _, ev := range *events {
		if name, _ := ev.Fields["name"].(string); name != "" {
			accounted[name] = true
		}
	}
	unexplained := 0
	for n := range before {
		if !after[n] && !accounted[n] {
			unexplained++
		}
	}
	if unexplained == 0 {
		t.Fatal("control failed: the pre-fix removal was explained by the event log, " +
			"so the oracle above would pass on the tree that produced the incident")
	}
}

// TestT435UnaccountedRemovalIsLoud is the red half of clause 1: a caller that
// reaches the chokepoint without naming a cause still emits, and says so at
// warn level. Silence is the one outcome the chokepoint may not produce.
func TestT435UnaccountedRemovalIsLoud(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jv-nameless", WorkDir: dir, SessionID: "s-1",
		Purpose: claudia.PurposeWork, Parent: "jevons-po", Materialized: true, Provider: "grok",
	}); err != nil {
		t.Fatal(err)
	}
	acct, events := captureAccount()
	if _, err := acct.Remove(reg, "jv-nameless", fleetlog.Removal{}); err != nil {
		t.Fatal(err)
	}
	if len(*events) != 1 {
		t.Fatalf("events = %d, want 1", len(*events))
	}
	ev := (*events)[0]
	if got, _ := ev.Fields["reason"].(string); got != fleetlog.ReasonUnaccounted {
		t.Fatalf("reason = %q, want %q", got, fleetlog.ReasonUnaccounted)
	}
	if got, _ := ev.Fields["level"].(string); got != "warn" {
		t.Fatalf("level = %q, want warn — a removal with no cause is a caller bug", got)
	}
}

func registeredNames(reg *claudia.Registry) map[string]bool {
	out := map[string]bool{}
	for _, d := range reg.List() {
		out[d.Name] = true
	}
	return out
}

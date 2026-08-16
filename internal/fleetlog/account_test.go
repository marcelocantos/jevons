// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleetlog

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
)

// capturedEvent is one call into the Logger seam.
type capturedEvent struct {
	Component string
	Decision  string
	Fields    map[string]any
}

type recorder struct{ events []capturedEvent }

func (r *recorder) log(component, decision string, fields map[string]any) {
	r.events = append(r.events, capturedEvent{component, decision, fields})
}

// removalFor returns the accounted removal event for name, or fails.
func (r *recorder) removalFor(t *testing.T, name string) capturedEvent {
	t.Helper()
	for _, ev := range r.events {
		if ev.Component != Component || ev.Decision != Decision {
			continue
		}
		if s, _ := ev.Fields["name"].(string); s == name {
			return ev
		}
	}
	t.Fatalf("no accounted removal for %q in %d events: %+v", name, len(r.events), r.events)
	return capturedEvent{}
}

func newTestRegistry(t *testing.T, defs ...claudia.AgentDef) *claudia.Registry {
	t.Helper()
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}
	for _, d := range defs {
		if err := reg.Register(d); err != nil {
			t.Fatalf("register %q: %v", d.Name, err)
		}
	}
	return reg
}

func work(name, parent, targetID string) claudia.AgentDef {
	return claudia.AgentDef{
		Name:      name,
		WorkDir:   "/tmp/repo",
		SessionID: "sess-" + name,
		Parent:    parent,
		Purpose:   claudia.PurposeWork,
		TargetID:  targetID,
	}
}

// TestRemoveAccountsForEveryReason walks the closed vocabulary: each removal
// path names its cause, and the event carries the row that vanished.
func TestRemoveAccountsForEveryReason(t *testing.T) {
	for _, reason := range Reasons() {
		t.Run(reason, func(t *testing.T) {
			rec := &recorder{}
			acct := New(rec.log)
			reg := newTestRegistry(t, work("jv-t1-worker", "jevons-po", "T1"))

			removed, err := acct.Remove(reg, "jv-t1-worker", Removal{
				Reason: reason,
				Detail: "because the test said so",
				Actor:  "jevons-po",
			})
			if err != nil {
				t.Fatalf("remove: %v", err)
			}
			if !removed {
				t.Fatal("remove reported no row left the registry")
			}
			if reg.Def("jv-t1-worker") != nil {
				t.Fatal("agent still registered after remove")
			}

			ev := rec.removalFor(t, "jv-t1-worker")
			if got := ev.Fields["reason"]; got != reason {
				t.Errorf("reason = %v, want %v", got, reason)
			}
			if got := ev.Fields["parent"]; got != "jevons-po" {
				t.Errorf("parent = %v, want jevons-po", got)
			}
			if got := ev.Fields["target_id"]; got != "T1" {
				t.Errorf("target_id = %v, want T1", got)
			}
			if got := ev.Fields["outcome"]; got != "ok" {
				t.Errorf("outcome = %v, want ok", got)
			}
			if msg, _ := ev.Fields["msg"].(string); !strings.Contains(msg, "because the test said so") {
				t.Errorf("msg = %q, want the caller's detail in it", msg)
			}
		})
	}
}

// TestRemoveWithoutAReasonIsLoud: reaching the chokepoint with no cause is a
// caller bug. The row still leaves — refusing the removal would be worse —
// but the event says unaccounted at warn level rather than inventing a cause.
func TestRemoveWithoutAReasonIsLoud(t *testing.T) {
	rec := &recorder{}
	acct := New(rec.log)
	reg := newTestRegistry(t, work("jv-nameless", "jevons-po", ""))

	if _, err := acct.Remove(reg, "jv-nameless", Removal{}); err != nil {
		t.Fatalf("remove: %v", err)
	}
	ev := rec.removalFor(t, "jv-nameless")
	if got := ev.Fields["reason"]; got != ReasonUnaccounted {
		t.Errorf("reason = %v, want %v", got, ReasonUnaccounted)
	}
	if got := ev.Fields["level"]; got != "warn" {
		t.Errorf("level = %v, want warn", got)
	}
}

// TestRemoveSubtreeAccountsForEveryDescendant: a subtree kill is several
// registry diffs, and each one needs its own account. Naming only the root
// leaves the children looking like the silent disappearances this target is
// about.
func TestRemoveSubtreeAccountsForEveryDescendant(t *testing.T) {
	rec := &recorder{}
	acct := New(rec.log)
	reg := newTestRegistry(t,
		work("jv-boss", "jevons-po", "T9"),
		work("jv-child-a", "jv-boss", "T9.1"),
		work("jv-grandchild", "jv-child-a", "T9.1.1"),
	)

	removed, err := acct.RemoveSubtree(reg, "jv-boss", Removal{
		Reason: ReasonKill,
		Detail: "owner killed the mission",
	})
	if err != nil {
		t.Fatalf("remove subtree: %v", err)
	}
	if len(removed) != 3 {
		t.Fatalf("removed = %v, want all three rows", removed)
	}
	if got := removed[len(removed)-1]; got != "jv-boss" {
		t.Errorf("root removed at %q, want last (children first)", got)
	}
	for _, name := range []string{"jv-boss", "jv-child-a", "jv-grandchild"} {
		if reg.Def(name) != nil {
			t.Errorf("%s still registered", name)
		}
		ev := rec.removalFor(t, name)
		if got := ev.Fields["reason"]; got != ReasonKill {
			t.Errorf("%s reason = %v, want %v", name, got, ReasonKill)
		}
	}
	// Descendants name the decision that took them.
	child := rec.removalFor(t, "jv-grandchild")
	if got, _ := child.Fields["root"].(string); got != "jv-boss" {
		t.Errorf("grandchild root = %q, want jv-boss", got)
	}
	if root := rec.removalFor(t, "jv-boss"); root.Fields["root"] != nil {
		t.Errorf("root row carries a root field: %v", root.Fields["root"])
	}
}

// TestRemoveOfAnAbsentRowIsNotAnEvent: no registry diff, nothing to explain.
// Accounting for removals that did not happen would train a reader to ignore
// the log.
func TestRemoveOfAnAbsentRowIsNotAnEvent(t *testing.T) {
	rec := &recorder{}
	acct := New(rec.log)
	reg := newTestRegistry(t)

	removed, err := acct.Remove(reg, "never-existed", Removal{Reason: ReasonKill})
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	if removed {
		t.Error("reported a removal of an unregistered name")
	}
	if len(rec.events) != 0 {
		t.Errorf("emitted %d events for a no-op removal: %+v", len(rec.events), rec.events)
	}
}

// TestNilAccountStillRemoves: unwired call paths (tests, alternate wiring)
// must still be able to remove an agent. Silence about the event is the
// cost; a stuck registry would be worse.
func TestNilAccountStillRemoves(t *testing.T) {
	var acct *Account
	reg := newTestRegistry(t, work("jv-unwired", "", ""))
	removed, err := acct.Remove(reg, "jv-unwired", Removal{Reason: ReasonThreadRemove})
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	if !removed || reg.Def("jv-unwired") != nil {
		t.Fatal("nil account failed to remove the agent")
	}
}

// TestRecentNoticesAgeOut keeps the fleet surface honest: a removal explains
// a diff for as long as a reader might be looking at it, then leaves.
func TestRecentNoticesAgeOut(t *testing.T) {
	rec := &recorder{}
	acct := New(rec.log)
	now := time.Date(2026, 8, 10, 20, 55, 30, 0, time.UTC)
	acct.SetClock(func() time.Time { return now })
	reg := newTestRegistry(t, work("jv-old", "jevons-po", "T420"), work("jv-new", "jevons-po", "T386"))

	if _, err := acct.Remove(reg, "jv-old", Removal{Reason: ReasonReapAchieve}); err != nil {
		t.Fatalf("remove: %v", err)
	}
	now = now.Add(time.Hour)
	if _, err := acct.Remove(reg, "jv-new", Removal{Reason: ReasonReapAchieve}); err != nil {
		t.Fatalf("remove: %v", err)
	}

	got := acct.Recent(DefaultNoticeWindow)
	if len(got) != 1 || got[0].Name != "jv-new" {
		t.Fatalf("recent = %+v, want only jv-new inside the window", got)
	}
	if all := acct.Recent(2 * time.Hour); len(all) != 2 {
		t.Fatalf("wide window = %+v, want both", all)
	}
	block := FormatNotices(acct.Recent(2 * time.Hour))
	for _, want := range []string{"jv-old", "jv-new", ReasonReapAchieve} {
		if !strings.Contains(block, want) {
			t.Errorf("notice block missing %q:\n%s", want, block)
		}
	}
	if body := PrependNotices("agent rows here", acct.Recent(2*time.Hour)); !strings.Contains(body, "agent rows here") {
		t.Errorf("prepend dropped the body: %s", body)
	}
	if body := PrependNotices("agent rows here", nil); body != "agent rows here" {
		t.Errorf("prepend with no notices = %q", body)
	}
}

// TestKnownReasonCoversTheVocabulary guards the closed list against a caller
// inventing a reason no reader can enumerate.
func TestKnownReasonCoversTheVocabulary(t *testing.T) {
	for _, r := range Reasons() {
		if !KnownReason(r) {
			t.Errorf("KnownReason(%q) = false", r)
		}
	}
	if KnownReason("vanished") {
		t.Error("KnownReason accepted an invented reason")
	}
	if KnownReason(ReasonUnaccounted) {
		t.Error("unaccounted is a fault marker, not a reason a caller may pick")
	}
}

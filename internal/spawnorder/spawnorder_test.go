// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package spawnorder

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/eventlog"
)

var t0 = time.Date(2026, 9, 21, 6, 35, 0, 0, time.UTC)

func order(seats ...Seat) Order {
	return Order{ID: "o-test", Parent: "jevons-po", IssuedAt: t0, Seats: seats}
}

func statuses(r Result) map[string]Status {
	out := map[string]Status{}
	for _, s := range r.Seats {
		out[s.Name] = s.Status
	}
	return out
}

// 🎯T762 acceptance 1: every named seat gets a verdict, not only the ones that
// exist — the 2026-09-21 shape, claude half minted and grok half never asked.
func TestReconcileNamesEverySeat(t *testing.T) {
	o := order(
		Seat{Name: "jv-t749", Provider: "claude"},
		Seat{Name: "jv-t759", Provider: "grok"},
		Seat{Name: "jv-t760", Provider: "grok"},
		Seat{Name: "jv-t755", Provider: "grok"},
		Seat{Name: "jv-t743", Provider: "grok"},
	)
	r := Reconcile(o, Evidence{Attempts: []Attempt{
		{Name: "jv-t749", At: t0.Add(time.Minute), OK: true, Provider: "claude", OrderID: "o-test"},
		{Name: "jv-t760", At: t0.Add(2 * time.Minute), Err: "dest_saturated", Provider: "claude", OrderID: "o-test"},
		{Name: "jv-t755", At: t0.Add(3 * time.Minute), OK: true, Provider: "claude", OrderID: "o-test"},
		// An unattributed attempt well before the order is not this order's.
		{Name: "jv-t743", At: t0.Add(-time.Hour), OK: true, Provider: "grok"},
	}})
	want := map[string]Status{
		"jv-t749": Minted, "jv-t759": NotAttempted, "jv-t760": Refused,
		"jv-t755": Rerouted, "jv-t743": NotAttempted,
	}
	for name, st := range statuses(r) {
		if st != want[name] {
			t.Errorf("%s = %s, want %s", name, st, want[name])
		}
	}
	if r.Complete() {
		t.Fatal("an order with dropped seats reported complete")
	}
	line := r.Line()
	for _, frag := range []string{"2/5 minted", "INCOMPLETE", "jv-t759 (grok) not_attempted", "jv-t760 (grok) refused: dest_saturated", "jv-t755 (grok) rerouted: minted on claude"} {
		if !strings.Contains(line, frag) {
			t.Errorf("line lacks %q: %s", frag, line)
		}
	}
}

// acceptance 2 converse: a fully minted order reads complete and names nobody.
func TestReconcileCompleteOrder(t *testing.T) {
	o := order(Seat{Name: "a", Provider: "claude"}, Seat{Name: "b", Provider: "grok"})
	r := Reconcile(o, Evidence{Attempts: []Attempt{
		{Name: "a", At: t0.Add(time.Minute), OK: true, Provider: "claude", OrderID: "o-test"},
		{Name: "b", At: t0.Add(time.Minute), Err: "launch timed out", OrderID: "o-test"},
		{Name: "b", At: t0.Add(2 * time.Minute), OK: true, Provider: "grok", OrderID: "o-test"},
	}})
	if got := r.Line(); got != "order o-test: 2/2 minted (complete)" {
		t.Fatalf("line = %q", got)
	}
}

// Review finding 3: missing evidence is unknown, never not_attempted.
func TestMissingEvidenceIsUnknown(t *testing.T) {
	o := order(Seat{Name: "a", Provider: "grok"})
	cases := map[string]Evidence{
		"journal unreadable": {ReadErr: "permission denied"},
		"tail too short":     {CoveredSince: t0.Add(time.Minute)},
		"older incarnation":  {Present: map[string]Incarnation{"a": {Provider: "grok", Target: "T100", Session: "s-old"}}},
	}
	for name, ev := range cases {
		r := Reconcile(o, ev)
		if r.Seats[0].Status != Unknown || r.Complete() {
			t.Errorf("%s: status = %s (%s), want unknown and incomplete", name, r.Seats[0].Status, r.Seats[0].Reason)
		}
	}
	// A tail that does reach back past the window decides absence.
	if r := Reconcile(o, Evidence{CoveredSince: t0.Add(-time.Hour)}); r.Seats[0].Status != NotAttempted {
		t.Fatalf("covered window: %s", r.Seats[0].Status)
	}
}

// Mission 3 limits 1 and 2: only a start carrying this order's id is
// attributed to it. A same-name start without one reads unknown whatever its
// target — a missing target is not a wildcard, and an agreeing target is not
// identity — while a conflicting target or another order's id rules it out.
func TestOnlyOrderIDAttributesAStart(t *testing.T) {
	at := t0.Add(time.Minute)
	cases := []struct {
		name string
		seat Seat
		a    Attempt
		want Status
		frag string
	}{
		{"no target on seat, unattributed ok", Seat{Name: "x", Provider: "grok"}, Attempt{Name: "x", At: at, OK: true, Provider: "grok", Target: "T100"}, Unknown, "carries no order id"},
		{"no target on start, unattributed ok", Seat{Name: "x", Provider: "grok", Target: "T759"}, Attempt{Name: "x", At: at, OK: true, Provider: "grok"}, Unknown, "carries no order id"},
		{"matching target, unattributed ok", Seat{Name: "x", Provider: "grok", Target: "T759"}, Attempt{Name: "x", At: at, OK: true, Provider: "grok", Target: "🎯t759"}, Unknown, "cannot be attributed to order o-test"},
		{"unattributed refusal", Seat{Name: "x", Provider: "grok"}, Attempt{Name: "x", At: at, Err: "dest_saturated"}, Unknown, "refused: dest_saturated"},
		{"conflicting target", Seat{Name: "x", Provider: "grok", Target: "T759"}, Attempt{Name: "x", At: at, OK: true, Provider: "grok", Target: "T100"}, NotAttempted, "no matching daemon start observed"},
		{"another order's start", Seat{Name: "x", Provider: "grok"}, Attempt{Name: "x", At: at, OK: true, Provider: "grok", OrderID: "o-other"}, NotAttempted, "no matching daemon start observed"},
		{"this order's start, no target anywhere", Seat{Name: "x", Provider: "grok"}, Attempt{Name: "x", At: at, OK: true, Provider: "grok", OrderID: "o-test"}, Minted, ""},
		{"this order's start before the window", Seat{Name: "x", Provider: "grok"}, Attempt{Name: "x", At: t0.Add(-time.Hour), OK: true, Provider: "grok", OrderID: "o-test"}, Minted, ""},
	}
	for _, c := range cases {
		r := Reconcile(order(c.seat), Evidence{Attempts: []Attempt{c.a}})
		got := r.Seats[0]
		if got.Status != c.want || !strings.Contains(got.Reason, c.frag) {
			t.Errorf("%s: %s (%s), want %s containing %q", c.name, got.Status, got.Reason, c.want, c.frag)
		}
		if c.want == Unknown && (r.Complete() || got.Observed != nil) {
			t.Errorf("%s: an unattributed start completed the order or was persisted as this order's", c.name)
		}
	}
}

// Mission 3 limit 3: not_attempted says what the journal can and cannot see.
func TestNotAttemptedWordingNamesItsScope(t *testing.T) {
	r := Reconcile(order(Seat{Name: "x", Provider: "grok"}), Evidence{})
	want := "no matching daemon start observed in the event journal since 2026-09-21T06:25:00Z (scope: starts that reached jevonsd; a start refused before it, e.g. by tool approval, or lost in transport is not visible here)"
	if r.Seats[0].Status != NotAttempted || r.Seats[0].Reason != want {
		t.Fatalf("reason = %q", r.Seats[0].Reason)
	}
	if strings.Contains(r.Line(), "no jevons_agent_start was made") {
		t.Fatal("line still claims no start was made")
	}
}

// Review finding 3: an observed outcome survives the journal tail rolling
// past it.
func TestObservedOutcomeOutlivesJournalTail(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), FileName))
	if err != nil {
		t.Fatal(err)
	}
	o, err := st.Declare(order(Seat{Name: "a", Provider: "grok"}, Seat{Name: "b", Provider: "grok"}), t0)
	if err != nil {
		t.Fatal(err)
	}
	first := Reconcile(o, Evidence{Attempts: []Attempt{
		{Name: "a", At: t0.Add(time.Minute), OK: true, Provider: "grok", Session: "s1", OrderID: "o-test"},
		{Name: "b", At: t0.Add(time.Minute), Err: "dest_saturated", OrderID: "o-test"},
	}})
	if err := st.Record([]Result{first}); err != nil {
		t.Fatal(err)
	}
	orders, err := st.Orders()
	if err != nil {
		t.Fatal(err)
	}
	// The journal has rolled: nothing read covers the window any more.
	later := Reconcile(orders[0], Evidence{CoveredSince: t0.Add(time.Hour)})
	got := statuses(later)
	if got["a"] != Minted || got["b"] != Refused {
		t.Fatalf("after tail roll: %v", got)
	}
	if later.Seats[0].Observed.Session != "s1" {
		t.Fatalf("session evidence lost: %+v", later.Seats[0].Observed)
	}
}

func TestAttemptsFromEvents(t *testing.T) {
	ts := t0.Format(time.RFC3339Nano)
	evs := []eventlog.Event{
		{TS: ts, Component: "agent_lifecycle", Decision: "start", Fields: map[string]any{"name": "a", "outcome": "ok", "provider": "claude", "target_id": "T1", "session_id": "s"}},
		{TS: ts, Component: "agent_lifecycle", Decision: "start", Fields: map[string]any{"name": "b", "outcome": "error", "err": "dest_saturated", "dest": "claude"}},
		{TS: ts, Component: "agent_lifecycle", Decision: "seat_stop", Fields: map[string]any{"name": "c"}},
		{TS: ts, Component: JournalComponent, Decision: JournalStart, Fields: map[string]any{"name": "d", "outcome": "ok", "order_id": "o-1"}},
		// A correlated event with no order id is dropped, not treated as plain.
		{TS: ts, Component: JournalComponent, Decision: JournalStart, Fields: map[string]any{"name": "e", "outcome": "ok"}},
	}
	got := AttemptsFromEvents(evs)
	if len(got) != 3 || got[2].Name != "d" || got[2].OrderID != "o-1" || got[0].OrderID != "" {
		t.Fatalf("attempts = %+v", got)
	}
	got = got[:2]
	if len(got) != 2 || !got[0].OK || got[0].Target != "T1" || got[0].Session != "s" || got[1].OK || got[1].Err != "dest_saturated" || got[1].Provider != "claude" {
		t.Fatalf("attempts = %+v", got)
	}
}

func TestParseSeats(t *testing.T) {
	seats, err := ParseSeats("jv-a:Grok:t759, jv-b:claude\njv-c")
	if err != nil {
		t.Fatal(err)
	}
	if len(seats) != 3 || seats[0] != (Seat{Name: "jv-a", Provider: "grok", Target: "T759"}) || seats[2].Provider != "" {
		t.Fatalf("seats = %+v", seats)
	}
	for _, bad := range []string{"", " , ", "a:claude, a:grok", ":grok"} {
		if _, err := ParseSeats(bad); err == nil {
			t.Errorf("ParseSeats(%q) accepted", bad)
		}
	}
}

func TestStoreRoundTripAndMalformed(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	o, err := st.Declare(Order{Parent: "jevons-po", Seats: []Seat{{Name: "a"}}}, t0)
	if err != nil || o.ID == "" {
		t.Fatalf("declare: %v %+v", err, o)
	}
	if _, err := st.Declare(Order{ID: o.ID, Parent: "jevons-po", Seats: []Seat{{Name: "b"}}}, t0); err == nil {
		t.Fatal("duplicate id accepted")
	}
	if _, err := st.Declare(Order{Seats: []Seat{{Name: "b"}}}, t0); err == nil {
		t.Fatal("order with no parent accepted")
	}
	if err := st.Close(o.ID, "re-ordered", t0); err != nil {
		t.Fatal(err)
	}
	got, err := st.Orders()
	if err != nil || len(got) != 1 || !got[0].Closed {
		t.Fatalf("orders = %+v %v", got, err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil {
		t.Fatal("malformed store opened: must be a hard error, never a silent reset")
	}
	if _, err := st.Orders(); err == nil {
		t.Fatal("malformed store read through an open handle as no orders")
	}
}

// Review finding 1: the daemon opens a Store per handler, so concurrent
// declarations, closures and records through separate Stores on one path
// must all land.
func TestConcurrentHandlersLoseNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	const n = 40
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			st, err := Open(path)
			if err != nil {
				t.Error(err)
				return
			}
			o, err := st.Declare(Order{ID: fmt.Sprintf("o-%02d", i), Parent: "p", IssuedAt: t0, Seats: []Seat{{Name: "a"}}}, t0)
			if err != nil {
				t.Error(err)
				return
			}
			if i%2 == 0 {
				other, err := Open(path)
				if err != nil {
					t.Error(err)
					return
				}
				if err := other.Close(o.ID, "done", t0); err != nil {
					t.Error(err)
				}
			}
		}(i)
	}
	wg.Wait()
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := st.Orders()
	if err != nil {
		t.Fatal(err)
	}
	closed := 0
	for _, o := range got {
		if o.Closed {
			closed++
		}
	}
	if len(got) != n || closed != n/2 {
		t.Fatalf("orders = %d (closed %d), want %d (closed %d)", len(got), closed, n, n/2)
	}
	leftovers, _ := filepath.Glob(path + ".*.tmp")
	if len(leftovers) != 0 {
		t.Fatalf("temp files left behind: %v", leftovers)
	}
}

// Review finding 4: at capacity, closed orders are evicted first and open
// ones are never silently dropped.
func TestCapacityNeverDropsOpenOrders(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), FileName))
	if err != nil {
		t.Fatal(err)
	}
	for i := range MaxOrders {
		if _, err := st.Declare(Order{ID: fmt.Sprintf("o-%03d", i), Parent: "p", IssuedAt: t0.Add(time.Duration(i) * time.Second), Seats: []Seat{{Name: "a"}}}, t0); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.Declare(Order{ID: "o-over", Parent: "p", Seats: []Seat{{Name: "a"}}}, t0); err == nil || !strings.Contains(err.Error(), "capacity") {
		t.Fatalf("declare at capacity with every order open: err = %v", err)
	}
	if err := st.Close("o-150", "done", t0); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Declare(Order{ID: "o-over", Parent: "p", Seats: []Seat{{Name: "a"}}}, t0); err != nil {
		t.Fatalf("declare after a close: %v", err)
	}
	got, err := st.Orders()
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, o := range got {
		ids[o.ID] = true
	}
	if len(got) != MaxOrders || ids["o-150"] || !ids["o-000"] || !ids["o-over"] {
		t.Fatalf("eviction took the wrong order: len=%d o-150=%v o-000=%v", len(got), ids["o-150"], ids["o-000"])
	}
}

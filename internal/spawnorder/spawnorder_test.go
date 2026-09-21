// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package spawnorder

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/eventlog"
)

var t0 = time.Date(2026, 9, 21, 6, 35, 0, 0, time.UTC)

func order(seats ...Seat) Order {
	return Order{ID: "o-test", Parent: "jevons-po", IssuedAt: t0, Seats: seats}
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
	attempts := []Attempt{
		{Name: "jv-t749", At: t0.Add(time.Minute), OK: true, Provider: "claude"},
		{Name: "jv-t760", At: t0.Add(2 * time.Minute), Err: "dest_saturated", Provider: "claude"},
		{Name: "jv-t755", At: t0.Add(3 * time.Minute), OK: true, Provider: "claude"},
		// An attempt well before the order is not this order's.
		{Name: "jv-t743", At: t0.Add(-time.Hour), OK: true, Provider: "grok"},
	}
	r := Reconcile(o, attempts, map[string]string{"jv-t749": "claude"})
	want := map[string]Status{
		"jv-t749": Minted, "jv-t759": NotAttempted, "jv-t760": Refused,
		"jv-t755": Rerouted, "jv-t743": NotAttempted,
	}
	if len(r.Seats) != len(want) {
		t.Fatalf("seats = %d, want %d", len(r.Seats), len(want))
	}
	for _, s := range r.Seats {
		if s.Status != want[s.Name] {
			t.Errorf("%s = %s (%s), want %s", s.Name, s.Status, s.Reason, want[s.Name])
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
	r := Reconcile(o, []Attempt{
		{Name: "a", At: t0.Add(time.Minute), OK: true, Provider: "claude"},
		{Name: "b", At: t0.Add(time.Minute), Err: "launch timed out"},
		{Name: "b", At: t0.Add(2 * time.Minute), OK: true, Provider: "grok"},
	}, nil)
	if !r.Complete() || r.Minted() != 2 {
		t.Fatalf("want complete 2/2: %s", r.Line())
	}
	if got := r.Line(); got != "order o-test: 2/2 minted (complete)" {
		t.Fatalf("line = %q", got)
	}
}

// A refused start whose seat is in the registry anyway (a retry past the
// journal tail) counts as minted: existence beats a stale error.
func TestRefusedButPresentIsMinted(t *testing.T) {
	o := order(Seat{Name: "a", Provider: "grok"})
	r := Reconcile(o, []Attempt{{Name: "a", At: t0.Add(time.Minute), Err: "x"}}, map[string]string{"a": "grok"})
	if r.Seats[0].Status != Minted {
		t.Fatalf("status = %s", r.Seats[0].Status)
	}
}

func TestAttemptsFromEvents(t *testing.T) {
	evs := []eventlog.Event{
		{TS: t0.Format(time.RFC3339Nano), Component: "agent_lifecycle", Decision: "start", Fields: map[string]any{"name": "a", "outcome": "ok", "provider": "claude"}},
		{TS: t0.Format(time.RFC3339Nano), Component: "agent_lifecycle", Decision: "start", Fields: map[string]any{"name": "b", "outcome": "error", "err": "dest_saturated", "dest": "claude"}},
		{TS: t0.Format(time.RFC3339Nano), Component: "agent_lifecycle", Decision: "seat_stop", Fields: map[string]any{"name": "c"}},
	}
	got := AttemptsFromEvents(evs)
	if len(got) != 2 || !got[0].OK || got[1].OK || got[1].Err != "dest_saturated" || got[1].Provider != "claude" {
		t.Fatalf("attempts = %+v", got)
	}
}

func TestParseSeats(t *testing.T) {
	seats, err := ParseSeats("jv-a:Grok:T759, jv-b:claude\njv-c")
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
}

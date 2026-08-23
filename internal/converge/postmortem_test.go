// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package converge

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

// capturingSink captures what the daemon would deliver as root.
type capturingSink struct {
	texts []string
	err   error
}

func (s *capturingSink) DeliverPostmortem(text string) error {
	if s.err != nil {
		return s.err
	}
	s.texts = append(s.texts, text)
	return nil
}

// incidentAt builds a closed episode the way the ladder hands one over.
func incidentAt(agent string, opened time.Time, dwell time.Duration, cause CloseCause, rungs ...Rung) Incident {
	return Incident{
		Agent:    agent,
		Mission:  "T319",
		Opened:   opened,
		Closed:   opened.Add(dwell),
		Dwell:    dwell,
		Rungs:    rungs,
		HumanLit: slices.Contains(rungs, RungHumanAlert),
		Cause:    cause,
	}
}

// Every closed incident is reported exactly once, however many times the
// converge loop re-records it (🎯T319 (5) coalesce).
func TestIncidentIsReportedExactlyOnce(t *testing.T) {
	j := NewPostmortemJournal()
	inc := incidentAt("jv-x", ladderT0, 46*time.Minute, ClosedBySatisfaction, RungRepressure, RungHumanAlert)

	if got := j.Record([]Incident{inc}); len(got) != 1 {
		t.Fatalf("first record: want 1 postmortem, got %d", len(got))
	}
	for i := range 3 {
		if got := j.Record([]Incident{inc}); len(got) != 0 {
			t.Fatalf("replay %d re-reported the incident: %+v", i, got)
		}
	}

	sink := &capturingSink{}
	if _, err := j.Flush(nil, sink); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if len(sink.texts) != 1 {
		t.Fatalf("want exactly one delivery, got %d", len(sink.texts))
	}
	if _, err := j.Flush([]Incident{inc}, sink); err != nil {
		t.Fatalf("second flush: %v", err)
	}
	if len(sink.texts) != 1 {
		t.Fatalf("incident redelivered after replay: %d deliveries", len(sink.texts))
	}
}

// The report says what was stuck, how long, which rungs fired, and how it
// cleared (🎯T319 (3)).
func TestPostmortemStatesStuckDwellRungsAndClearance(t *testing.T) {
	inc := incidentAt("jv-x", ladderT0, 46*time.Minute, ClosedBySatisfaction,
		RungRepressure, RungRepressure, RungOverseerNoise, RungHumanAlert)
	text := RenderPostmortem(inc)

	for _, want := range []string{
		"jv-x",                    // what was stuck
		"T319",                    // its mission
		"46m",                     // how long
		"repressure ×2",           // which rungs, coalesced
		"overseer-noise",          //
		"human-alert",             //
		"returned to working",     // how it cleared
		"withdrawn automatically", // and that the owner's alert came down
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("postmortem missing %q:\n%s", want, text)
		}
	}
}

// A postmortem is emitted whatever pathway ended the episode — a 🎯T316
// satisfaction verdict or the gap simply departing the set — and the two
// tell different stories (🎯T319 (3)).
func TestPostmortemFiresOnEveryResolutionPathway(t *testing.T) {
	for _, tc := range []struct {
		name  string
		cause CloseCause
		want  string
	}{
		{"satisfied", ClosedBySatisfaction, "returned to working"},
		{"departed", ClosedByDeparture, "closed or reaped"},
		{"provider_resume", ClosedByProviderResume, "provider began accepting"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			j := NewPostmortemJournal()
			sink := &capturingSink{}
			inc := incidentAt("jv-"+tc.name, ladderT0, 50*time.Minute, tc.cause, RungHumanAlert)
			if _, err := j.Flush([]Incident{inc}, sink); err != nil {
				t.Fatalf("flush: %v", err)
			}
			if len(sink.texts) != 1 {
				t.Fatalf("want one report, got %d", len(sink.texts))
			}
			if !strings.Contains(sink.texts[0], tc.want) {
				t.Fatalf("want %q in:\n%s", tc.want, sink.texts[0])
			}
		})
	}
}

// The postmortem is a report step, never satisfaction (🎯T319 (4)).
func TestPostmortemIsNotSatisfaction(t *testing.T) {
	j := NewPostmortemJournal()
	pms := j.Record([]Incident{incidentAt("jv-x", ladderT0, time.Hour, ClosedBySatisfaction, RungHumanAlert)})
	if len(pms) != 1 {
		t.Fatalf("want 1 postmortem, got %d", len(pms))
	}
	if pms[0].Satisfies {
		t.Fatal("postmortem reported itself as satisfaction")
	}
}

// An undelivered — even undeliverable — postmortem never holds owner-visible
// chrome up. The ladder clears on satisfaction alone (🎯T319 (1)(4)).
func TestPendingPostmortemDoesNotHoldTheStickyUp(t *testing.T) {
	l := NewLadder()
	l.Reconcile(ladderT0.Add(HumanAlertAfter), idle("jv-x"))

	at := ladderT0.Add(HumanAlertAfter + time.Minute)
	acts, closed := l.Reconcile(at, []Gap{{Agent: "jv-x", Mission: "T317", Since: ladderT0, Satisfied: true}})
	if len(acts) != 1 || acts[0].Kind != ActClearHuman {
		t.Fatalf("sticky not cleared on satisfaction: %+v", acts)
	}

	// Delivery is broken; the clear above already happened regardless.
	j := NewPostmortemJournal()
	if _, err := j.Flush(closed, &capturingSink{err: errors.New("root unreachable")}); err == nil {
		t.Fatal("want a delivery error")
	}
	if len(j.Pending()) != 1 {
		t.Fatalf("failed delivery lost the report: %+v", j.Pending())
	}
	if l.Tracked("jv-x") {
		t.Fatal("gap re-tracked by an undelivered postmortem")
	}
}

// A failed delivery is retried and still lands exactly once (🎯T319 (3)(5)).
func TestFailedDeliveryRetriesWithoutDuplicating(t *testing.T) {
	j := NewPostmortemJournal()
	inc := incidentAt("jv-x", ladderT0, time.Hour, ClosedBySatisfaction, RungHumanAlert)

	broken := &capturingSink{err: errors.New("root unreachable")}
	if _, err := j.Flush([]Incident{inc}, broken); err == nil {
		t.Fatal("want a delivery error")
	}

	good := &capturingSink{}
	if _, err := j.Flush([]Incident{inc}, good); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if len(good.texts) != 1 {
		t.Fatalf("want one delivery on retry, got %d", len(good.texts))
	}
	if len(j.Pending()) != 0 {
		t.Fatalf("delivered report still pending: %+v", j.Pending())
	}
}

// A burst of closures is one interruption, not several (🎯T319 (5)).
func TestFlushCoalescesABurstIntoOneDelivery(t *testing.T) {
	j := NewPostmortemJournal()
	sink := &capturingSink{}
	_, err := j.Flush([]Incident{
		incidentAt("jv-x", ladderT0, time.Hour, ClosedBySatisfaction, RungHumanAlert),
		incidentAt("jv-y", ladderT0, 20*time.Minute, ClosedByDeparture, RungRepressure),
	}, sink)
	if err != nil {
		t.Fatalf("flush: %v", err)
	}
	if len(sink.texts) != 1 {
		t.Fatalf("want one coalesced delivery, got %d", len(sink.texts))
	}
	for _, want := range []string{"2 impatience incidents closed", "jv-x", "jv-y"} {
		if !strings.Contains(sink.texts[0], want) {
			t.Fatalf("digest missing %q:\n%s", want, sink.texts[0])
		}
	}
}

// End to end on the ladder: a lit human alert plus true satisfaction yields
// both the clear and exactly one postmortem (🎯T319 (1)(5)).
func TestSatisfactionClearsNoiseAndReportsOnce(t *testing.T) {
	l := NewLadder()
	j := NewPostmortemJournal()
	sink := &capturingSink{}

	l.Reconcile(ladderT0.Add(RepressureAfter), idle("jv-x"))
	l.Reconcile(ladderT0.Add(HumanAlertAfter), idle("jv-x"))

	at := ladderT0.Add(HumanAlertAfter + time.Minute)
	acts, closed := l.Reconcile(at, []Gap{{Agent: "jv-x", Mission: "T317", Since: ladderT0, Satisfied: true}})
	if len(acts) != 1 || acts[0].Kind != ActClearHuman {
		t.Fatalf("want ActClearHuman, got %+v", acts)
	}
	if _, err := j.Flush(closed, sink); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if len(sink.texts) != 1 {
		t.Fatalf("want one postmortem, got %d", len(sink.texts))
	}
	if !strings.Contains(sink.texts[0], "no ack needed") {
		t.Fatalf("postmortem should record the auto-clear:\n%s", sink.texts[0])
	}

	// Subsequent quiet ticks add nothing.
	_, closed = l.Reconcile(at.Add(time.Hour), nil)
	if _, err := j.Flush(closed, sink); err != nil {
		t.Fatalf("quiet flush: %v", err)
	}
	if len(sink.texts) != 1 {
		t.Fatalf("quiet tick re-reported: %d deliveries", len(sink.texts))
	}
}

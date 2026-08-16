// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package rsi

import (
	"testing"
	"time"
)

// 🎯T428, the RSI half: a judgment the overseer has ALREADY ANSWERED is not
// asked again on the same evidence.
//
// The specimen: fingerprint 44c736552294ab9e02e8c3a01dcca666 was re-delivered
// to the overseer after it had recorded disposition=act_other for that exact
// fingerprint. Before this target only `file` and the achieved/set_aside
// outcomes suppressed anything, so `park`, `ignore_with_reason` and
// `act_other` were recorded and then ignored by the very loop that asked for
// them. Red on the pre-fix tree: Suppressions() returned no entry at all for
// any of the three, and the cycle re-proposed.

// t428DecidedDispositionsSuppress runs the whole loop the coach actually runs
// — record delivered → overseer decides → Suppressions() → RunCoachCycle —
// for each terminal disposition that used to change nothing.
func TestT428DecidedDispositionsSuppressRepropose(t *testing.T) {
	deliveredAt := time.Date(2026, 8, 10, 20, 0, 0, 0, time.UTC)
	decidedAt := deliveredAt.Add(2 * time.Hour)

	evidence := func(ts time.Time) []Evidence {
		var out []Evidence
		for range 3 {
			out = append(out, Evidence{
				Kind: "lifecycle_error", Component: "sentinel", Decision: "phase",
				Outcome: "error", Message: "idle-past-grace on a growing session",
				SourceID: "ca276b85", TS: ts,
			})
		}
		return out
	}

	// The fingerprint this cluster produces, discovered rather than assumed.
	base, err := RunCoachCycle(CoachCycleArgs{Evidence: evidence(deliveredAt), DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(base.Judgments) != 1 {
		t.Fatalf("baseline: want 1 judgment, got %d", len(base.Judgments))
	}
	fp := base.Judgments[0].Fingerprint

	for _, tc := range []struct {
		disposition string
		reason      string
		targetID    string
	}{
		{disposition: DispositionActOther},
		{disposition: DispositionPark},
		{disposition: DispositionIgnoreWithReason, reason: "known, tracked elsewhere"},
	} {
		t.Run(tc.disposition, func(t *testing.T) {
			s := dispositionFixtureStore(t)
			if err := s.RecordDelivered(base.Judgments, deliveredAt); err != nil {
				t.Fatal(err)
			}
			if _, err := s.SetDisposition(SetDispositionArgs{
				Fingerprint: fp,
				Disposition: tc.disposition,
				Reason:      tc.reason,
				TargetID:    tc.targetID,
				Now:         decidedAt,
			}); err != nil {
				t.Fatal(err)
			}

			sup, err := s.Suppressions(decidedAt)
			if err != nil {
				t.Fatal(err)
			}
			got, ok := sup[fp]
			if !ok {
				t.Fatalf("%s recorded and suppresses nothing — the judgment will be re-proposed", tc.disposition)
			}
			if !got.After.Equal(decidedAt) {
				t.Fatalf("%s suppression bar = %v, want the decision time %v", tc.disposition, got.After, decidedAt)
			}

			// Same evidence, next cycle: the question is not asked again.
			res, err := RunCoachCycle(CoachCycleArgs{
				Evidence: evidence(deliveredAt), OutcomeSuppressions: sup, DryRun: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(res.Judgments) != 0 {
				t.Fatalf("%s: judgment re-proposed on the evidence already answered: %+v",
					tc.disposition, res.Judgments)
			}

			// Newer than the decision is a changed situation, not a replay.
			res2, err := RunCoachCycle(CoachCycleArgs{
				Evidence: evidence(decidedAt.Add(24 * time.Hour)), OutcomeSuppressions: sup, DryRun: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(res2.Judgments) != 1 {
				t.Fatalf("%s: new evidence was swallowed — suppression must not be permanent: %+v",
					tc.disposition, res2.Skipped)
			}
		})
	}
}

// A judgment still awaiting an answer is not suppressed: pending means the
// overseer has not decided, and going quiet on it would lose the escalation.
func TestT428PendingDispositionDoesNotSuppress(t *testing.T) {
	s := dispositionFixtureStore(t)
	now := time.Date(2026, 8, 10, 20, 0, 0, 0, time.UTC)
	if err := s.RecordDelivered([]Judgment{{Fingerprint: "fp-pending", Name: "unanswered gap"}}, now); err != nil {
		t.Fatal(err)
	}
	sup, err := s.Suppressions(now)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := sup["fp-pending"]; ok {
		t.Fatal("a pending judgment was suppressed; only a DECIDED one is silent")
	}
}

// A decision written by a store that predates DispositionAt (or any entry
// whose decision time is missing) must still suppress. Falling through to a
// zero bar would suppress nothing, which is the defect this target closes.
func TestT428DecisionWithoutTimeFallsBackToDeliveryTime(t *testing.T) {
	s := dispositionFixtureStore(t)
	now := time.Date(2026, 8, 10, 20, 0, 0, 0, time.UTC)
	if err := s.RecordDelivered([]Judgment{{Fingerprint: "fp-old", Name: "gap"}}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetDisposition(SetDispositionArgs{
		Fingerprint: "fp-old", Disposition: DispositionActOther, Now: now.Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	entries, err := s.Entries()
	if err != nil {
		t.Fatal(err)
	}
	// Rewrite the entry as a pre-DispositionAt store would have left it.
	entries[0].DispositionAt = time.Time{}
	if err := s.save(entries); err != nil {
		t.Fatal(err)
	}

	sup, err := s.Suppressions(now)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := sup["fp-old"]
	if !ok {
		t.Fatal("a decided entry with no decision time suppressed nothing")
	}
	if !got.After.Equal(now) {
		t.Fatalf("fallback bar = %v, want the delivery time %v", got.After, now)
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package converge

import (
	"errors"
	"testing"
	"time"
)

func ownerVerdict(t *testing.T, vs []OwnerVerdict, dim OwnerDimension) OwnerVerdict {
	t.Helper()
	for _, v := range vs {
		if v.Dimension == dim {
			return v
		}
	}
	t.Fatalf("no verdict for dimension %q in %v", dim, vs)
	return OwnerVerdict{}
}

// healthyOwner is a plant where every dimension holds: the newest send is
// durable and acked, the reply sealed, chrome agrees with an idle server, and
// a client is connected and ticking.
func healthyOwner(now time.Time) OwnerObservation {
	return OwnerObservation{
		ClientsConnected: 1,
		SendID:           "s1",
		SendAt:           now.Add(-time.Minute),
		SendJournaled:    true,
		SendDelivered:    true,
		OwnerTurnAt:      now.Add(-time.Minute),
		ReplySealedAt:    now.Add(-30 * time.Second),
	}
}

// 🎯T355 acceptance 1: the three owner-visible dimensions are observed as
// levels, and a healthy plant satisfies all of them.
func TestClassifyOwnerHealthyPlantSatisfiesEveryDimension(t *testing.T) {
	now := time.Now()
	vs := ClassifyOwnerInteraction(healthyOwner(now), now, OwnerBounds{})
	if len(vs) != len(OwnerDimensions) {
		t.Fatalf("got %d verdicts, want one per dimension (%d)", len(vs), len(OwnerDimensions))
	}
	for i, v := range vs {
		if v.Dimension != OwnerDimensions[i] {
			t.Errorf("verdict %d dimension = %q, want %q", i, v.Dimension, OwnerDimensions[i])
		}
		if v.Condition != ConditionSatisfied {
			t.Errorf("%s = %s (%s), want satisfied", v.Dimension, v.Condition, v.Reason)
		}
	}
}

// Each owner-visible failure mode must classify as a gap of its own kind:
// one outage model, several classes (restart is not privileged).
func TestClassifyOwnerOutageClasses(t *testing.T) {
	now := time.Now()
	b := DefaultOwnerBounds()

	cases := []struct {
		name   string
		dim    OwnerDimension
		mutate func(o *OwnerObservation)
		kind   OwnerGapKind
		reason string
	}{
		{
			name: "ghost send never reaches the chatlog",
			dim:  OwnerDimSendLanded,
			mutate: func(o *OwnerObservation) {
				o.SendAt = now.Add(-b.SendLand - time.Second)
				o.SendJournaled = false
				o.SendDelivered = false
			},
			kind:   OwnerGapSendNotLanded,
			reason: "not_durable_in_chatlog",
		},
		{
			name: "durable but never acked by the overseer",
			dim:  OwnerDimSendLanded,
			mutate: func(o *OwnerObservation) {
				o.SendAt = now.Add(-b.SendLand - time.Second)
				o.SendJournaled = true
				o.SendDelivered = false
			},
			kind:   OwnerGapSendNotDelivered,
			reason: "not_acked_by_overseer",
		},
		{
			name: "spinner with nothing in flight",
			dim:  OwnerDimChromeTruthful,
			mutate: func(o *OwnerObservation) {
				o.ChromeWorking = true
				o.TurnInFlight = false
				o.ChromeMismatchSince = now.Add(-b.ChromeTruth - time.Second)
			},
			kind:   OwnerGapFalseWorking,
			reason: "working_chrome_without_turn",
		},
		{
			name: "silent work with idle chrome",
			dim:  OwnerDimChromeTruthful,
			mutate: func(o *OwnerObservation) {
				o.ChromeWorking = false
				o.TurnInFlight = true
				o.ChromeMismatchSince = now.Add(-b.ChromeTruth - time.Second)
			},
			kind:   OwnerGapFalseIdle,
			reason: "turn_in_flight_without_chrome",
		},
		{
			name: "prompt in flight with no provider progress",
			dim:  OwnerDimReplyOrResidual,
			mutate: func(o *OwnerObservation) {
				o.OwnerTurnAt = now.Add(-10 * time.Minute)
				o.ReplySealedAt = time.Time{}
				o.PromptInFlight = true
				o.SinceACPProgress = b.ACPStall + time.Second
			},
			kind:   OwnerGapACPStall,
			reason: "prompt_in_flight_no_progress",
		},
		{
			name: "owner turn evaporated (restart class)",
			dim:  OwnerDimReplyOrResidual,
			mutate: func(o *OwnerObservation) {
				o.OwnerTurnAt = now.Add(-b.Reply - time.Second)
				o.ReplySealedAt = time.Time{}
			},
			kind:   OwnerGapTurnStall,
			reason: "no_reply_no_residual",
		},
		{
			name: "owner turn still queued past the bound",
			dim:  OwnerDimReplyOrResidual,
			mutate: func(o *OwnerObservation) {
				o.OwnerTurnAt = now.Add(-b.Reply - time.Second)
				o.ReplySealedAt = time.Time{}
				o.QueueDepth = 2
			},
			kind:   OwnerGapTurnStall,
			reason: "queued_undelivered",
		},
		{
			name: "client main thread stopped ticking",
			dim:  OwnerDimInteractive,
			mutate: func(o *OwnerObservation) {
				o.SinceUIHeartbeat = b.UIHeartbeat + time.Second
			},
			kind:   OwnerGapUXDegraded,
			reason: "ui_heartbeat_stale",
		},
		{
			name: "composer cannot submit",
			dim:  OwnerDimInteractive,
			mutate: func(o *OwnerObservation) {
				o.ComposerBlocked = true
			},
			kind:   OwnerGapUXDegraded,
			reason: "composer_blocked",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := healthyOwner(now)
			tc.mutate(&o)
			v := ownerVerdict(t, ClassifyOwnerInteraction(o, now, b), tc.dim)
			if v.Condition != ConditionGap {
				t.Fatalf("%s = %s (%s), want gap", tc.dim, v.Condition, v.Reason)
			}
			if v.Kind != tc.kind {
				t.Errorf("kind = %q, want %q", v.Kind, tc.kind)
			}
			if v.Reason != tc.reason {
				t.Errorf("reason = %q, want %q", v.Reason, tc.reason)
			}
		})
	}
}

// The bounds are the contract: nothing is a gap before its bound elapses, so
// the ordinary send→drain race never fires an actuator.
func TestClassifyOwnerWithinBoundsIsSatisfied(t *testing.T) {
	now := time.Now()
	b := DefaultOwnerBounds()

	fresh := healthyOwner(now)
	fresh.SendAt = now.Add(-b.SendLand / 2)
	fresh.SendJournaled = false
	fresh.SendDelivered = false
	if v := ownerVerdict(t, ClassifyOwnerInteraction(fresh, now, b), OwnerDimSendLanded); v.Condition != ConditionSatisfied || v.Reason != "within_land_bound" {
		t.Errorf("fresh send = %s (%s), want satisfied within_land_bound", v.Condition, v.Reason)
	}

	racing := healthyOwner(now)
	racing.ChromeWorking = true
	racing.TurnInFlight = false
	racing.ChromeMismatchSince = now.Add(-b.ChromeTruth / 2)
	if v := ownerVerdict(t, ClassifyOwnerInteraction(racing, now, b), OwnerDimChromeTruthful); v.Condition != ConditionSatisfied || v.Reason != "mismatch_within_grace" {
		t.Errorf("racing chrome = %s (%s), want satisfied mismatch_within_grace", v.Condition, v.Reason)
	}

	// A long turn that keeps producing events is healthy however long it
	// runs: the reply bound governs silence, not duration.
	long := healthyOwner(now)
	long.OwnerTurnAt = now.Add(-2 * time.Hour)
	long.ReplySealedAt = time.Time{}
	long.PromptInFlight = true
	long.SinceACPProgress = time.Second
	if v := ownerVerdict(t, ClassifyOwnerInteraction(long, now, b), OwnerDimReplyOrResidual); v.Condition != ConditionSatisfied || v.Reason != "turn_progressing" {
		t.Errorf("long progressing turn = %s (%s), want satisfied turn_progressing", v.Condition, v.Reason)
	}
}

// A named residual is satisfaction; silence is not. This is the half that
// keeps recovery honest when nothing is recoverable.
func TestClassifyOwnerNamedResidualSatisfiesReply(t *testing.T) {
	now := time.Now()
	b := DefaultOwnerBounds()
	o := healthyOwner(now)
	o.OwnerTurnAt = now.Add(-10 * time.Minute)
	o.ReplySealedAt = time.Time{}

	if v := ownerVerdict(t, ClassifyOwnerInteraction(o, now, b), OwnerDimReplyOrResidual); v.Condition != ConditionGap {
		t.Fatalf("silence = %s (%s), want gap", v.Condition, v.Reason)
	}

	o.Residual = "cancelled_by_owner"
	o.ResidualAt = now.Add(-time.Minute)
	v := ownerVerdict(t, ClassifyOwnerInteraction(o, now, b), OwnerDimReplyOrResidual)
	if v.Condition != ConditionSatisfied || v.Reason != "residual:cancelled_by_owner" {
		t.Fatalf("named residual = %s (%s), want satisfied residual:cancelled_by_owner", v.Condition, v.Reason)
	}

	// A residual recorded *before* the turn says nothing about it.
	o.ResidualAt = o.OwnerTurnAt.Add(-time.Minute)
	if v := ownerVerdict(t, ClassifyOwnerInteraction(o, now, b), OwnerDimReplyOrResidual); v.Condition != ConditionGap {
		t.Errorf("stale residual = %s (%s), want gap", v.Condition, v.Reason)
	}
}

// Chrome and interaction are only wanted while someone is watching; scoping
// out is explicitly not satisfaction.
func TestClassifyOwnerNoClientScopesOutChromeAndInteraction(t *testing.T) {
	now := time.Now()
	o := healthyOwner(now)
	o.ClientsConnected = 0
	o.ChromeWorking = true
	o.ChromeMismatchSince = now.Add(-time.Hour)
	o.SinceUIHeartbeat = time.Hour
	vs := ClassifyOwnerInteraction(o, now, OwnerBounds{})
	for _, dim := range []OwnerDimension{OwnerDimChromeTruthful, OwnerDimInteractive} {
		if v := ownerVerdict(t, vs, dim); v.Condition != ConditionOutOfScope {
			t.Errorf("%s with no client = %s (%s), want out_of_scope", dim, v.Condition, v.Reason)
		}
	}
	// Durability still matters with nobody watching.
	if v := ownerVerdict(t, vs, OwnerDimSendLanded); v.Condition != ConditionSatisfied {
		t.Errorf("send-landed with no client = %s (%s), want satisfied", v.Condition, v.Reason)
	}
}

// 🎯T355 acceptance 3, the load-bearing invariant inherited from 🎯T316:
// steps never close the gap. Only an observation does.
func TestOwnerSetStepsNeverSatisfy(t *testing.T) {
	now := time.Now()
	s := NewOwnerSet()
	gap := OwnerVerdict{
		Dimension: OwnerDimChromeTruthful,
		Condition: ConditionGap,
		Kind:      OwnerGapFalseWorking,
		Reason:    "working_chrome_without_turn",
	}
	if out := s.Reconcile(gap, now); out.Resolution != ResolutionOpened {
		t.Fatalf("first gap = %s, want opened", out.Resolution)
	}

	for i := 0; i < 10; i++ {
		at := now.Add(time.Duration(i) * time.Minute)
		if _, ok := s.RecordStep(OwnerDimChromeTruthful, StepRecord{Kind: StepPublishLevelTruth, At: at, Delivered: true}); !ok {
			t.Fatalf("step %d not recorded", i)
		}
		if s.Len() != 1 {
			t.Fatalf("set emptied by a step after %d attempts", i+1)
		}
	}
	g, ok := s.Get(OwnerDimChromeTruthful)
	if !ok {
		t.Fatal("gap gone after steps")
	}
	if g.Satisfied() {
		t.Error("a standing gap reported itself satisfied")
	}
	if g.StepCount() != 10 {
		t.Errorf("StepCount = %d, want 10", g.StepCount())
	}

	// Only the observation empties the set.
	out := s.Reconcile(OwnerVerdict{
		Dimension: OwnerDimChromeTruthful,
		Condition: ConditionSatisfied,
		Reason:    "chrome_matches_level",
	}, now.Add(11*time.Minute))
	if out.Resolution != ResolutionSatisfied {
		t.Fatalf("satisfying observation = %s, want satisfied", out.Resolution)
	}
	if s.Len() != 0 {
		t.Fatalf("set still holds %d gaps after satisfaction", s.Len())
	}
	if out.Gap.StepCount() != 10 {
		t.Errorf("closed gap kept %d steps, want the whole episode (10)", out.Gap.StepCount())
	}
}

// Scoping out is withdrawal, not achievement, and a re-entered gap is a new
// episode so a flapping chrome is visible as flapping.
func TestOwnerSetWithdrawalAndEpisodes(t *testing.T) {
	now := time.Now()
	s := NewOwnerSet()
	gap := OwnerVerdict{Dimension: OwnerDimInteractive, Condition: ConditionGap, Kind: OwnerGapUXDegraded, Reason: "ui_heartbeat_stale"}
	s.Reconcile(gap, now)

	out := s.Reconcile(OwnerVerdict{Dimension: OwnerDimInteractive, Condition: ConditionOutOfScope, Reason: "no_client_connected"}, now.Add(time.Minute))
	if out.Resolution != ResolutionWithdrawn {
		t.Fatalf("scoped-out gap = %s, want withdrawn", out.Resolution)
	}

	again := s.Reconcile(gap, now.Add(2*time.Minute))
	if again.Resolution != ResolutionOpened {
		t.Fatalf("re-entered gap = %s, want opened", again.Resolution)
	}
	if again.Gap.Episode != 2 {
		t.Errorf("Episode = %d, want 2", again.Gap.Episode)
	}
}

// The actuator routing is the recovery half: each outage class gets the step
// that can actually close it, then escalates to noise rather than retrying
// forever.
func TestPlanOwnerStepRoutesThenEscalates(t *testing.T) {
	routes := map[OwnerGapKind]StepKind{
		OwnerGapSendNotLanded:    StepRequeueOwnerSend,
		OwnerGapSendNotDelivered: StepRequeueOwnerSend,
		OwnerGapTurnStall:        StepRequeueOwnerSend,
		OwnerGapFalseWorking:     StepPublishLevelTruth,
		OwnerGapFalseIdle:        StepPublishLevelTruth,
		OwnerGapACPStall:         StepACPUnstick,
		OwnerGapUXDegraded:       StepUXCoordinate,
	}
	for kind, want := range routes {
		g := OwnerGap{Kind: kind, StepsByKind: map[StepKind]int{}}
		if got := PlanOwnerStep(g); got != want {
			t.Errorf("PlanOwnerStep(%s) = %q, want %q", kind, got, want)
		}
		g.StepsByKind[want] = OwnerMaxPrimarySteps
		if got := PlanOwnerStep(g); got != StepOverseerNoise {
			t.Errorf("PlanOwnerStep(%s) after %d attempts = %q, want overseer noise",
				kind, OwnerMaxPrimarySteps, got)
		}
		g.StepsByKind[StepOverseerNoise] = 1
		if got := PlanOwnerStep(g); got != StepHumanAlert {
			t.Errorf("PlanOwnerStep(%s) after noise = %q, want human alert", kind, got)
		}
	}
}

// Drive spaces steps by the kind's bound, records what the actuator did, and
// skips a step whose precondition vanished without spending its budget.
func TestOwnerSetDriveSpacesRecordsAndSkips(t *testing.T) {
	now := time.Now()
	b := OwnerBounds{ChromeTruth: time.Minute}
	s := NewOwnerSet()
	s.Reconcile(OwnerVerdict{
		Dimension: OwnerDimChromeTruthful,
		Condition: ConditionGap,
		Kind:      OwnerGapFalseWorking,
		Reason:    "working_chrome_without_turn",
	}, now)

	var applied []StepKind
	act := OwnerActuatorFunc(func(g OwnerGap, kind StepKind, at time.Time) error {
		applied = append(applied, kind)
		return nil
	})

	if recs := s.Drive(now, OwnerDue(b), act); len(recs) != 1 || recs[0].Kind != StepPublishLevelTruth {
		t.Fatalf("first drive = %v, want one publish_level_truth", recs)
	}
	// Too soon: the bound has not elapsed.
	if recs := s.Drive(now.Add(10*time.Second), OwnerDue(b), act); len(recs) != 0 {
		t.Fatalf("drive inside the bound produced %v, want nothing", recs)
	}
	if recs := s.Drive(now.Add(2*time.Minute), OwnerDue(b), act); len(recs) != 1 {
		t.Fatalf("drive past the bound = %v, want one step", recs)
	}
	if len(applied) != 2 {
		t.Fatalf("actuator applied %d times, want 2", len(applied))
	}

	// A vanished precondition is not a failed attempt.
	before, _ := s.Get(OwnerDimChromeTruthful)
	skip := OwnerActuatorFunc(func(OwnerGap, StepKind, time.Time) error { return ErrOwnerStepNotApplicable })
	if recs := s.Drive(now.Add(10*time.Minute), OwnerDue(b), skip); len(recs) != 0 {
		t.Fatalf("not-applicable step recorded %v", recs)
	}
	after, _ := s.Get(OwnerDimChromeTruthful)
	if after.StepCount() != before.StepCount() {
		t.Errorf("step budget spent on a skipped step: %d → %d", before.StepCount(), after.StepCount())
	}

	// A failing actuator is recorded as an attempt, and the gap stands.
	fail := OwnerActuatorFunc(func(OwnerGap, StepKind, time.Time) error { return errors.New("no clients") })
	recs := s.Drive(now.Add(20*time.Minute), OwnerDue(b), fail)
	if len(recs) != 1 || recs[0].Delivered || recs[0].Err == "" {
		t.Fatalf("failed step = %v, want one undelivered record with an error", recs)
	}
	if s.Len() != 1 {
		t.Error("a failed step removed the gap")
	}
}

// Owner gaps ride the 🎯T317 ladder's view rather than a second engine.
func TestOwnerGapLadderView(t *testing.T) {
	now := time.Now()
	s := NewOwnerSet()
	s.Reconcile(OwnerVerdict{
		Dimension: OwnerDimReplyOrResidual,
		Condition: ConditionGap,
		Kind:      OwnerGapTurnStall,
		Reason:    "no_reply_no_residual",
	}, now)
	gaps := s.LadderGaps()
	if len(gaps) != 1 {
		t.Fatalf("LadderGaps = %v, want one", gaps)
	}
	if gaps[0].Agent != "owner" || gaps[0].Mission != string(OwnerDimReplyOrResidual) {
		t.Errorf("ladder view = %+v, want owner/%s", gaps[0], OwnerDimReplyOrResidual)
	}
	if gaps[0].Satisfied {
		t.Error("a standing gap projected as satisfied into the ladder")
	}
	if !gaps[0].Since.Equal(now) {
		t.Errorf("Since = %v, want the gap open time %v", gaps[0].Since, now)
	}
}

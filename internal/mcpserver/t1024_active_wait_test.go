// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
)

// 🎯T1024 specimens: the two real stored reports the daemon reaped on
// 2026-10-07 (arrai-po's incident), byte-for-byte from the report store.
//
//	arrai-t41-nest-quadratic  20261007T033601Z-05efb2ad  reaped 03:36:56Z
//	arrai-t41-verify-land     20261007T034225Z-0040e363  reaped 03:43:23Z
//
// Both are plain mid-progress narration of a worker queued on the shared
// heavy gate lease; neither carries a jevons envelope, a status tag, or a
// mission-level completion claim.

func t1024NestQuadraticReport(t *testing.T) string {
	t.Helper()
	return loadMentionReport(t, "t1024_arrai_t41_nest_quadratic_report.md", 2300)
}

func t1024VerifyLandReport(t *testing.T) string {
	t.Helper()
	return loadMentionReport(t, "t1024_arrai_t41_verify_land_report.md", 500)
}

// TestT1024ReportsAreTheIncidentShape pins that the fixtures still
// reproduce the incident, so the green below means the new guard is doing
// the work. Each report reads as a finish to the classifier that reaped it
// (hasFinishShape: completion word + oracle evidence), on the exact marker
// the lifecycle log recorded, and the 🎯T565 verb-shaped gate-wait veto
// that should have kept the seat is silent on both. If any of that lapses,
// the regression test is testing nothing.
func TestT1024ReportsAreTheIncidentShape(t *testing.T) {
	for _, tc := range []struct {
		name, report, wantMarker, wantSpan string
	}{
		{"nest-quadratic", t1024NestQuadraticReport(t), "finished", "50365 finished (another worker's gate)."},
		{"verify-land", t1024VerifyLandReport(t), "done", "The landing is done and verified;"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lower := strings.ToLower(strings.TrimSpace(tc.report))
			if !hasFinishShape(lower) {
				t.Fatal("fixture no longer reads as a finish shape — there was no reap for the guard to block")
			}
			marker, span, _, ok := FindCompletionClaim(tc.report)
			if !ok || marker != tc.wantMarker || span != tc.wantSpan {
				t.Fatalf("FindCompletionClaim = (%q, %q, %v), want (%q, %q) — the lifecycle log's own evidence", marker, span, ok, tc.wantMarker, tc.wantSpan)
			}
			if ClassifyReportAsk(tc.report) != AskNone || hasForwardLookingPlan(tc.report) {
				t.Fatal("fixture carries an ask or forward plan — an older veto would have kept it, not this one")
			}
			if declaresBlockingGateWaitLocalized(tc.report) {
				t.Fatal("🎯T565's verb-shaped veto hears this report; the 🎯T1024 gap is closed by something else")
			}
		})
	}
}

// TestT1024ActiveWaitIsNamedInTheReport is the classifier oracle: both real
// reports narrate an active wait on a named alive external process, and
// the sentence returned is the worker's own monitoring narration.
func TestT1024ActiveWaitIsNamedInTheReport(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, report, wantInSpan string
	}{
		{"nest-quadratic", t1024NestQuadraticReport(t), "queue"},
		{"verify-land", t1024VerifyLandReport(t), "alive and still queued"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			span, ok := FindActiveExternalWait(tc.report)
			if !ok {
				t.Fatal("FindActiveExternalWait did not recognise a worker queued on the shared gate lease")
			}
			if !strings.Contains(strings.ToLower(span), tc.wantInSpan) {
				t.Fatalf("span = %q, want it to name the wait (%q)", span, tc.wantInSpan)
			}
		})
	}
}

// TestT1024ActiveWaitIsNotAFinishReap runs both reports through the reap
// decision the daemon actually takes: the seat is kept, and the reason
// names the 🎯T972 scope scan's new classifier so a reader can disagree.
func TestT1024ActiveWaitIsNotAFinishReap(t *testing.T) {
	for _, tc := range []struct{ name, report string }{
		{"arrai-t41-nest-quadratic", t1024NestQuadraticReport(t)},
		{"arrai-t41-verify-land", t1024VerifyLandReport(t)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scope := OutstandingScopeReasons(tc.report, nil, false)
			if len(scope) != 1 || scope[0].Kind != activeWaitScopeKind {
				t.Fatalf("scope = %+v, want one %s entry", scope, activeWaitScopeKind)
			}
			reg := t439Registry(t, tc.name)
			ok, reason := ShouldAutoReapDoneWorkAgent(reg, tc.name, tc.report, nil)
			if ok {
				t.Fatalf("reaped a worker actively waiting on an alive gate (reason %s)", reason)
			}
			if want := outstandingScopeReapReasonPrefix + activeWaitScopeKind; reason != want {
				t.Fatalf("reason = %q, want %q", reason, want)
			}
		})
	}
}

// TestT1024ActiveWaitParksNotReaps covers the 🎯T985 half: the same report
// as a stop reason, or as the stored report behind a reason-less stop, is
// an external block that may resume — park, never a reap_stop.
func TestT1024ActiveWaitParksNotReaps(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, report string }{
		{"nest-quadratic", t1024NestQuadraticReport(t)},
		{"verify-land", t1024VerifyLandReport(t)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyWorkerIdleDisposition(tc.report); got != IdleActionPark {
				t.Fatalf("ClassifyWorkerIdleDisposition = %s, want %s", got, IdleActionPark)
			}
			d := ClassifyStopDisposition("", "", tc.report, false, false, false)
			if d.Action != IdleActionPark {
				t.Fatalf("reason-less stop over this stored report = %s (%s), want %s", d.Action, d.Why, IdleActionPark)
			}
		})
	}
}

// TestT1024SentinelDoesNotReadActiveWaitAsFinished covers the sentinel's
// separate path (🎯T410 finished_awaiting_gate): it consumed the same
// LooksLikeFinishedWorkReport verdict and would have filed a close-target
// against a worker still queued on the lease.
func TestT1024SentinelDoesNotReadActiveWaitAsFinished(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, report string }{
		{"nest-quadratic", t1024NestQuadraticReport(t)},
		{"verify-land", t1024VerifyLandReport(t)},
		{"T972 pending gate", t972PendingGateReport},
	} {
		if storedReportLooksFinished(tc.report) {
			t.Errorf("%s: sentinel reads a mid-wait report as finished", tc.name)
		}
	}
	if !storedReportLooksFinished(t972PlainFinishReport) {
		t.Error("a genuine finish with no outstanding scope must still read as finished to the sentinel")
	}
}

// TestT1024GenuineFinishStillReaps is the over-correction control: the
// guard narrows one false positive and must not widen the veto. A finish
// that recounts a wait it saw through, cites a running daemon as
// activation evidence, mentions a sibling's gate exiting, or reports its
// own delivery queued to the PO still reaps.
func TestT1024GenuineFinishStillReaps(t *testing.T) {
	t.Parallel()
	for i, tc := range []struct{ name, report string }{
		{"plain finish", t972PlainFinishReport},
		{"recounted wait, resolved in the same sentence",
			"Done. The slowpath gate was queued behind two siblings on the shared lease and came back green; go test -tags slowpath ./... passed. SHA abcdef0123456 landed on master."},
		{"running daemon as activation evidence",
			"Done. SHA abcdef0123456 committed; go test ./internal/mcpserver -run T1024 PASS. The development daemon is running abcdef0 after restart-jevonsd."},
		{"sibling PID exited",
			"50365 finished (another worker's gate). My own run passed: go test ./internal/mcpserver -run T1024 PASS. Done. SHA abcdef0123456."},
		{"delivery queued to the PO",
			"Done. SHA abcdef0123456; go test ./internal/mcpserver -run T1024 PASS. My finish report to jevons-po came back queued (1 pending) behind its in-flight turn."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if span, ok := FindActiveExternalWait(tc.report); ok {
				t.Fatalf("read a genuine finish as an active wait on %q", span)
			}
			if !LooksLikeFinishedWorkReport(tc.report) {
				t.Fatal("control fixture must read as a finish, or it tests nothing")
			}
			name := fmt.Sprintf("jv-t1024-control-%d", i) // never a -po suffix: that is a durable role
			reg := t439Registry(t, name)
			if ok, reason := ShouldAutoReapDoneWorkAgent(reg, name, tc.report, nil); !ok {
				t.Fatalf("a genuine finish must still reap (reason %s)", reason)
			}
			if got := ClassifyWorkerIdleDisposition(tc.report); got != IdleActionReap {
				t.Fatalf("ClassifyWorkerIdleDisposition = %s, want %s", got, IdleActionReap)
			}
		})
	}
}

// TestT1024QuotedWaitIsNotTheWorkersWait: 🎯T750 masking applies — a worker
// quoting someone else's "still queued" is not itself waiting.
func TestT1024QuotedWaitIsNotTheWorkersWait(t *testing.T) {
	t.Parallel()
	report := "Done. SHA abcdef0123456; go test ./internal/mcpserver -run T1024 PASS. The PO's note said \"my gate run is alive and still queued\" but that was a sibling's seat."
	if span, ok := FindActiveExternalWait(report); ok {
		t.Fatalf("quoted wait read as the worker's own: %q", span)
	}
}

// TestT1024MaybeReapKeepsSeatAndNotifiesParent is the end-to-end path the
// incident took (maybeReapDoneWorkAgent): the seat stays registered and the
// parent is told, with the worker's own sentence quoted.
func TestT1024MaybeReapKeepsSeatAndNotifiesParent(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []claudia.AgentDef{
		{Name: "arrai-po", WorkDir: dir, SessionID: "po", Purpose: claudia.PurposeWork, Parent: "jevons", Materialized: true, Provider: "grok"},
		{Name: "arrai-t41-verify-land", WorkDir: dir, SessionID: "w", Purpose: claudia.PurposeWork, Parent: "arrai-po", TargetID: "T41", Materialized: true, Provider: "grok"},
	} {
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}
	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	po := &fakeSender{alive: true}
	setObservedSenderResolver(s, func(name string) (agentSender, bool, error) {
		if name != "arrai-po" {
			t.Fatalf("unexpected fleet delivery to %q", name)
		}
		return po, false, nil
	})
	s.SetTurnWitness(witnessYielding(TurnEvidence{
		Observed: true, PayloadSeen: true,
		Detail: "transcript gained a user message carrying this payload",
	}))

	s.maybeReapDoneWorkAgent("arrai-t41-verify-land", t1024VerifyLandReport(t))

	if reg.Def("arrai-t41-verify-land") == nil {
		t.Fatal("a worker queued on the shared gate lease must not be reaped")
	}
	if len(po.sent) != 1 {
		t.Fatalf("PO deliveries = %d, want 1: %v", len(po.sent), po.sent)
	}
	got := po.sent[0]
	for _, want := range []string{"reap-scope 🎯T972", "arrai-t41-verify-land", "active wait", "alive and still queued"} {
		if !strings.Contains(got, want) {
			t.Errorf("PO notice missing %q:\n%s", want, got)
		}
	}
}

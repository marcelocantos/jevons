// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/gate"
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
//
// 🎯T766.4.1 re-encodes each with the typed active-wait slot, two ways:
//
//   - *_finish_active_wait.md — the incident shape as a declared
//     finish-report: oracle slots that would reap, the completion word in
//     the payload, and the wait declared beside them. Contradictory.
//   - *_status_ping_active_wait.md — the shape the fleet brief instructs:
//     status-ping, status in-progress, active-wait.
//
// The raw payload follows each fence unchanged.

func t1024NestQuadraticReport(t *testing.T) string {
	t.Helper()
	return loadMentionReport(t, "t1024_arrai_t41_nest_quadratic_report.md", 2300)
}

func t1024VerifyLandReport(t *testing.T) string {
	t.Helper()
	return loadMentionReport(t, "t1024_arrai_t41_verify_land_report.md", 500)
}

// t1024Specimen is one incident with its raw report and both typed
// re-encodings.
type t1024Specimen struct {
	name                  string
	raw, finish, ping     string
	wantClass, wantOnPart string
}

func t1024Specimens(t *testing.T) []t1024Specimen {
	t.Helper()
	return []t1024Specimen{
		{
			name:       "arrai-t41-nest-quadratic",
			raw:        t1024NestQuadraticReport(t),
			finish:     loadMentionReport(t, "t1024_arrai_t41_nest_quadratic_finish_active_wait.md", 2500),
			ping:       loadMentionReport(t, "t1024_arrai_t41_nest_quadratic_status_ping_active_wait.md", 2500),
			wantClass:  "gate",
			wantOnPart: "queued on the shared heavy lease",
		},
		{
			name:       "arrai-t41-verify-land",
			raw:        t1024VerifyLandReport(t),
			finish:     loadMentionReport(t, "t1024_arrai_t41_verify_land_finish_active_wait.md", 700),
			ping:       loadMentionReport(t, "t1024_arrai_t41_verify_land_status_ping_active_wait.md", 700),
			wantClass:  "lease",
			wantOnPart: "queued fifth on the shared heavy lease",
		},
	}
}

// t1024GateStore points the gate store at a hermetic directory holding the
// GREEN record the nest-quadratic finish encoding cites, so no host record
// and no missing record can decide these tests.
func t1024GateStore(t *testing.T) {
	t.Helper()
	root := t.TempDir()
	t.Setenv(gate.StoreDirEnv, root)
	store, err := gate.OpenStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(&gate.Record{
		ID: "b39d81c5", Name: "arrai-t41-race-timingsensitive",
		Command:    []string{"go", "test", "-race", "-tags", "timingsensitive", "./..."},
		ExitStatus: 0, StatusKnown: true, Verdict: gate.VerdictGreen,
		Started: time.Now().Add(-time.Minute), Ended: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
}

// TestT1024ReportsAreTheIncidentShape pins that the raw fixtures still
// carry the incident sentences, and that no prose reader hears them: they
// are not a finish shape, not a completion claim, not an ask, and not a
// typed wait (🎯T766.4).
func TestT1024ReportsAreTheIncidentShape(t *testing.T) {
	for _, tc := range []struct {
		name, report, wantMarker, wantSpan, wantWait string
	}{
		{"nest-quadratic", t1024NestQuadraticReport(t), "finished", "50365 finished (another worker's gate).", "Still alive and queued"},
		{"verify-land", t1024VerifyLandReport(t), "done", "The landing is done and verified;", "alive and still queued"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lower := strings.ToLower(strings.TrimSpace(tc.report))
			if hasFinishShape(lower) {
				t.Fatal("a completion sentence is a finish shape")
			}
			if _, _, _, ok := FindCompletionClaim(tc.report); ok {
				t.Fatal("a completion sentence is a completion claim")
			}
			if !strings.Contains(tc.report, tc.wantSpan) {
				t.Fatalf("fixture lost the incident sentence %q", tc.wantSpan)
			}
			if !strings.Contains(strings.ToLower(tc.report), tc.wantMarker) {
				t.Fatalf("fixture lost the incident word %q", tc.wantMarker)
			}
			if !strings.Contains(strings.ToLower(tc.report), strings.ToLower(tc.wantWait)) {
				t.Fatalf("fixture lost the wait narration %q", tc.wantWait)
			}
			if ClassifyReportAsk(tc.report) != AskNone || hasForwardLookingPlan(tc.report) {
				t.Fatal("fixture carries an ask or forward plan — an older veto would have kept it, not this one")
			}
			if DeclaresBlockingGateWait(tc.report) {
				t.Fatal("an unenveloped sentence was read as a typed wait")
			}
		})
	}
}

// TestT1024ActiveWaitIsNotAFinishReap runs the specimens through the reap
// decision. The raw reports declare nothing and stay unknown. Both typed
// encodings keep the seat: the finish encoding names outstanding scope, the
// status-ping keeps quietly. The control shows the slot, not the payload,
// decides: the same payload declared as a finish with no wait slot is a
// finish the worker declared.
func TestT1024ActiveWaitIsNotAFinishReap(t *testing.T) {
	t1024GateStore(t)
	for _, sp := range t1024Specimens(t) {
		t.Run(sp.name, func(t *testing.T) {
			reg := t439Registry(t, sp.name)
			if ok, reason := ShouldAutoReapDoneWorkAgent(reg, sp.name, sp.raw, nil); ok || reason != "unknown_report" {
				t.Fatalf("raw: reap=%v reason=%s, want unknown_report", ok, reason)
			}
			if ok, reason := ShouldAutoReapDoneWorkAgent(reg, sp.name, sp.finish, nil); ok || reason != "outstanding_scope_active_wait" {
				t.Fatalf("finish with active-wait: reap=%v reason=%s, want outstanding_scope_active_wait", ok, reason)
			}
			if ok, reason := ShouldAutoReapDoneWorkAgent(reg, sp.name, sp.ping, nil); ok || reason != "active_wait" {
				t.Fatalf("status-ping with active-wait: reap=%v reason=%s, want active_wait", ok, reason)
			}
			scope := OutstandingScopeReasons(sp.finish, nil, false)
			if len(scope) == 0 || scope[0].Kind != activeWaitScopeKind || !strings.Contains(scope[0].Detail, sp.wantOnPart) {
				t.Fatalf("finish scope = %+v, want the declared wait first", scope)
			}
			if scope := OutstandingScopeReasons(sp.raw, nil, false); len(scope) != 0 {
				t.Fatalf("raw scope = %+v, want none — narration is not scope", scope)
			}
			if ok, reason := ShouldAutoReapDoneWorkAgent(reg, sp.name, declaredFinishReport("T41", sp.raw), nil); !ok {
				t.Fatalf("control: a declared finish with no wait slot did not reap (reason %s)", reason)
			}
		})
	}
}

// TestT1024ActiveWaitParksNotReaps covers the 🎯T985 half: a declared wait
// as a stop reason, or as the stored report behind a reason-less stop, is
// an external wait that will resume — park, never a reap_stop. The raw
// narration is unclassifiable and parks only because a silent stop parks.
func TestT1024ActiveWaitParksNotReaps(t *testing.T) {
	t.Parallel()
	for _, sp := range t1024Specimens(t) {
		t.Run(sp.name, func(t *testing.T) {
			if got := ClassifyWorkerIdleDisposition(sp.raw); got != IdleActionKeep {
				t.Fatalf("raw ClassifyWorkerIdleDisposition = %s, want %s", got, IdleActionKeep)
			}
			for _, enc := range []struct{ name, text string }{{"finish", sp.finish}, {"status-ping", sp.ping}} {
				if got := ClassifyWorkerIdleDisposition(enc.text); got != IdleActionPark {
					t.Fatalf("%s ClassifyWorkerIdleDisposition = %s, want %s", enc.name, got, IdleActionPark)
				}
				if LooksLikeOwnWorkCompleteReport(enc.text) {
					t.Fatalf("%s read as own work complete", enc.name)
				}
				d := ClassifyStopDisposition("", "", enc.text, false, false, false)
				if d.Action != IdleActionPark || !strings.Contains(d.Why, "stored report") {
					t.Fatalf("%s reason-less stop = %s (%s), want park on the stored report", enc.name, d.Action, d.Why)
				}
				d = ClassifyStopDisposition("", enc.text, "", false, false, false)
				if d.Action != IdleActionPark || !strings.Contains(d.Why, "stop reason") {
					t.Fatalf("%s as stop reason = %s (%s), want park on the reason", enc.name, d.Action, d.Why)
				}
			}
			d := ClassifyStopDisposition("", "", sp.raw, false, false, false)
			if d.Action != IdleActionPark || !strings.Contains(d.Why, "nothing classifiable") {
				t.Fatalf("raw reason-less stop = %s (%s), want the unclassifiable park", d.Action, d.Why)
			}
			// The caller's declared disposition still decides.
			if d := ClassifyStopDisposition("reap", "", sp.finish, false, false, false); d.Action != IdleActionReap {
				t.Fatalf("disposition=reap = %s, want reap", d.Action)
			}
		})
	}
}

// TestT1024SentinelDoesNotReadActiveWaitAsFinished covers the sentinel's
// separate path (🎯T410 finished_awaiting_gate): it would have filed a
// close-target against a worker still queued on the lease.
func TestT1024SentinelDoesNotReadActiveWaitAsFinished(t *testing.T) {
	t.Parallel()
	for _, sp := range t1024Specimens(t) {
		for _, enc := range []struct{ name, text string }{{"raw", sp.raw}, {"finish", sp.finish}, {"status-ping", sp.ping}} {
			if storedReportLooksFinished(enc.text) {
				t.Errorf("%s %s: sentinel reads a mid-wait report as finished", sp.name, enc.name)
			}
		}
	}
	if storedReportLooksFinished(t972PendingGateReport) {
		t.Error("T972 pending gate: sentinel reads an unenveloped report as finished")
	}
	if !storedReportLooksFinished(declaredFinishReport("T972", t972PlainFinishReport)) {
		t.Error("a declared finish with no outstanding scope must still read as finished to the sentinel")
	}
}

// TestT1024IdleSeatWithDeclaredWaitIsNotNews: the idle and impatience
// sweeps hold off a seat whose last turn declares an active wait, and a
// declared wait is a report, not a plan-only turn.
func TestT1024IdleSeatWithDeclaredWaitIsNotNews(t *testing.T) {
	t.Parallel()
	for _, sp := range t1024Specimens(t) {
		tr := NewIdleActivityTracker()
		tr.NoteTerminalTurn(sp.name, sp.ping, 0)
		if tr.by[sp.name].PlanOnly {
			t.Errorf("%s: a declared wait was read as a plan-only turn", sp.name)
		}
		tr.NoteTerminalTurn(sp.name+"-raw", sp.raw, 0)
		if !tr.by[sp.name+"-raw"].PlanOnly {
			t.Errorf("%s: control — raw narration with no tool call should stay plan-only", sp.name)
		}
	}
}

// TestT1024GenuineFinishStillReaps is the over-correction control: a
// finish that recounts a wait it saw through, cites a running daemon,
// mentions a sibling's gate exiting, or reports its own delivery queued
// declares no wait slot and still reaps.
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
			done := declaredFinishReport("T1024", tc.report)
			if !LooksLikeFinishedWorkReport(done) {
				t.Fatal("control fixture must read as a finish, or it tests nothing")
			}
			name := fmt.Sprintf("jv-t1024-control-%d", i)
			reg := t439Registry(t, name)
			if ok, reason := ShouldAutoReapDoneWorkAgent(reg, name, done, nil); !ok {
				t.Fatalf("a genuine finish must still reap (reason %s)", reason)
			}
			if got := ClassifyWorkerIdleDisposition(done); got != IdleActionReap {
				t.Fatalf("ClassifyWorkerIdleDisposition = %s, want %s", got, IdleActionReap)
			}
		})
	}
}

// TestT1024MaybeReapKeepsSeatAndNotifiesParent drives the production reap
// caller (maybeReapDoneWorkAgent) with each encoding. The contradictory
// finish keeps the seat and tells the parent what it waits on; the
// status-ping and the raw narration keep it with no parent notice.
func TestT1024MaybeReapKeepsSeatAndNotifiesParent(t *testing.T) {
	t1024GateStore(t)
	for _, sp := range t1024Specimens(t) {
		for _, enc := range []struct {
			name       string
			text       string
			wantNotice bool
		}{{"finish", sp.finish, true}, {"status-ping", sp.ping, false}, {"raw", sp.raw, false}} {
			t.Run(sp.name+"/"+enc.name, func(t *testing.T) {
				dir := t.TempDir()
				reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
				if err != nil {
					t.Fatal(err)
				}
				for _, d := range []claudia.AgentDef{
					{Name: "arrai-po", WorkDir: dir, SessionID: "po", Purpose: claudia.PurposeWork, Parent: "jevons", Materialized: true, Provider: "grok"},
					{Name: sp.name, WorkDir: dir, SessionID: "w", Purpose: claudia.PurposeWork, Parent: "arrai-po", TargetID: "T41", Materialized: true, Provider: "grok"},
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

				s.maybeReapDoneWorkAgent(sp.name, enc.text)

				if reg.Def(sp.name) == nil {
					t.Fatal("a seat in an active wait was reaped")
				}
				po.mu.Lock()
				sent := append([]string(nil), po.sent...)
				po.mu.Unlock()
				if !enc.wantNotice {
					if len(sent) != 0 {
						t.Fatalf("PO deliveries = %d, want 0: %v", len(sent), sent)
					}
					return
				}
				if len(sent) != 1 {
					t.Fatalf("PO deliveries = %d, want 1 notice naming the wait: %v", len(sent), sent)
				}
				if !strings.Contains(sent[0], "[reap-scope 🎯T972]") || !strings.Contains(sent[0], "active wait on "+sp.wantClass) || !strings.Contains(sent[0], sp.wantOnPart) {
					t.Fatalf("parent notice does not name the declared wait:\n%s", sent[0])
				}
			})
		}
	}
}

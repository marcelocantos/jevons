// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package turnrate

import (
	"errors"
	"testing"
	"time"
)

const fixtureSession = "019fd13d-e500-7913-b96c-981e50aa2e99"

// compactOrRotate is the declined 100k lever. The shipped Admit never
// calls it. A mutation that uses it to cut burn must fail the
// session-identity assertion — that is the 🎯T392.1 oracle going RED.
func compactOrRotate(sessionID string) string {
	return "rotated-" + sessionID
}

func highBurn() Burn {
	return Burn{WorkerUSDPerHour: 7} // worker throttle is 5, pause is 10
}

func TestT392_1HighBurnDefersTurnWithoutRotatingSession(t *testing.T) {
	now := time.Date(2026, 8, 15, 16, 0, 0, 0, time.UTC)
	req := Request{
		Agent:     "jv-t392.1-turn-rate",
		SessionID: fixtureSession,
		LastTurn:  now.Add(-time.Second),
		Now:       now,
	}
	d := DefaultPolicy().Admit(req, highBurn())
	if !d.Deferred() || d.Verdict != VerdictDelay {
		t.Fatalf("high-burn snapshot verdict=%s want delay (%s)", d.Verdict, d.Reason)
	}
	if d.Wait <= 0 {
		t.Fatalf("delay carried no wait: %+v", d)
	}
	if d.SessionID != fixtureSession {
		t.Fatalf("session reminted: got %s want %s", d.SessionID, fixtureSession)
	}
}

func TestT392_1MutationCompactOrRotateGoesRed(t *testing.T) {
	now := time.Date(2026, 8, 15, 16, 0, 0, 0, time.UTC)
	req := Request{
		Agent:     "jv-t392.1-turn-rate",
		SessionID: fixtureSession,
		LastTurn:  now.Add(-time.Second),
		Now:       now,
	}
	real := DefaultPolicy().Admit(req, highBurn())
	if real.SessionID != fixtureSession {
		t.Fatalf("shipped path reminted: %s", real.SessionID)
	}
	if !real.Deferred() {
		t.Fatal("shipped path admitted a high-burn turn — not a rate limit")
	}

	// Mutation control: the declined lever. If compact-or-rotate is a
	// no-op, this oracle is dead. If Admit had used it, SessionID would
	// match the rotated id and the session-identity check would go RED.
	mutated := compactOrRotate(fixtureSession)
	if mutated == fixtureSession {
		t.Fatal("compact-or-rotate is a no-op — the 🎯T392.1 oracle is dead")
	}
	wrong := fakeSpendPathThatRotates(req, highBurn())
	if wrong.SessionID == fixtureSession {
		t.Fatal("mutation control did not rotate — oracle cannot go RED")
	}
	if real.SessionID == mutated || real.SessionID == wrong.SessionID {
		t.Fatal("shipped Admit compact-or-rotated a session to cut burn")
	}
}

func fakeSpendPathThatRotates(req Request, burn Burn) Decision {
	d := DefaultPolicy().Admit(req, burn)
	if d.Deferred() {
		d.SessionID = compactOrRotate(req.SessionID)
	}
	return d
}

func TestGovernorSpacesSubsequentTurnsUnderHighBurn(t *testing.T) {
	now := time.Date(2026, 8, 15, 16, 0, 0, 0, time.UTC)
	g := NewGovernor(GovernorArgs{
		Burn: func(string) Burn { return highBurn() },
		Now:  func() time.Time { return now },
	})
	if err := g.AllowTurn("w", fixtureSession); err != nil {
		t.Fatalf("first turn under throttle should run: %v", err)
	}
	if g.LastTurn("w") != now {
		t.Fatal("admitted turn did not record last-turn")
	}
	err := g.AllowTurn("w", fixtureSession)
	if !IsDeferred(err) {
		t.Fatalf("second turn under throttle was not spaced: %v", err)
	}
	var def *DeferredError
	if !asDeferred(err, &def) || def.Decision.SessionID != fixtureSession {
		t.Fatalf("spaced turn reminted or lost session: %v", err)
	}
	if def.Decision.Verdict != VerdictDelay {
		t.Fatalf("verdict=%s want delay", def.Decision.Verdict)
	}
}

func TestPauseAndRefuseLeaveSessionUnchanged(t *testing.T) {
	req := Request{Agent: "w", SessionID: fixtureSession}
	pause := DefaultPolicy().Admit(req, Burn{WorkerUSDPerHour: 12})
	if pause.Verdict != VerdictPause || pause.SessionID != fixtureSession {
		t.Fatalf("pause: %+v", pause)
	}
	refuse := DefaultPolicy().Admit(req, Burn{WorkerUSDPerHour: 25})
	if refuse.Verdict != VerdictRefuse || refuse.SessionID != fixtureSession {
		t.Fatalf("refuse: %+v", refuse)
	}
	fleetPause := DefaultPolicy().Admit(req, Burn{FleetUSDPerHour: 25})
	if fleetPause.Verdict != VerdictPause || fleetPause.SessionID != fixtureSession {
		t.Fatalf("fleet pause: %+v", fleetPause)
	}
}

func TestLowBurnAdmits(t *testing.T) {
	d := DefaultPolicy().Admit(Request{Agent: "w", SessionID: fixtureSession}, Burn{WorkerUSDPerHour: 1})
	if !d.Admitted() || d.SessionID != fixtureSession {
		t.Fatalf("low burn: %+v", d)
	}
}

func TestSubscriptionNeverDelaysOnUSD(t *testing.T) {
	d := DefaultPolicy().Admit(
		Request{Agent: "w", SessionID: fixtureSession, LastTurn: time.Now()},
		Burn{WorkerUSDPerHour: 99, Subscription: true},
	)
	if !d.Admitted() {
		t.Fatalf("subscription delayed on USD: %+v", d)
	}
	if d.SessionID != fixtureSession {
		t.Fatalf("subscription reminted: %s", d.SessionID)
	}
}

func TestOwnerTurnAlwaysAdmits(t *testing.T) {
	d := DefaultPolicy().Admit(
		Request{Agent: "jevons", SessionID: fixtureSession, OwnerTurn: true},
		Burn{WorkerUSDPerHour: 99},
	)
	if !d.Admitted() {
		t.Fatalf("owner turn deferred: %+v", d)
	}
}

func TestDisabledAdmits(t *testing.T) {
	p := DefaultPolicy()
	p.Disabled = true
	d := p.Admit(Request{Agent: "w", SessionID: fixtureSession}, highBurn())
	if !d.Admitted() {
		t.Fatalf("disabled: %+v", d)
	}
}

func asDeferred(err error, dest **DeferredError) bool {
	return errors.As(err, dest)
}

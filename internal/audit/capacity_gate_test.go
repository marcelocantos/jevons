// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package audit

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/capacity"
)

// stubGate returns one canned decision for every ask, and counts the asks.
// asked carries the first ask so a caller can rendezvous with it rather than
// poll against a deadline (🎯T400).
//
// skipRelease makes Begin return a no-op release so a control can prove
// waitForRelease fails when the product never frees the slot (🎯T462).
type stubGate struct {
	mu          sync.Mutex
	decision    capacity.Decision
	asks        []string
	released    int
	asked       chan struct{}
	skipRelease bool
}

func (g *stubGate) Begin(class, name string) (capacity.Decision, func()) {
	g.mu.Lock()
	g.asks = append(g.asks, class+":"+name)
	d := g.decision
	skip := g.skipRelease
	g.mu.Unlock()
	if g.asked != nil {
		select {
		case g.asked <- struct{}{}:
		default:
		}
	}
	d.Class, d.Name = capacity.NormalizeClass(class), name
	if skip {
		return d, func() {}
	}
	return d, func() {
		g.mu.Lock()
		g.released++
		g.mu.Unlock()
	}
}

func (g *stubGate) askCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.asks)
}

func (g *stubGate) releaseCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.released
}

func (g *stubGate) askedClass(class capacity.Class) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, a := range g.asks {
		if strings.HasPrefix(a, string(class)+":") {
			return true
		}
	}
	return false
}

// waitForRelease parks until the stub's release runs, or returns false when
// the deadline passes with released still zero. 🎯T462: the product path
// waits here after cancel instead of asserting releaseCount in the same
// breath as the ask rendezvous — Begin signals asked before it has even
// returned the release func, so an immediate assert races the auditor
// goroutine. The control (skipRelease) must still time out.
func waitForRelease(g *stubGate, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if g.releaseCount() > 0 {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return g.releaseCount() > 0
}

// fixtureReport is a well-formed auditor answer with one critical finding.
const fixtureReport = `{
  "summary": "One defect in the chat path.",
  "findings": [
    {
      "scope": "code",
      "path": "internal/server/chat.go",
      "line": 412,
      "severity": "critical",
      "title": "owner turn dropped when the send collides with a busy seat",
      "detail": "The busy branch returns without queueing, so the turn is lost."
    }
  ]
}`

// testAuditor builds an auditor over a fixture tree with a canned runner.
// The returned assignment channel carries each dispatched pass so a test can
// assert on the bounds the tier produced.
func testAuditor(t *testing.T, mut func(*Args)) (*Auditor, chan Assignment, string) {
	t.Helper()
	workdir, home := fixtureTree(t)
	state := t.TempDir()
	seen := make(chan Assignment, 8)
	args := Args{
		StateDir: state,
		Workdir:  workdir,
		Home:     home,
		Runner: RunnerFunc(func(_ context.Context, a Assignment) (RunOutput, error) {
			seen <- a
			return RunOutput{Raw: []byte(fixtureReport), Model: a.Model}, nil
		}),
	}
	if mut != nil {
		mut(&args)
	}
	a, err := New(args)
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	return a, seen, state
}

// 🎯T359: a scheduled pass that capacity defers must not dispatch, and the
// skip must be durable — a deferred audit cannot look like a clean one.
//
// The schedule is driven by an injected tick (🎯T400). The earlier shape —
// a 20ms interval polled against a 3s deadline — asserted two things at
// once: that a tick asks the gate, and that this machine gets round to it
// within three seconds. Only the first is the subject, and the second is
// false often enough under a loaded full-suite run to have made the standing
// gate unbelievable. Here the tick is handed over by hand and every wait is
// a blocking rendezvous, so the test has no wall-clock deadline left to lose.
//
// 🎯T462: after cancel, wait for the deferred release rather than reading
// releaseCount in the same breath as the ask. Begin signals asked before it
// returns the release func; runSafe's defer release() is already correct —
// only the oracle races. The control below proves waitForRelease is not a
// no-op: a stub that never frees the slot still fails the wait.
func TestScheduledCycleDefersUnderCapacityPressure(t *testing.T) {
	gate := &stubGate{
		decision: capacity.Decision{
			Verdict: capacity.VerdictDefer, Reason: capacity.ReasonCriticalOwnerOnly,
			Detail: "only owner and Build work fits", Pressure: capacity.PressureCritical,
		},
		asked: make(chan struct{}, 1),
	}
	ticks := make(chan time.Time)
	auditor, seen, state := testAuditor(t, func(a *Args) {
		a.Capacity = gate
		a.Ticks = ticks
		// Long enough that a real ticker could not fire during this test:
		// if the injected source were ignored, the rendezvous below would
		// hang rather than pass on a stray tick.
		a.Interval = time.Hour
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stopped := make(chan struct{})
	go func() { defer close(stopped); auditor.Run(ctx) }()

	ticks <- time.Now() // returns once the schedule has taken the tick
	<-gate.asked        // and once the tick has reached the capacity gate
	cancel()
	// Wait for the slot release before reading the stub. <-stopped alone is
	// not the subject here: the ask channel fires inside Begin, before the
	// release closure exists for runSafe to defer.
	if !waitForRelease(gate, 2*time.Second) {
		t.Fatal("a deferred ask must still release its slot")
	}
	<-stopped // no further asks can land while the assertions read the stub

	// One tick in, one ask out. Driving the cadence buys this: the polled
	// version could only ever assert "at least one", because it had no idea
	// how many ticks had gone by while it was waiting.
	if n := gate.askCount(); n != 1 {
		t.Fatalf("one tick must produce exactly one ask, got %d: %v", n, gate.asks)
	}
	if !gate.askedClass(capacity.ClassAudit) {
		t.Fatalf("audit must ask under its own capacity class: %v", gate.asks)
	}
	select {
	case a := <-seen:
		t.Fatalf("deferred cycle dispatched anyway: %+v", a.Reason)
	default:
	}

	st, err := LoadState(state)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(st.LastSkipReason, capacity.ReasonCriticalOwnerOnly) {
		t.Fatalf("deferral must be durable in state, got %q", st.LastSkipReason)
	}
}

// 🎯T462 control: waitForRelease must fail when the release is never called.
// If this ever goes green, the wait became a no-op and the product path's
// release assert would pass even when the slot leaked.
func TestWaitForReleaseFailsWhenReleaseNeverCalled(t *testing.T) {
	gate := &stubGate{
		decision: capacity.Decision{
			Verdict: capacity.VerdictDefer, Reason: capacity.ReasonCriticalOwnerOnly,
			Detail: "only owner and Build work fits", Pressure: capacity.PressureCritical,
		},
		skipRelease: true,
	}
	_, release := gate.Begin(string(capacity.ClassAudit), "control")
	release() // deliberately a no-op — skipRelease
	if waitForRelease(gate, 50*time.Millisecond) {
		t.Fatal("waitForRelease returned true when release was never called")
	}
	if gate.releaseCount() != 0 {
		t.Fatalf("control stub must leave released at 0, got %d", gate.releaseCount())
	}
}

// 🎯T359: an elevated-pressure pass still runs, at a reduced scope — degrade
// rather than drop, so the surfaces keep getting covered.
func TestScheduledCycleRunsReducedUnderElevatedPressure(t *testing.T) {
	gate := &stubGate{decision: capacity.Decision{
		Verdict: capacity.VerdictDegrade, Tier: capacity.TierReduced,
		Reason: capacity.ReasonElevatedDegrade, Pressure: capacity.PressureElevated,
	}}
	auditor, _, state := testAuditor(t, func(a *Args) { a.Capacity = gate })

	full, err := auditor.RunOnce(context.Background(), "test", true)
	if err != nil {
		t.Fatal(err)
	}
	if full.Tier != capacity.TierFull {
		t.Fatalf("a manual cycle runs at full tier, got %q", full.Tier)
	}

	reduced, err := auditor.cycle(context.Background(), "schedule", true, capacity.TierReduced)
	if err != nil {
		t.Fatal(err)
	}
	if reduced.Tier != capacity.TierReduced {
		t.Fatalf("reduced tier not recorded on the result: %q", reduced.Tier)
	}
	if reduced.Manifest.Bounds.MaxFilesPerScope >= full.Manifest.Bounds.MaxFilesPerScope {
		t.Fatalf("reduced pass must shrink the manifest bounds: full=%d reduced=%d",
			full.Manifest.Bounds.MaxFilesPerScope, reduced.Manifest.Bounds.MaxFilesPerScope)
	}
	// A degraded pass is still a real pass: every required surface stays in
	// scope, or the residue merge would wrongly resolve what it never saw.
	if !reduced.FullScan {
		t.Fatalf("degrading must not drop a surface, missing: %v", reduced.MissingScopes)
	}

	st, err := LoadState(state)
	if err != nil {
		t.Fatal(err)
	}
	if st.LastTier != capacity.TierReduced {
		t.Fatalf("state must record the tier the pass ran at, got %q", st.LastTier)
	}
	if st.LastSkipReason != "" {
		t.Fatalf("a dispatched pass clears the stale deferral note, got %q", st.LastSkipReason)
	}
}

// A nil gate is the pre-🎯T359 behaviour: every scheduled tick runs.
func TestNilCapacityGateRunsUngated(t *testing.T) {
	auditor, _, _ := testAuditor(t, nil)
	res, err := auditor.RunOnce(context.Background(), "test", true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped != "" {
		t.Fatalf("ungated cycle must run, skipped=%q", res.Skipped)
	}
	if len(res.Report.Findings) != 1 {
		t.Fatalf("fixture pass should carry its finding, got %d", len(res.Report.Findings))
	}
}

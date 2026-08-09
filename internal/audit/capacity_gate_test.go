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
type stubGate struct {
	mu       sync.Mutex
	decision capacity.Decision
	asks     []string
	released int
}

func (g *stubGate) Begin(class, name string) (capacity.Decision, func()) {
	g.mu.Lock()
	g.asks = append(g.asks, class+":"+name)
	d := g.decision
	g.mu.Unlock()
	d.Class, d.Name = capacity.NormalizeClass(class), name
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
func TestScheduledCycleDefersUnderCapacityPressure(t *testing.T) {
	gate := &stubGate{decision: capacity.Decision{
		Verdict: capacity.VerdictDefer, Reason: capacity.ReasonCriticalOwnerOnly,
		Detail: "only owner and Build work fits", Pressure: capacity.PressureCritical,
	}}
	auditor, seen, state := testAuditor(t, func(a *Args) {
		a.Capacity = gate
		a.Interval = 20 * time.Millisecond
	})

	ctx, cancel := context.WithCancel(context.Background())
	go auditor.Run(ctx)
	deadline := time.After(3 * time.Second)
	for gate.askCount() == 0 {
		select {
		case <-deadline:
			cancel()
			t.Fatal("capacity gate was never asked — the schedule ran ungated")
		case <-time.After(5 * time.Millisecond):
		}
	}
	cancel()

	if !gate.askedClass(capacity.ClassAudit) {
		t.Fatalf("audit must ask under its own capacity class: %v", gate.asks)
	}
	select {
	case a := <-seen:
		t.Fatalf("deferred cycle dispatched anyway: %+v", a.Reason)
	default:
	}
	if gate.releaseCount() == 0 {
		t.Fatal("a deferred ask must still release its slot")
	}

	st, err := LoadState(state)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(st.LastSkipReason, capacity.ReasonCriticalOwnerOnly) {
		t.Fatalf("deferral must be durable in state, got %q", st.LastSkipReason)
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

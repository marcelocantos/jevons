// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/agentreport"
	"github.com/marcelocantos/jevons/internal/converge"
	"github.com/marcelocantos/jevons/internal/notice"
)

// 🎯T938: a work seat whose stored finish-report declares it blocked on the
// owner is not re-pressured. On 2026-09-30 jv-t935-broker-auto-return waited
// on an owner go-ahead for a daemon restart and received
// impatience_ladder_repressure nudges every few minutes.

const (
	t938Blocked = "jv-t938-blocked"
	t938Control = "jv-t938-control"
	t938PO      = "jevons-po"
	// The incident's own spelling: status blocked_design, blocker named.
	t938BlockedReport = "```jevons\njevons: kind finish-report\njevons: target T935\njevons: status blocked_design\njevons: blocker daemon-restart-needs-owner-go-ahead\njevons: silent-ledger none\n```\n" +
		"Broker auto-return is implemented and committed; activating it needs a daemon restart, which needs the owner's go-ahead."
	t938Sweeps = 12
)

// t938Fixture registers two open-mission workers with the same idle shape.
// Only t938Blocked has a stored report — the blocked finish-report.
func t938Fixture(t *testing.T, now time.Time) (*Server, *IdleActivityTracker, string) {
	t.Helper()
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []claudia.AgentDef{
		{Name: "jevons", WorkDir: dir, SessionID: "s-o", Purpose: claudia.PurposeOverseer,
			Materialized: true, Provider: "grok", AutoStart: true},
		{Name: t938Blocked, WorkDir: dir, SessionID: "s-b", Purpose: claudia.PurposeWork,
			Parent: t938PO, Materialized: true, Provider: "grok", AutoStart: true, TargetID: "T935"},
		{Name: t938Control, WorkDir: dir, SessionID: "s-c", Purpose: claudia.PurposeWork,
			Parent: t938PO, Materialized: true, Provider: "grok", AutoStart: true, TargetID: "T936"},
	} {
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}
	ledger, err := OpenIdleNudgeLedger(dir)
	if err != nil {
		t.Fatal(err)
	}
	activity := NewIdleActivityTracker()
	for _, n := range []string{t938Blocked, t938Control} {
		activity.by[n] = IdleActivity{Phase: "idle", Updated: now.Add(-time.Hour)}
	}
	s := &Server{registry: reg, idleActivity: activity, idleNudgeLedger: ledger}
	s.SetAgentReportDir(dir)
	if h := s.storeAgentReport(t938Blocked, t938BlockedReport); h.Empty() {
		t.Fatal("blocked report was not stored")
	}
	s.SetIdlePressureHooks(IdlePressureHooks{
		MissionOpen:    func(string) bool { return true },
		LooksSatisfied: NewReportStoreLooksSatisfied(dir),
	})
	return s, activity, dir
}

func t938Running(name string) bool { return name == t938Blocked || name == t938Control }

// t938Sweep runs n T315 sweeps, each past every backoff, keeping both seats
// idle, and counts deliveries per seat.
func t938Sweep(s *Server, activity *IdleActivityTracker, from time.Time, n int) (map[string]int, time.Time) {
	pushes := map[string]int{}
	push := func(target, event, text string) error { pushes[target]++; return nil }
	now := from
	for i := 0; i < n; i++ {
		now = now.Add(20 * time.Minute) // past the longest DefaultIdleNudgeBackoffs step
		for _, name := range []string{t938Blocked, t938Control} {
			activity.by[name] = IdleActivity{Phase: "idle", Updated: now.Add(-time.Hour)}
		}
		s.idlePressureSweep(idlePressureDeps{Now: now, Running: t938Running, Push: push, SessionPhase: idlePhase})
	}
	return pushes, now
}

// Acceptance 4: a stored blocked finish-report yields zero idle-nudge sends
// over N sweeps; the unblocked seat with the same idle shape still gets
// nudged. Acceptance 3: an explicit clear ends the hold and pressure resumes.
func TestT938BlockedSeatGetsZeroIdleNudgesOverSweeps(t *testing.T) {
	t.Parallel()
	start := time.Unix(50_000, 0)
	s, activity, _ := t938Fixture(t, start)

	pushes, now := t938Sweep(s, activity, start, t938Sweeps)
	if pushes[t938Blocked] != 0 {
		t.Fatalf("blocked seat got %d idle nudges over %d sweeps, want 0", pushes[t938Blocked], t938Sweeps)
	}
	if pushes[t938Control] == 0 {
		t.Fatalf("control seat with the same idle shape got no nudge over %d sweeps — the fixture proves nothing", t938Sweeps)
	}

	// The classifier names why: blocked, not idle-open-mission.
	reps := s.idlePressureSweep(idlePressureDeps{Now: now.Add(20 * time.Minute), Running: t938Running,
		Push: func(string, string, string) error { return nil }, SessionPhase: idlePhase})
	if r := reportFor(reps, t938Blocked); r.Action != IdleNudgeSkip || r.Reason != IdleSkipBlockedOnOwner {
		t.Fatalf("blocked seat report %+v, want skip/%s", r, IdleSkipBlockedOnOwner)
	}

	// Acceptance 3: the blocker is marked cleared → pressure resumes.
	if !s.ClearSeatBlocker(t938Blocked, "test") {
		t.Fatal("ClearSeatBlocker found no blocked report to clear")
	}
	after, _ := t938Sweep(s, activity, now.Add(time.Hour), 2)
	if after[t938Blocked] == 0 {
		t.Fatal("cleared seat still got no idle nudge — pressure did not resume")
	}
}

// The impatience ladder (🎯T317) — the path that actually sent the
// incident's impatience_ladder_repressure nudges — holds off too.
func TestT938ImpatienceLadderSkipsBlockedSeat(t *testing.T) {
	t.Parallel()
	start := time.Unix(60_000, 0)
	s, activity, _ := t938Fixture(t, start)
	rep := &recordingRepressure{}
	eng := NewImpatienceEngine(ImpatienceEngineArgs{
		Sinks: converge.Sinks{RePressure: rep, Overseer: &recordingOverseer{}, Human: &recordingHuman{}},
	})
	s.SetImpatienceEngine(eng)

	now := start
	for i := 0; i < t938Sweeps; i++ {
		for _, name := range []string{t938Blocked, t938Control} {
			activity.by[name] = IdleActivity{Phase: "idle", Updated: start}
		}
		s.idlePressureSweep(idlePressureDeps{Now: now, Running: t938Running})
		now = now.Add(converge.RepressureAfter + time.Minute)
	}
	var blocked, control int
	for _, a := range rep.agents {
		switch {
		case strings.HasPrefix(a, t938Blocked+"/"):
			blocked++
		case strings.HasPrefix(a, t938Control+"/"):
			control++
		}
	}
	if blocked != 0 {
		t.Fatalf("ladder re-pressured the blocked seat %d times: %v", blocked, rep.agents)
	}
	if control == 0 {
		t.Fatalf("ladder never re-pressured the control seat: %v", rep.agents)
	}
	eng.mu.Lock()
	tracked := eng.ladder.Tracked(t938Blocked)
	eng.mu.Unlock()
	if tracked {
		t.Fatal("blocked seat is still an open ladder incident")
	}
}

// Acceptance 3: an owner message or an ancestor's direct ends the blocked
// state; daemon-composed traffic, peers and the seat itself do not. A newer
// stored report — even a byte-identical blocked one — blocks again.
func TestT938OwnerOrParentMessageClearsBlocker(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fleet := map[string]*fakeSender{
		t938Blocked: {alive: true},
		t938PO:      {alive: true},
		"jv-peer":   {alive: true},
	}
	s, _ := chainServer(t, fleet)
	s.registry = newLineageRegistry(t, map[string]string{
		t938PO:      "",
		t938Blocked: t938PO,
		"jv-peer":   "",
	})
	s.SetAgentReportDir(dir)
	s.SetIdlePressureHooks(IdlePressureHooks{LooksSatisfied: NewReportStoreLooksSatisfied(dir)})
	s.storeAgentReport(t938Blocked, t938BlockedReport)

	blocked := func() string {
		_, _, b := s.storedReportFor(t938Blocked, s.latestStoredReport(t938Blocked))
		return b
	}
	if blocked() != "daemon-restart-needs-owner-go-ahead" {
		t.Fatalf("stored report not read as blocked: %q", blocked())
	}

	// None of these answer the blocker.
	if _, err := s.sendToAgent(t938Blocked, "daemon note: fleet health tick", false); err != nil {
		t.Fatalf("daemon send: %v", err)
	}
	if _, err := s.sendToAgentAs("jv-peer", t938Blocked, "peer chatter", false); err != nil {
		t.Fatalf("peer send: %v", err)
	}
	if blocked() == "" {
		t.Fatal("daemon or peer traffic cleared the blocker")
	}

	// The parent directing down answers it.
	if _, err := s.sendToAgentAs(t938PO, t938Blocked, "Owner said go: restart is scheduled, carry on.", false); err != nil {
		t.Fatalf("parent send: %v", err)
	}
	if b := blocked(); b != "" {
		t.Fatalf("parent direct did not clear the blocker: %q", b)
	}

	// The seat reports blocked again (identical bytes): blocked again.
	s.storeAgentReport(t938Blocked, t938BlockedReport)
	if blocked() == "" {
		t.Fatal("a fresh blocked report after a clear did not block again")
	}

	// The owner speaking answers it too.
	if _, err := s.deliverByName(t938Blocked, "Go ahead with the restart.", OriginOwner, false); err != nil {
		t.Fatalf("owner send: %v", err)
	}
	if b := blocked(); b != "" {
		t.Fatalf("owner message did not clear the blocker: %q", b)
	}
}

// Acceptance 2: the parent holds the blocker — the blocked report is not
// finished work (never reaped), and the parent's inbox records the blocker.
func TestT938BlockedReportIsNotReapedAndParentHoldsBlocker(t *testing.T) {
	t.Parallel()
	if LooksLikeFinishedWorkReport(t938BlockedReport) {
		t.Fatal("blocked finish-report classified as finished work — T165 would reap a waiting seat")
	}
	reg := newLineageRegistry(t, map[string]string{t938PO: "", t938Blocked: t938PO})
	if ok, reason := ShouldAutoReapDoneWorkAgent(reg, t938Blocked, t938BlockedReport, nil); ok || reason != IdleSkipBlockedOnOwner {
		t.Fatalf("reap decision = %v/%s, want false/%s", ok, reason, IdleSkipBlockedOnOwner)
	}
	n, ok := notice.FromReport(t938Blocked, t938PO, t938BlockedReport, time.Unix(1, 0))
	if !ok || n.Outcome != notice.OutcomeBlocked || n.Blocker != "daemon-restart-needs-owner-go-ahead" {
		t.Fatalf("parent notice = %+v ok=%v, want outcome blocked with the blocker named", n, ok)
	}

	// Worker-idle to the parent is suppressed while it holds the blocker.
	dir := t.TempDir()
	s := &Server{registry: reg}
	s.SetAgentReportDir(dir)
	if _, err := agentreport.Save(dir, t938Blocked, t938BlockedReport, time.Unix(2, 0)); err != nil {
		t.Fatal(err)
	}
	if sup, why := s.workerIdleSuppressReason(t938Blocked); !sup || why != IdleSkipBlockedOnOwner {
		t.Fatalf("worker-idle suppress = %v/%s, want true/%s", sup, why, IdleSkipBlockedOnOwner)
	}
	s.ClearSeatBlocker(t938Blocked, "test")
	if sup, why := s.workerIdleSuppressReason(t938Blocked); sup {
		t.Fatalf("worker-idle still suppressed after clear (%s)", why)
	}
}

func TestT938ClassifiersSkipBlocked(t *testing.T) {
	t.Parallel()
	o := t761IdleBase()
	o.BlockedOnOwner = "owner go-ahead"
	if act, reason := ClassifyIdleNudge(o); act != IdleNudgeSkip || reason != IdleSkipBlockedOnOwner {
		t.Fatalf("idle nudge = %s/%s", act, reason)
	}
	o.BlockedOnOwner = ""
	if act, _ := ClassifyIdleNudge(o); act != IdleNudgeNudge {
		t.Fatalf("control idle nudge = %s, want nudge", act)
	}
	r := t761RecoverBase()
	r.BlockedOnOwner = "owner go-ahead"
	if act, reason := ClassifyFleetRecover(r); act != FleetRecoverSkip || reason != IdleSkipBlockedOnOwner {
		t.Fatalf("fleet recover = %s/%s", act, reason)
	}
	if !clearsSeatBlocker(OriginOwner, RelationOwnerSurface, "hi") ||
		!clearsSeatBlocker(OriginAgent, RelationDirectDown, "go") ||
		clearsSeatBlocker(OriginAgent, RelationOwnerSurface, "daemon note") ||
		clearsSeatBlocker(OriginAgent, RelationPeer, "hi") ||
		clearsSeatBlocker(OriginAgent, RelationSelf, "hi") {
		t.Fatal("clearsSeatBlocker relation table wrong")
	}
}

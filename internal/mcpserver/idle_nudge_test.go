// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/turnev"
)

func TestClassifyIdleNudgeSkips(t *testing.T) {
	t.Parallel()
	base := IdleNudgeObs{
		Name:           "w",
		Purpose:        claudia.PurposeWork,
		ProcessRunning: true,
		Phase:          "idle",
		IdleFor:        10 * time.Minute,
		HasOpenMission: true,
	}
	cases := []struct {
		name   string
		mut    func(*IdleNudgeObs)
		action IdleNudgeAction
		reason string
	}{
		{"aside", func(o *IdleNudgeObs) { o.Purpose = claudia.PurposeAside }, IdleNudgeSkip, "not_work_purpose"},
		{"overseer", func(o *IdleNudgeObs) { o.Purpose = claudia.PurposeOverseer }, IdleNudgeSkip, "not_work_purpose"},
		{"finished", func(o *IdleNudgeObs) { o.LooksFinished = true }, IdleNudgeSkip, "achieved_should_reap"},
		{"design", func(o *IdleNudgeObs) { o.DesignGated = true }, IdleNudgeSkip, "design_gated"},
		{"deliberate_stop", func(o *IdleNudgeObs) {
			o.DeliberateStop = true
			o.ProcessRunning = false
		}, IdleNudgeSkip, "deliberate_stop"},
		{"no_mission", func(o *IdleNudgeObs) { o.HasOpenMission = false }, IdleNudgeSkip, "no_open_mission"},
		{"not_running", func(o *IdleNudgeObs) { o.ProcessRunning = false }, IdleNudgeSkip, "not_running"},
		{"working", func(o *IdleNudgeObs) { o.Phase = "working" }, IdleNudgeSkip, "in_progress"},
		{"below_threshold", func(o *IdleNudgeObs) { o.IdleFor = time.Minute }, IdleNudgeSkip, "idle_below_threshold"},
		{"maxed", func(o *IdleNudgeObs) { o.NudgeCount = DefaultIdleNudgeMax }, IdleNudgeMaxed, "max_nudges"},
		{"backoff", func(o *IdleNudgeObs) {
			o.EverNudged = true
			o.NudgeCount = 1
			o.SinceLastNudge = time.Minute
		}, IdleNudgeSkip, "backoff"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := base
			tc.mut(&o)
			act, reason := ClassifyIdleNudge(o)
			if act != tc.action || reason != tc.reason {
				t.Fatalf("got %s/%s want %s/%s", act, reason, tc.action, tc.reason)
			}
		})
	}
}

func TestT423SweepUsesDecoderNotACPIdle(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jv-t423", WorkDir: dir, SessionID: "s1",
		Purpose: claudia.PurposeWork, Materialized: true, AutoStart: true, TargetID: "T423",
	}); err != nil {
		t.Fatal(err)
	}
	activity := NewIdleActivityTracker()
	now := time.Unix(5000, 0)
	activity.by = map[string]IdleActivity{
		"jv-t423": {Phase: "idle", Updated: now.Add(-10 * time.Minute)},
	}
	var pushed int
	reps := SweepIdleNudges(IdleNudgeSweepArgs{
		Reg: reg, Activity: activity, Now: now, OverseerName: "jevons",
		SessionPhase: func(claudia.AgentDef) turnev.Phase { return turnev.PhaseWorking },
		Push: func(target, event, text string) error {
			pushed++
			return nil
		},
		ProcessRunning: func(string) bool { return true },
	})
	if pushed != 0 {
		t.Fatalf("decoder working still pushed %d; reps=%+v", pushed, reps)
	}
}

func TestT423SystemicCapDoesNotPush(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		name := "jv-sys-" + string(rune('a'+i))
		if err := reg.Register(claudia.AgentDef{
			Name: name, WorkDir: dir, SessionID: "s" + name,
			Purpose: claudia.PurposeWork, Materialized: true, AutoStart: true, TargetID: "T1",
		}); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Unix(6000, 0)
	activity := NewIdleActivityTracker()
	for _, d := range reg.List() {
		activity.by[d.Name] = IdleActivity{Phase: "idle", Updated: now.Add(-10 * time.Minute)}
	}
	var pushed int
	reps := SweepIdleNudges(IdleNudgeSweepArgs{
		Reg: reg, Activity: activity, Now: now, OverseerName: "jevons",
		SessionPhase: func(claudia.AgentDef) turnev.Phase { return turnev.PhaseIdle },
		Push: func(target, event, text string) error {
			pushed++
			return nil
		},
		ProcessRunning: func(string) bool { return true },
	})
	if pushed != 0 {
		t.Fatalf("systemic pass pushed %d — cap must precede Push; reps=%+v", pushed, reps)
	}
	for _, r := range reps {
		if r.Reason != "systemic_read" {
			t.Fatalf("%s reason=%s want systemic_read", r.Name, r.Reason)
		}
		if r.Delivered {
			t.Fatalf("%s delivered after cap", r.Name)
		}
	}
}

func TestT423EmptyPhaseIsNotIdle(t *testing.T) {
	t.Parallel()
	o := IdleNudgeObs{
		Name: "w", Purpose: claudia.PurposeWork, ProcessRunning: true,
		Phase: "", IdleFor: 10 * time.Minute, HasOpenMission: true,
	}
	act, reason := ClassifyIdleNudge(o)
	if act != IdleNudgeSkip || reason != "phase_unknown" {
		t.Fatalf("empty phase: %s/%s — unknown must not nudge (🎯T423)", act, reason)
	}
	o.Phase = "phase_unknown"
	act, reason = ClassifyIdleNudge(o)
	if act != IdleNudgeSkip || reason != "phase_unknown" {
		t.Fatalf("phase_unknown: %s/%s", act, reason)
	}
}

func TestClassifyIdleNudgeIdleStuckAndPostRestart(t *testing.T) {
	t.Parallel()
	idle := IdleNudgeObs{
		Name: "w", Purpose: claudia.PurposeWork, ProcessRunning: true,
		Phase: "idle", IdleFor: 10 * time.Minute, HasOpenMission: true,
	}
	act, reason := ClassifyIdleNudge(idle)
	if act != IdleNudgeNudge || reason != "idle_stuck" {
		t.Fatalf("idle stuck: %s/%s", act, reason)
	}

	// Post-restart wakes without full idle age.
	pr := IdleNudgeObs{
		Name: "w", Purpose: claudia.PurposeWork, ProcessRunning: true,
		Phase: "idle", IdleFor: 0, HasOpenMission: true, PostRestart: true,
	}
	act, reason = ClassifyIdleNudge(pr)
	if act != IdleNudgeNudge || reason != "post_restart_wake" {
		t.Fatalf("post-restart: %s/%s", act, reason)
	}
}

// 🎯T207 owner pin: never-briefed → full_brief; briefed → continue.
func TestClassifyIdleNudgeKindBriefOrVerify(t *testing.T) {
	t.Parallel()
	if k := ClassifyIdleNudgeKind(false); k != IdleNudgeKindFullBrief {
		t.Fatalf("never-briefed want full_brief got %s", k)
	}
	if k := ClassifyIdleNudgeKind(true); k != IdleNudgeKindContinue {
		t.Fatalf("briefed want continue got %s", k)
	}
}

func TestFormatIdleNudgeTextNeverBareContinueFirstPass(t *testing.T) {
	t.Parallel()
	full := FormatIdleNudgeText(IdleNudgeTextArgs{
		Name: "jv-t207", TargetID: "T207",
		Acceptance: "auto-nudge idle workers",
		Reason:     "post_restart_wake", PostRestart: true,
		Kind: IdleNudgeKindFullBrief,
	})
	if !strings.Contains(full, "Jevons fleet standing brief") {
		t.Fatal("full_brief must include standing fleet brief")
	}
	if !strings.Contains(full, "🎯T207") {
		t.Fatal("full_brief must include target id")
	}
	if !strings.Contains(full, "auto-nudge idle workers") {
		t.Fatal("full_brief must include acceptance")
	}
	// Must not be a one-word / bare continue payload.
	trimmed := strings.TrimSpace(full)
	if strings.EqualFold(trimmed, "continue") || strings.EqualFold(trimmed, "resume") {
		t.Fatal("full_brief must not be bare continue/resume")
	}
	if len(full) < 200 {
		t.Fatalf("full_brief too short to be a real brief: %d chars", len(full))
	}

	cont := FormatIdleNudgeText(IdleNudgeTextArgs{
		Name: "jv-t207", TargetID: "T207", Reason: "idle_stuck",
		Kind: IdleNudgeKindContinue,
	})
	if strings.Contains(cont, "Jevons fleet standing brief") {
		t.Fatal("continue kind must not re-inject full standing brief")
	}
	if !strings.Contains(cont, "brief already present") {
		t.Fatal("continue kind should state brief already present")
	}
	// Empty kind fails closed to full brief.
	failClosed := FormatIdleNudgeText(IdleNudgeTextArgs{Name: "w", Kind: ""})
	if !strings.Contains(failClosed, "Jevons fleet standing brief") {
		t.Fatal("empty kind must fail closed to full_brief")
	}
}

func TestSessionLooksBriefed(t *testing.T) {
	t.Parallel()
	if SessionLooksBriefed("") {
		t.Fatal("empty not briefed")
	}
	if SessionLooksBriefed("please continue") {
		t.Fatal("bare continue is not a brief")
	}
	if !SessionLooksBriefed(FleetStandingBrief + "do the thing") {
		t.Fatal("standing brief marker should count")
	}
}

func TestNextNudgeBackoff(t *testing.T) {
	t.Parallel()
	if d := NextNudgeBackoff(0, nil); d != 0 {
		t.Fatalf("count 0: %v", d)
	}
	if d := NextNudgeBackoff(1, nil); d != DefaultIdleNudgeBackoffs[0] {
		t.Fatalf("count 1: %v", d)
	}
	if d := NextNudgeBackoff(99, nil); d != DefaultIdleNudgeBackoffs[len(DefaultIdleNudgeBackoffs)-1] {
		t.Fatalf("overflow: %v", d)
	}
}

func TestIdleNudgeLedgerRoundTrip(t *testing.T) {
	dir := t.TempDir()
	l, err := OpenIdleNudgeLedger(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	if err := l.Record("w1", now); err != nil {
		t.Fatal(err)
	}
	c, last := l.Get("w1")
	if c != 1 || !last.Equal(now) {
		t.Fatalf("got count=%d last=%v", c, last)
	}
	// Reload
	l2, err := OpenIdleNudgeLedger(dir)
	if err != nil {
		t.Fatal(err)
	}
	c2, last2 := l2.Get("w1")
	if c2 != 1 || !last2.Equal(now) {
		t.Fatalf("reload count=%d last=%v", c2, last2)
	}
	if _, err := OpenIdleNudgeLedger(""); err == nil {
		t.Fatal("empty state dir should error")
	}
	_ = filepath.Join(dir, "fleet", "idle_nudge.json") // path shape
}

// Hermetic: never-briefed → full brief payload; briefed → continue;
// event source names match product path.
func TestIdleNudgeDeliverPayloadBriefOrVerify(t *testing.T) {
	t.Parallel()
	var pushed []struct{ event, text string }
	push := func(target, event, text string) error {
		pushed = append(pushed, struct{ event, text string }{event, text})
		return nil
	}
	kind0 := ClassifyIdleNudgeKind(false)
	text0 := FormatIdleNudgeText(IdleNudgeTextArgs{
		Name: "never", TargetID: "T207", Kind: kind0, PostRestart: true,
		Reason: "post_restart_wake",
	})
	if err := push("never", IdleNudgeEventSource(true, kind0), text0); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(pushed[0].text, "Jevons fleet standing brief") {
		t.Fatal("first push must be full brief")
	}
	if pushed[0].event != "post-restart-brief" {
		t.Fatalf("event=%s", pushed[0].event)
	}

	kind1 := ClassifyIdleNudgeKind(true)
	text1 := FormatIdleNudgeText(IdleNudgeTextArgs{
		Name: "never", TargetID: "T207", Kind: kind1, Reason: "idle_stuck",
	})
	if err := push("never", IdleNudgeEventSource(false, kind1), text1); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(pushed[1].text, "Jevons fleet standing brief") {
		t.Fatal("second push must be short continue, not re-brief")
	}
	if pushed[1].event != "idle-nudge" {
		t.Fatalf("event=%s", pushed[1].event)
	}

	// Sweep with nil reg is no-op.
	if reps := SweepIdleNudges(IdleNudgeSweepArgs{}); len(reps) != 0 {
		t.Fatalf("nil reg: %v", reps)
	}
}

func TestIdleActivityTrackerObserve(t *testing.T) {
	t.Parallel()
	tr := NewIdleActivityTracker()
	tr.now = func() time.Time { return time.Unix(1000, 0) }
	tr.SeedRunning("a")
	got := tr.Get("a")
	if got.Phase != "idle" {
		t.Fatalf("seed phase=%q", got.Phase)
	}
	tr.Observe("a", claudia.Event{Type: "assistant", Text: "hi"})
	got = tr.Get("a")
	if got.Phase != "working" {
		t.Fatalf("after assistant phase=%q", got.Phase)
	}
}

// Daemon-path hermetic: post-restart sweep delivers full_brief for never-briefed
// running work agent; deliberate stop + design-gated + maxed skipped; second
// sweep after MarkBriefed uses continue kind.
func TestSweepIdleNudgesPostRestartFullBriefThenContinue(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []claudia.AgentDef{
		{Name: "jevons", WorkDir: dir, SessionID: "s-o", Purpose: claudia.PurposeOverseer, Materialized: true, Provider: "grok", AutoStart: true},
		{Name: "jv-t207-idle", WorkDir: dir, SessionID: "s-w", Purpose: claudia.PurposeWork, Parent: "jevons-po",
			Materialized: true, Provider: "grok", AutoStart: true, TargetID: "T207"},
		{Name: "jv-stopped", WorkDir: dir, SessionID: "s-s", Purpose: claudia.PurposeWork,
			Materialized: true, Provider: "grok", AutoStart: false}, // deliberate stop residual
		{Name: "jv-design", WorkDir: dir, SessionID: "s-d", Purpose: claudia.PurposeWork,
			Materialized: true, Provider: "grok", AutoStart: true, TargetID: "T29"},
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
	activity.now = func() time.Time { return time.Unix(2000, 0) }
	activity.SeedRunning("jv-t207-idle")
	activity.SeedRunning("jv-design")

	briefed := map[string]bool{}
	var pushed []struct{ target, event, text string }
	push := func(target, event, text string) error {
		pushed = append(pushed, struct{ target, event, text string }{target, event, text})
		return nil
	}
	running := map[string]bool{
		"jv-t207-idle": true,
		"jv-design":    true,
		// jv-stopped deliberately not running
	}

	now := time.Unix(2000, 0)
	reps := SweepIdleNudges(IdleNudgeSweepArgs{
		Reg:            reg,
		Activity:       activity,
		Ledger:         ledger,
		Push:           push,
		Now:            now,
		PostRestart:    true,
		OverseerName:   "jevons",
		BriefPresent:   func(name string) bool { return briefed[name] },
		MarkBriefed:    func(name string) { briefed[name] = true },
		DesignGated:    func(tid string) bool { return tid == "T29" },
		ProcessRunning: func(name string) bool { return running[name] },
	})

	byName := map[string]IdleNudgeReport{}
	for _, r := range reps {
		byName[r.Name] = r
	}
	w := byName["jv-t207-idle"]
	if !w.Delivered || w.Kind != IdleNudgeKindFullBrief {
		t.Fatalf("worker report=%+v want full_brief delivered", w)
	}
	if len(pushed) != 1 || !strings.Contains(pushed[0].text, "Jevons fleet standing brief") {
		t.Fatalf("pushed=%+v", pushed)
	}
	if !briefed["jv-t207-idle"] {
		t.Fatal("MarkBriefed should run after full_brief")
	}
	if d := byName["jv-design"]; d.Action != IdleNudgeSkip || d.Reason != "design_gated" {
		t.Fatalf("design: %+v", d)
	}
	if s := byName["jv-stopped"]; s.Action != IdleNudgeSkip || (s.Reason != "deliberate_stop" && s.Reason != "not_running") {
		t.Fatalf("stopped: %+v", s)
	}

	// Second sweep: brief present → continue kind after backoff satisfied
	// (simulate EverNudged with ledger count 1 and far-future now).
	pushed = nil
	later := now.Add(20 * time.Minute)
	// Force idle again for second classification.
	activity.by["jv-t207-idle"] = IdleActivity{Phase: "idle", Updated: later.Add(-10 * time.Minute)}
	reps2 := SweepIdleNudges(IdleNudgeSweepArgs{
		Reg:            reg,
		Activity:       activity,
		Ledger:         ledger,
		Push:           push,
		Now:            later,
		PostRestart:    false,
		OverseerName:   "jevons",
		SessionPhase:   func(claudia.AgentDef) turnev.Phase { return turnev.PhaseIdle },
		BriefPresent:   func(name string) bool { return briefed[name] },
		MarkBriefed:    func(name string) { briefed[name] = true },
		DesignGated:    func(tid string) bool { return tid == "T29" },
		ProcessRunning: func(name string) bool { return running[name] },
	})
	var w2 IdleNudgeReport
	for _, r := range reps2 {
		if r.Name == "jv-t207-idle" {
			w2 = r
		}
	}
	if !w2.Delivered || w2.Kind != IdleNudgeKindContinue {
		t.Fatalf("second nudge report=%+v want continue delivered", w2)
	}
	if len(pushed) != 1 || strings.Contains(pushed[0].text, "Jevons fleet standing brief") {
		t.Fatalf("second push should be short continue: %+v", pushed)
	}

	// Max out: pad ledger count to DefaultIdleNudgeMax.
	for {
		c, _ := ledger.Get("jv-t207-idle")
		if c >= DefaultIdleNudgeMax {
			break
		}
		_ = ledger.Record("jv-t207-idle", later)
	}
	activity.by["jv-t207-idle"] = IdleActivity{Phase: "idle", Updated: later.Add(-10 * time.Minute)}
	far := later.Add(2 * time.Hour)
	reps3 := SweepIdleNudges(IdleNudgeSweepArgs{
		Reg: reg, Activity: activity, Ledger: ledger, Push: push, Now: far,
		OverseerName:   "jevons",
		BriefPresent:   func(name string) bool { return true },
		ProcessRunning: func(name string) bool { return running[name] },
	})
	for _, r := range reps3 {
		if r.Name == "jv-t207-idle" && r.Action != IdleNudgeMaxed {
			t.Fatalf("want maxed after max nudges: %+v", r)
		}
	}

	// After a successful nudge, activity stays phase=idle (clock reset only).
	// Claiming "working" without ACP evidence poisoned live re-nudge.
	if ph := activity.Get("jv-t207-idle").Phase; ph != "idle" {
		t.Fatalf("post-nudge activity phase=%q want idle (not fake working)", ph)
	}
}

// 🎯T207: empty target_id work agents remain eligible (no_open_mission is not
// the live skip for jv-* rows that never bound T198).
func TestSweepIdleNudgesEmptyTargetIDWorkEligible(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jv-t198-frontier-engaged", WorkDir: dir, SessionID: "s1",
		Purpose: claudia.PurposeWork, Materialized: true, Provider: "grok", AutoStart: true,
		// TargetID empty — live common case
	}); err != nil {
		t.Fatal(err)
	}
	activity := NewIdleActivityTracker()
	now := time.Unix(3000, 0)
	activity.by = map[string]IdleActivity{
		"jv-t198-frontier-engaged": {Phase: "idle", Updated: now.Add(-10 * time.Minute)},
	}
	var pushed int
	reps := SweepIdleNudges(IdleNudgeSweepArgs{
		Reg: reg, Activity: activity, Now: now, OverseerName: "jevons",
		SessionPhase: func(claudia.AgentDef) turnev.Phase { return turnev.PhaseIdle },
		Push: func(target, event, text string) error {
			pushed++
			return nil
		},
		ProcessRunning: func(name string) bool { return true },
	})
	if pushed != 1 {
		t.Fatalf("pushed=%d want 1 for empty target_id idle work agent; reps=%+v", pushed, reps)
	}
}

func TestFormatIdleNudgeWire(t *testing.T) {
	t.Parallel()
	got := formatIdleNudgeWire("idle-nudge-brief", "hello")
	if got != "[event: idle-nudge-brief] hello" {
		t.Fatalf("got %q", got)
	}
}

// 🎯T171 path 2: post-restart resume delivers to open-mission workers only —
// not PO, not aside, not deliberate-stop.
func TestSweepIdleNudgesPostRestartSkipsPOAndAside(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []claudia.AgentDef{
		{Name: "jevons", WorkDir: dir, SessionID: "s-o", Purpose: claudia.PurposeOverseer, Materialized: true, Provider: "grok", AutoStart: true},
		{Name: "jevons-po", WorkDir: dir, SessionID: "s-po", Purpose: claudia.PurposeWork, Parent: "jevons",
			Materialized: true, Provider: "grok", AutoStart: true},
		{Name: "jv-t171-worker", WorkDir: dir, SessionID: "s-w", Purpose: claudia.PurposeWork, Parent: "jevons-po",
			Materialized: true, Provider: "grok", AutoStart: true, TargetID: "T171"},
		{Name: "aside-1", WorkDir: dir, SessionID: "s-a", Purpose: claudia.PurposeAside, Parent: "jevons",
			Materialized: true, Provider: "grok", AutoStart: true},
	} {
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}
	activity := NewIdleActivityTracker()
	activity.SeedRunning("jv-t171-worker")
	activity.SeedRunning("jevons-po")
	var pushed []string
	reps := SweepIdleNudges(IdleNudgeSweepArgs{
		Reg: reg, Activity: activity, Now: time.Unix(4000, 0), PostRestart: true,
		OverseerName: "jevons",
		Push: func(target, event, text string) error {
			pushed = append(pushed, target)
			return nil
		},
		ProcessRunning: func(name string) bool {
			return name == "jv-t171-worker" || name == "jevons-po" || name == "aside-1"
		},
	})
	if len(pushed) != 1 || pushed[0] != "jv-t171-worker" {
		t.Fatalf("pushed=%v want only jv-t171-worker; reps=%+v", pushed, reps)
	}
	by := map[string]IdleNudgeReport{}
	for _, r := range reps {
		by[r.Name] = r
	}
	if w := by["jv-t171-worker"]; !w.Delivered {
		t.Fatalf("worker: %+v", w)
	}
	if po := by["jevons-po"]; po.Action == IdleNudgeNudge || po.Delivered {
		t.Fatalf("PO must not short-resume: %+v", po)
	}
}

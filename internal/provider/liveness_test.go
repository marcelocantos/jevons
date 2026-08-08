// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/config"
)

// 🎯T27.9 unit oracles: signal-source probes and the stall state machine
// under an injected clock. The end-to-end fault-injection oracle (server
// + HTTP surfaces + notification path) lives in internal/server.

func livenessDecl(id, cadence string, src config.AutomationSource) config.AutomationDecl {
	return config.AutomationDecl{ID: id, Cadence: cadence, Grace: 2, Source: src}
}

func TestLivenessFileMtimeStallAndClear(t *testing.T) {
	sig := filepath.Join(t.TempDir(), "auto.log")
	if err := os.WriteFile(sig, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	base := time.Now()
	now := base
	var notices []AutomationStatus
	reg := NewRegistry()
	m := NewLivenessMonitor(LivenessMonitorArgs{
		Decls: []config.AutomationDecl{livenessDecl("auto", "1h",
			config.AutomationSource{Kind: config.AutomationSourceFileMtime, Path: sig})},
		Registry: reg,
		Now:      func() time.Time { return now },
		OnNotice: func(st AutomationStatus) { notices = append(notices, st) },
	})
	ctx := context.Background()

	// Fresh signal → ok, and first-sight ok is not owner-worthy.
	m.Check(ctx)
	sts := m.Statuses()
	if len(sts) != 1 || sts[0].State != AutomationOK {
		t.Fatalf("statuses=%+v, want ok", sts)
	}
	if len(notices) != 0 {
		t.Fatalf("notices=%+v, want none on first ok", notices)
	}

	// Freeze the signal past cadence×grace (1h × 2) → stalled + notice +
	// stall event folded into the aggregated model.
	now = base.Add(3 * time.Hour)
	m.Check(ctx)
	sts = m.Statuses()
	if sts[0].State != AutomationStalled {
		t.Fatalf("state=%q detail=%q, want stalled", sts[0].State, sts[0].Detail)
	}
	if !strings.Contains(sts[0].Detail, "no signal for") {
		t.Fatalf("detail=%q, want overdue description", sts[0].Detail)
	}
	if len(notices) != 1 || notices[0].State != AutomationStalled {
		t.Fatalf("notices=%+v, want one stall", notices)
	}
	evs := reg.ModelFeed(LivenessProviderID, LivenessFeed)
	if len(evs) == 0 || evs[len(evs)-1].Kind != "stall" {
		t.Fatalf("model events=%+v, want trailing stall", evs)
	}

	// A fresh signal clears the stall → ok + recovery notice + clear event.
	if err := os.Chtimes(sig, now, now); err != nil {
		t.Fatal(err)
	}
	m.Check(ctx)
	sts = m.Statuses()
	if sts[0].State != AutomationOK {
		t.Fatalf("state=%q, want ok after fresh signal", sts[0].State)
	}
	if len(notices) != 2 || notices[1].State != AutomationOK {
		t.Fatalf("notices=%+v, want stall then recovery", notices)
	}
	evs = reg.ModelFeed(LivenessProviderID, LivenessFeed)
	if evs[len(evs)-1].Kind != "clear" {
		t.Fatalf("model events=%+v, want trailing clear", evs)
	}

	// Steady state folds nothing new.
	before := len(evs)
	m.Check(ctx)
	if got := len(reg.ModelFeed(LivenessProviderID, LivenessFeed)); got != before {
		t.Fatalf("steady-state check folded events: %d → %d", before, got)
	}
}

func TestLivenessProviderFeedSource(t *testing.T) {
	base := time.Now()
	now := base
	reg := NewRegistry()
	// mnemo's activity-style feed, last event a day old.
	reg.Ingest("mnemo", FeedEvent{
		Feed: "health", Seq: 1, TS: base.Add(-24 * time.Hour), Origin: "mnemo", Kind: "tick",
	})
	m := NewLivenessMonitor(LivenessMonitorArgs{
		Decls: []config.AutomationDecl{livenessDecl("mnemo-activity", "1h",
			config.AutomationSource{Kind: config.AutomationSourceProviderFeed, Provider: "mnemo", Feed: "health"})},
		Registry: reg,
		Now:      func() time.Time { return now },
	})
	ctx := context.Background()
	m.Check(ctx)
	if st := m.Statuses()[0]; st.State != AutomationStalled {
		t.Fatalf("state=%q, want stalled on day-old feed", st.State)
	}
	// Fresh feed event → recovered.
	reg.Ingest("mnemo", FeedEvent{Feed: "health", Seq: 2, TS: now, Origin: "mnemo", Kind: "tick"})
	m.Check(ctx)
	if st := m.Statuses()[0]; st.State != AutomationOK {
		t.Fatalf("state=%q, want ok after fresh feed event", st.State)
	}
}

func TestLivenessProviderFeedNeverSignalled(t *testing.T) {
	reg := NewRegistry()
	m := NewLivenessMonitor(LivenessMonitorArgs{
		Decls: []config.AutomationDecl{livenessDecl("ghost", "1h",
			config.AutomationSource{Kind: config.AutomationSourceProviderFeed, Provider: "mnemo", Feed: "health"})},
		Registry: reg,
	})
	m.Check(context.Background())
	st := m.Statuses()[0]
	if st.State != AutomationStalled || !strings.Contains(st.Detail, "no signal ever") {
		t.Fatalf("status=%+v, want stalled with never-signalled detail", st)
	}
}

func TestLivenessLaunchdLastExitStatus(t *testing.T) {
	out := `{ "PID" = 4; "LastExitStatus" = 0; };`
	var cmdErr error
	reg := NewRegistry()
	m := NewLivenessMonitor(LivenessMonitorArgs{
		Decls: []config.AutomationDecl{livenessDecl("yadm-sync", "30m",
			config.AutomationSource{Kind: config.AutomationSourceLaunchd, Label: "com.example.sync"})},
		Registry: reg,
		Run: func(ctx context.Context, name string, args ...string) (string, error) {
			if name != "launchctl" || args[0] != "list" || args[1] != "com.example.sync" {
				t.Fatalf("unexpected command %s %v", name, args)
			}
			return out, cmdErr
		},
	})
	ctx := context.Background()

	m.Check(ctx)
	if st := m.Statuses()[0]; st.State != AutomationOK {
		t.Fatalf("state=%q, want ok on exit 0", st.State)
	}

	out = `{ "LastExitStatus" = 78; };`
	m.Check(ctx)
	if st := m.Statuses()[0]; st.State != AutomationStalled || !strings.Contains(st.Detail, "78") {
		t.Fatalf("status=%+v, want stalled with exit status 78", m.Statuses()[0])
	}

	cmdErr = fmt.Errorf("Could not find service")
	m.Check(ctx)
	if st := m.Statuses()[0]; st.State != AutomationStalled || !strings.Contains(st.Detail, "not loaded") {
		t.Fatalf("status=%+v, want stalled not-loaded", m.Statuses()[0])
	}
}

func TestLivenessGitLastCommitSource(t *testing.T) {
	base := time.Unix(1700000000, 0)
	now := base.Add(30 * time.Minute)
	reg := NewRegistry()
	m := NewLivenessMonitor(LivenessMonitorArgs{
		Decls: []config.AutomationDecl{livenessDecl("knowledge", "1h",
			config.AutomationSource{Kind: config.AutomationSourceGitLastCommit, Repo: "/tmp/repo"})},
		Registry: reg,
		Now:      func() time.Time { return now },
		Run: func(ctx context.Context, name string, args ...string) (string, error) {
			return "1700000000\n", nil
		},
	})
	ctx := context.Background()
	m.Check(ctx)
	if st := m.Statuses()[0]; st.State != AutomationOK {
		t.Fatalf("state=%q, want ok at 30m", st.State)
	}
	now = base.Add(3 * time.Hour)
	m.Check(ctx)
	if st := m.Statuses()[0]; st.State != AutomationStalled {
		t.Fatalf("state=%q, want stalled at 3h", st.State)
	}
}

func TestLivenessNewestArtifact(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "a.md")
	fresh := filepath.Join(dir, "b.md")
	for _, p := range []string{old, fresh} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	base := time.Now()
	if err := os.Chtimes(old, base.Add(-48*time.Hour), base.Add(-48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(fresh, base.Add(-10*time.Minute), base.Add(-10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	reg := NewRegistry()
	m := NewLivenessMonitor(LivenessMonitorArgs{
		Decls: []config.AutomationDecl{livenessDecl("artifacts", "1h",
			config.AutomationSource{Kind: config.AutomationSourceNewestArtifact, Dir: dir, Glob: "*.md"})},
		Registry: reg,
		Now:      func() time.Time { return base },
	})
	m.Check(context.Background())
	st := m.Statuses()[0]
	if st.State != AutomationOK {
		t.Fatalf("state=%q detail=%q, want ok (newest artifact 10m old)", st.State, st.Detail)
	}
	if got := base.Sub(st.LastSignal).Round(time.Minute); got != 10*time.Minute {
		t.Fatalf("last signal age=%s, want 10m (newest, not oldest)", got)
	}
}

func TestLivenessProbeErrorIsUnknownNotNotified(t *testing.T) {
	var notices []AutomationStatus
	reg := NewRegistry()
	m := NewLivenessMonitor(LivenessMonitorArgs{
		Decls: []config.AutomationDecl{livenessDecl("gone", "1h",
			config.AutomationSource{Kind: config.AutomationSourceFileMtime, Path: filepath.Join(t.TempDir(), "missing.log")})},
		Registry: reg,
		OnNotice: func(st AutomationStatus) { notices = append(notices, st) },
	})
	m.Check(context.Background())
	if st := m.Statuses()[0]; st.State != AutomationUnknown {
		t.Fatalf("state=%q, want unknown on probe error", st.State)
	}
	if len(notices) != 0 {
		t.Fatalf("notices=%+v, want none for unknown", notices)
	}
}

func TestFormatAutomationNotice(t *testing.T) {
	stall := FormatAutomationNotice(AutomationStatus{
		ID: "ytt-daily", State: AutomationStalled,
		Detail: "no signal for 216h (cadence 24h × grace 2)", Source: "file-mtime",
	})
	if !strings.Contains(stall, "ytt-daily") || !strings.Contains(stall, "no signal for 216h") {
		t.Fatalf("stall notice=%q", stall)
	}
	clear := FormatAutomationNotice(AutomationStatus{
		ID: "ytt-daily", State: AutomationOK, LastSignal: time.Now(), Source: "file-mtime",
	})
	if !strings.Contains(clear, "recovered") {
		t.Fatalf("clear notice=%q", clear)
	}
}

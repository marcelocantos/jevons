// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/handover"
	"github.com/marcelocantos/jevons/internal/planusage"
	"github.com/marcelocantos/jevons/internal/thread"
)

type sweepLedger struct {
	pending   []handover.Pending
	seeded    []string
	cleared   []string
	prepared  int
	completed int
	compacts  int
	reg       *claudia.Registry
	launchErr error
}

func (l *sweepLedger) PrepareMigration(name string, to claudia.Provider, _ bool) (handover.Pending, error) {
	l.prepared++
	p := handover.Pending{Agent: name, From: "grok", To: string(to), TranscriptPath: "/thin.jsonl"}
	l.pending = append(l.pending, p)
	return p, nil
}
func (l *sweepLedger) CompleteThinBrief(p handover.Pending) (handover.Pending, error) {
	l.completed++
	l.compacts++
	if l.reg != nil {
		sid := "compact-sess"
		temp := "jv-compact-" + sid[:8]
		_ = l.reg.Register(claudia.AgentDef{
			Name: temp, SessionID: sid, Provider: claudia.Provider(p.To),
			Purpose: claudia.PurposeAside, Parent: "jevons-po",
		})
	}
	return p, nil
}
func (l *sweepLedger) Launch(*thread.Thread) error                   { return l.launchErr }
func (l *sweepLedger) PendingHandovers() ([]handover.Pending, error) { return l.pending, nil }
func (l *sweepLedger) SeedSuccessor(name string) (handover.Pending, bool, error) {
	l.seeded = append(l.seeded, name)
	return handover.Pending{Agent: name}, true, nil
}

func TestT543PlanSweepCompletesPendingHandoverOnlyOnce(t *testing.T) {
	reg, err := claudia.NewRegistry(t.TempDir() + "/agents.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jv-t543-worker", SessionID: "s1", Provider: claudia.ProviderGrok,
		Purpose: claudia.PurposeWork, Parent: "jevons-po",
	}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	led := &sweepLedger{launchErr: errors.New("depth ceiling"), reg: reg}
	s := New(t.TempDir(), nil, nil)
	s.SetRegistry(reg)
	s.SetMigrator(led)
	s.SetPlanUsageSource(func() planusage.Snapshot {
		return planusage.Snapshot{At: now, Backends: []planusage.Backend{
			t39015Weekly("grok", 0, 100, now),
			t39015Weekly("codex", 80, 20, now),
		}}
	})

	s.SweepPlanPolicy()
	s.SweepPlanPolicy()
	if led.prepared != 1 || led.completed != 1 || led.compacts != 1 {
		t.Fatalf("prepare=%d complete=%d compacts=%d; want each once across two sweeps",
			led.prepared, led.completed, led.compacts)
	}
	var compact, actions []string
	for _, d := range reg.List() {
		if strings.HasPrefix(d.Name, "jv-compact-") {
			compact = append(compact, d.Name)
		}
	}
	if len(compact) != 1 {
		t.Fatalf("compact seats in registry = %v; want exactly one mint", compact)
	}
	for _, a := range s.SweepPlanPolicy() {
		actions = append(actions, a.Name)
	}
	for _, name := range actions {
		if strings.HasPrefix(name, "jv-compact-") {
			t.Fatalf("PlanActions still targeted compact seat %q", name)
		}
	}
}
func (l *sweepLedger) ClearHandover(name string) error {
	l.cleared = append(l.cleared, name)
	kept := l.pending[:0]
	for _, p := range l.pending {
		if p.Agent != name {
			kept = append(kept, p)
		}
	}
	l.pending = kept
	return nil
}

func TestT418SweepRetriesAlivePending(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(dir + "/agents.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{Name: "jv", SessionID: "s1"}); err != nil {
		t.Fatal(err)
	}
	led := &sweepLedger{pending: []handover.Pending{{
		Agent: "jv", TranscriptPath: "/t.jsonl",
		CreatedAt: time.Now().UTC().Add(-time.Minute).Format(time.RFC3339),
	}}}
	s := &Server{registry: reg, migrator: led}
	s.SetSenderResolver(func(string) (agentSender, bool, error) {
		return &recordingSender{}, true, nil
	})
	s.SweepHandovers()
	if len(led.seeded) != 1 || led.seeded[0] != "jv" {
		t.Fatalf("seeded = %v; want retry of jv", led.seeded)
	}
}

func TestT517SweepHandoversDropsPOPending(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(dir + "/agents.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []claudia.AgentDef{
		{Name: "jevons", SessionID: "s-root", Purpose: claudia.PurposeOverseer},
		{Name: "jevons-po", SessionID: "s-po", Purpose: claudia.PurposeWork, Parent: "jevons"},
		{Name: "jv-w", SessionID: "s-w", Purpose: claudia.PurposeWork, Parent: "jevons-po"},
	} {
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}
	led := &sweepLedger{pending: []handover.Pending{
		{Agent: "jevons-po", TranscriptPath: "/po.jsonl", CreatedAt: time.Now().UTC().Format(time.RFC3339)},
		{Agent: "jv-w", TranscriptPath: "/w.jsonl", CreatedAt: time.Now().UTC().Format(time.RFC3339)},
	}}
	s := &Server{registry: reg, migrator: led}
	s.SetSenderResolver(func(string) (agentSender, bool, error) {
		return &recordingSender{}, true, nil
	})
	s.SweepHandovers()
	if len(led.cleared) != 1 || led.cleared[0] != "jevons-po" {
		t.Fatalf("cleared = %v; want jevons-po only", led.cleared)
	}
	if len(led.seeded) != 1 || led.seeded[0] != "jv-w" {
		t.Fatalf("seeded = %v; want worker retry", led.seeded)
	}
}

func TestT418SweepReapsGoneAgent(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(dir + "/agents.json")
	if err != nil {
		t.Fatal(err)
	}
	led := &sweepLedger{pending: []handover.Pending{{
		Agent: "gone", TranscriptPath: "/t.jsonl",
		CreatedAt: time.Now().UTC().Add(-time.Hour).Format(time.RFC3339),
	}}}
	s := &Server{registry: reg, migrator: led}
	s.SweepHandovers()
	if len(led.cleared) != 1 || led.cleared[0] != "gone" {
		t.Fatalf("cleared = %v; want reap of gone", led.cleared)
	}
}

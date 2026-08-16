// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/handover"
	"github.com/marcelocantos/jevons/internal/thread"
)

type sweepLedger struct {
	pending []handover.Pending
	seeded  []string
	cleared []string
}

func (l *sweepLedger) PrepareMigration(string, claudia.Provider, bool) (handover.Pending, error) {
	return handover.Pending{}, nil
}
func (l *sweepLedger) CompleteThinBrief(p handover.Pending) (handover.Pending, error) { return p, nil }
func (l *sweepLedger) Launch(*thread.Thread) error                                    { return nil }
func (l *sweepLedger) PendingHandovers() ([]handover.Pending, error)                  { return l.pending, nil }
func (l *sweepLedger) SeedSuccessor(name string) (handover.Pending, bool, error) {
	l.seeded = append(l.seeded, name)
	return handover.Pending{Agent: name}, true, nil
}
func (l *sweepLedger) ClearHandover(name string) error {
	l.cleared = append(l.cleared, name)
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

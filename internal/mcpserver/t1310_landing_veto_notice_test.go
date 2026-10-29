// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/envelope"
)

// 🎯T1310: a worker whose cited commit is on its own branch but not on the
// shared checkout's tip is kept by the landing veto, and its parent hears
// so once, with both ways out. On 2026-10-29 an overseer cherry-picked two
// workers' branch commits onto master; the veto kept both with a log line
// only and they sat idle for good.
func TestT1310LandingVetoTellsTheParentOnce(t *testing.T) {
	dir := t.TempDir()
	t1122GitRun(t, dir, "init", "-q")
	t1122GitRun(t, dir, "checkout", "-q", "-b", "master")
	t1122GitRun(t, dir, "commit", "-q", "--allow-empty", "-m", "base")
	t1122GitRun(t, dir, "checkout", "-q", "-b", "feature/json")
	t1122GitRun(t, dir, "commit", "-q", "--allow-empty", "-m", "json on its branch")
	branchSHA := t1122GitRun(t, dir, "rev-parse", "HEAD")
	t1122GitRun(t, dir, "checkout", "-q", "master")

	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []claudia.AgentDef{
		{Name: "lead", WorkDir: dir, SessionID: "lead", Purpose: claudia.PurposeWork, Parent: "jevons", Materialized: true, Provider: "grok"},
		{Name: "json-worker", WorkDir: dir, SessionID: "w", Purpose: claudia.PurposeWork, Parent: "lead", Materialized: true, Provider: "grok"},
	} {
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}
	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	lead := &fakeSender{alive: true}
	setObservedSenderResolver(s, func(name string) (agentSender, bool, error) {
		if name != "lead" {
			t.Fatalf("unexpected delivery to %q", name)
		}
		return lead, false, nil
	})
	s.SetTurnWitness(witnessYielding(TurnEvidence{Observed: true, PayloadSeen: true, Detail: "test"}))

	report := envelope.Format(&envelope.Message{
		Kind: envelope.KindFinishReport, Target: "T1", SHA: branchSHA,
		SilentLedger: envelope.SilentLedgerEmpty, Payload: "Committed on feature/json.",
	})
	s.maybeReapDoneWorkAgent("json-worker", report)
	s.maybeReapDoneWorkAgent("json-worker", report) // the retries that follow a terminal report

	if reg.Def("json-worker") == nil {
		t.Fatal("a worker whose cited commit is not on the tip was reaped")
	}
	lead.mu.Lock()
	sent := append([]string(nil), lead.sent...)
	lead.mu.Unlock()
	if len(sent) != 1 {
		t.Fatalf("parent notices = %d, want exactly 1: %v", len(sent), sent)
	}
	for _, want := range []string{"[reap-veto 🎯T1310]", "json-worker", branchSHA, "disposition=reap", "re-land"} {
		if !strings.Contains(sent[0], want) {
			t.Fatalf("notice lacks %q:\n%s", want, sent[0])
		}
	}
}

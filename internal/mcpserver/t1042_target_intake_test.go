package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/ownerquestion"
	"github.com/marcelocantos/jevons/internal/ownergate"
	"github.com/marcelocantos/jevons/internal/ownerquestions"
	"github.com/marcelocantos/jevons/internal/ownerquestionview"
)

func mustOwnerGateQuestion(t *testing.T, cwd, target, text string) ownerquestion.Question {
	t.Helper()
	q, err := ownerquestion.FromOwnerGate(cwd, target, text, "po")
	if err != nil {
		t.Fatal(err)
	}
	return q
}

func TestT1042TargetLedgerExplicitContextPagesOnce(t *testing.T) {
	dir := t.TempDir()
	ledger := `schema_version: "1.0"
targets:
  T44.6:
    name: cube map pick
    status: converging
    context: |
      owner decision requested: choose cube-map A or B for YourWorld2.
      Evidence: commit fedcba0987654321.
`
	if err := os.WriteFile(filepath.Join(dir, "bullseye.yaml"), []byte(ledger), 0o644); err != nil {
		t.Fatal(err)
	}
	state := t.TempDir()
	bin := t.TempDir()
	marker := filepath.Join(bin, "calls")
	if err := os.WriteFile(filepath.Join(bin, "blurter"), []byte("#!/bin/sh\necho \"$*\" >> '"+marker+"'\necho /spool/accepted\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	s := New(dir, nil, nil)
	s.SetAgentReportDir(state)
	s.SetOwnerQuestionsDir(state)
	s.observeTargetLedgerOwnerBlockers(dir, "T44.6")
	s.observeTargetLedgerOwnerBlockers(dir, "T44.6")
	b, err := os.ReadFile(marker)
	if err != nil || strings.Count(string(b), "--subject") != 1 || !strings.Contains(string(b), "cube-map") {
		t.Fatalf("spool %q err=%v", b, err)
	}
	open, _ := ownerquestionview.New(state).List(true)
	if len(open) != 1 || open[0].Asker != "target-ledger" {
		t.Fatalf("view %+v", open)
	}
	assertT1042OutboxHonestAudit(t, state, 1)
}

func TestT1042TargetLedgerDeviceHoldDoesNotPage(t *testing.T) {
	dir := t.TempDir()
	ledger := `schema_version: "1.0"
targets:
  T370:
    name: keypress
    status: converging
    owned_by:
      owner: marcelo
      reason: needs real cmd-shift-] keypress
`
	if err := os.WriteFile(filepath.Join(dir, "bullseye.yaml"), []byte(ledger), 0o644); err != nil {
		t.Fatal(err)
	}
	state := t.TempDir()
	s := New(dir, nil, nil)
	s.SetOwnerQuestionsDir(state)
	s.scanLedgerOwnerBlockers(dir)
	if open, _ := ownerquestionview.New(state).List(true); len(open) != 0 {
		t.Fatalf("device hold paged %+v", open)
	}
	if entries, _, _ := ownerquestions.New(state).Snapshot(); len(entries) != 0 {
		t.Fatalf("outbox %+v", entries)
	}
}

func TestT1042TargetLedgerT449DoesNotDuplicateOwnerGate(t *testing.T) {
	dir := t.TempDir()
	reason, err := ownergate.Record{Question: "Accept the reload?", Evidence: "commit abcdef0123456789", RecordedBy: "po"}.Reason()
	if err != nil {
		t.Fatal(err)
	}
	ledger := `schema_version: "1.0"
targets:
  T383:
    name: ui gate
    status: converging
    owned_by:
      owner: owner
      reason: ` + strings.ReplaceAll(reason, "\n", " ") + `
`
	if err := os.WriteFile(filepath.Join(dir, "bullseye.yaml"), []byte(ledger), 0o644); err != nil {
		t.Fatal(err)
	}
	state := t.TempDir()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "blurter"), []byte("#!/bin/sh\necho spooled\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	s := New(dir, nil, nil)
	s.SetAgentReportDir(state)
	s.SetOwnerQuestionsDir(state)
	if err := s.recordOwnerQuestion(mustOwnerGateQuestion(t, dir, "T383", "Accept the reload?")); err != nil {
		t.Fatal(err)
	}
	s.observeTargetLedgerOwnerBlockers(dir, "T383")
	if open, _ := ownerquestionview.New(state).List(true); len(open) != 1 || open[0].Identity.ID != "owner-gate" {
		t.Fatalf("duplicate intake %+v", open)
	}
}

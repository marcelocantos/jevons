// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0
package mcpserver

import (
	"context"
	"github.com/marcelocantos/jevons/internal/envelope"
	"github.com/marcelocantos/jevons/internal/ownerquestions"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOwnerGateRealHandlerSpoolsOnceAndAnswerCloses(t *testing.T) {
	prev := runBullseye
	t.Cleanup(func() { runBullseye = prev })
	runBullseye = func(...string) (string, error) { return "ok: true", nil }
	dir := t.TempDir()
	executable := filepath.Join(dir, "blurter")
	marker := filepath.Join(dir, "calls")
	// The fake executable tests the real CLI invocation seam without touching
	// the owner's actual notification sinks.
	script := "#!/bin/sh\necho send >> '" + marker + "'\necho /spool/accepted\n"
	if err := os.WriteFile(executable, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	s := New(dir, nil, nil)
	s.stateDir = dir
	repo := t.TempDir()
	req := t720RecordReq(repo, "T22")
	for i := 0; i < 2; i++ {
		result, err := s.handleOwnerGate(context.Background(), req)
		if err != nil || result.IsError || !strings.Contains(targetFileToolText(result), "spooled, NOT owner-delivered") {
			t.Fatalf("record %d: %+v %v", i, result, err)
		}
	}
	b, err := os.ReadFile(marker)
	if err != nil || strings.Count(string(b), "send") != 1 {
		t.Fatalf("duplicate transport invocation: %q %v", b, err)
	}
	entries, _, err := ownerquestions.New(dir).Snapshot()
	if err != nil || len(entries) != 1 || entries[0].Delivered || entries[0].SpoolPath != "/spool/accepted" {
		t.Fatalf("outbox %+v %v", entries, err)
	}
	result, err := s.handleOwnerGate(context.Background(), t720AnswerReq(repo, "T22", "accept"))
	if err != nil || result.IsError {
		t.Fatalf("answer %+v %v", result, err)
	}
	entries, _, err = ownerquestions.New(dir).Snapshot()
	if err != nil || len(entries) != 0 {
		t.Fatalf("answered gate stayed open: %+v %v", entries, err)
	}
}

func TestTypedBlockedReportIntakeSpoolsOnce(t *testing.T) {
	dir := t.TempDir()
	bin := t.TempDir()
	marker := filepath.Join(bin, "calls")
	if err := os.WriteFile(filepath.Join(bin, "blurter"), []byte("#!/bin/sh\necho send >> '"+marker+"'\necho /spool/accepted\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	repo := t.TempDir()
	m := &envelope.Message{Kind: envelope.KindFinishReport, Target: "T1028.1", Status: envelope.ProgressBlocked, Blocker: "waiting on scope", BlockClass: envelope.BlockOwnerDecision, QuestionRepo: repo, QuestionID: "scope", QuestionVersion: "1", Question: "Ship both shapes?", QuestionAsker: "jv-worker", AnswerRoute: "jevons-po", SilentLedger: envelope.SilentLedgerEmpty}
	s := New(repo, nil, nil)
	s.SetAgentReportDir(dir)
	for i := 0; i < 2; i++ {
		s.storeAgentReport("worker", envelope.Format(m))
	}
	b, err := os.ReadFile(marker)
	if err != nil || strings.Count(string(b), "send") != 1 {
		t.Fatalf("intake did not coalesce: %q %v", b, err)
	}
	entries, _, err := ownerquestions.New(dir).Snapshot()
	if err != nil || len(entries) != 1 || entries[0].Status != "spooled" {
		t.Fatalf("outbox %+v %v", entries, err)
	}
}

package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/ownerquestion"
	"github.com/marcelocantos/jevons/internal/ownerquestions"
	"github.com/marcelocantos/jevons/internal/ownerquestionview"
)

func TestT1042ActualReportProducerReplayAndLifecycle(t *testing.T) {
	raw, err := os.ReadFile("../ownerquestion/testdata/t1041-blocked-report.txt")
	if err != nil {
		t.Fatal(err)
	}
	state := t.TempDir()
	bin := t.TempDir()
	marker := filepath.Join(bin, "calls")
	if err := os.WriteFile(filepath.Join(bin, "blurter"), []byte("#!/bin/sh\necho \"$*\" >> '"+marker+"'\necho /spool/accepted\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	repo := t.TempDir()
	s := New(repo, nil, nil)
	s.SetAgentReportDir(state)
	s.SetOwnerQuestionsDir(state)
	s.storeAgentReport("reviewer", string(raw))
	s.storeAgentReport("reviewer", string(raw))
	b, err := os.ReadFile(marker)
	if err != nil || strings.Count(string(b), "--subject") != 1 || !strings.Contains(string(b), "T1041") || !strings.Contains(string(b), "--link http://localhost:13705/") {
		t.Fatalf("bad spool %q %v", b, err)
	}
	rows, err := ownerquestionview.New(state).List(true)
	if err != nil || len(rows) != 1 || rows[0].Identity.ID != "hardware-visual-review" || rows[0].Review == nil || rows[0].Review.Readiness != ownerquestion.PrerequisiteBlocked || !strings.Contains(rows[0].Text, "Reconnect the Fold") || !strings.Contains(rows[0].Text, "accept/reject") {
		t.Fatalf("view %+v %v", rows, err)
	}
	entries, _, err := ownerquestions.New(state).Snapshot()
	if err != nil || len(entries) != 1 || entries[0].Delivered || entries[0].Status != "spooled" {
		t.Fatalf("outbox %+v %v", entries, err)
	}
	// A changed artifact revises the ask once; a stale replay cannot re-page it.
	revised := strings.ReplaceAll(string(raw), "artifacts/t1041-development-800.png", "artifacts/t1041-development-800-revised.png")
	s.storeAgentReport("reviewer", revised)
	versions, err := ownerquestionview.New(state).List(false)
	if err != nil || len(versions) != 2 || versions[0].State == versions[1].State || strings.Count(string(mustReadT1042(t, marker)), "--subject") != 2 {
		t.Fatalf("revision %+v %v", versions, err)
	}
	s.storeAgentReport("reviewer", string(raw))
	if strings.Count(string(mustReadT1042(t, marker)), "--subject") != 2 {
		t.Fatal("stale replay paged")
	}
	open, err := ownerquestionview.New(state).List(true)
	if err != nil || len(open) != 1 {
		t.Fatalf("revised open %+v %v", open, err)
	}
	rows = open
	b = mustReadT1042(t, marker)
	if err := ownerquestionview.New(state).Resolve(rows[0].Identity, ownerquestionview.Answered, "owner accepted after hardware review"); err != nil {
		t.Fatal(err)
	}
	if err := ownerquestions.New(state).Resolve(entries[0].Question.Key); err != nil {
		t.Fatal(err)
	}
	s.storeAgentReport("reviewer", string(raw))
	if b2, _ := os.ReadFile(marker); string(b2) != string(b) {
		t.Fatalf("answered replay paged again: %q", b2)
	}
	if entries, _, _ := ownerquestions.New(state).Snapshot(); len(entries) != 0 {
		t.Fatalf("answered outbox %+v", entries)
	}
}

func mustReadT1042(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

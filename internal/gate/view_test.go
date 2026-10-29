// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate

import (
	"testing"
	"time"
)

func TestValidRecordID(t *testing.T) {
	ok := []string{"c2b2e23b", "9f13c0a2", "A1", "0"}
	for _, id := range ok {
		if !ValidRecordID(id) {
			t.Errorf("ValidRecordID(%q) = false, want true", id)
		}
	}
	bad := []string{"", "../etc/passwd", "a/b", "id.json", "a b", "id;rm"}
	for _, id := range bad {
		if ValidRecordID(id) {
			t.Errorf("ValidRecordID(%q) = true, want false", id)
		}
	}
}

func TestViewOfRoundTripsCommandStatusVerdictTree(t *testing.T) {
	start := time.Date(2026, 9, 20, 20, 0, 0, 0, time.UTC)
	rec := &Record{
		ID:          "c2b2e23b",
		Name:        "make-test-go",
		Command:     []string{"make", "test-go"},
		Dir:         "/tmp/repo",
		Started:     start,
		Ended:       start.Add(42 * time.Second),
		ExitStatus:  0,
		StatusKnown: true,
		Verdict:     VerdictGreen,
		OutputPath:  "/tmp/jevons/gates/c2b2e23b.log",
		Tree: &TreeProvenance{
			Commit:     "ac753cbbdeadbeef",
			Clean:      false,
			DirtyFiles: 3,
		},
	}
	v := ViewOf(rec)
	if !v.Found {
		t.Fatal("ViewOf set found=false")
	}
	if v.Status != "0" || v.Verdict != VerdictGreen {
		t.Fatalf("status/verdict = %s %s", v.Status, v.Verdict)
	}
	if len(v.Command) != 2 || v.Command[0] != "make" || v.Command[1] != "test-go" {
		t.Fatalf("command = %#v", v.Command)
	}
	if v.Tree == nil || v.Tree.Clean || v.Tree.DirtyFiles != 3 || v.Tree.Commit != "ac753cbbdeadbeef" {
		t.Fatalf("tree = %+v", v.Tree)
	}
	if v.Attestation == "" || v.ID != rec.ID {
		t.Fatalf("attestation=%q id=%q", v.Attestation, v.ID)
	}
	if v.Duration != "42s" {
		t.Fatalf("duration=%q want 42s", v.Duration)
	}
	if v.OutputPath != rec.OutputPath {
		t.Fatalf("output_path=%q", v.OutputPath)
	}
}

func TestNotFoundViewIsDistinguishable(t *testing.T) {
	n := NewNotFoundView("932855c1")
	if n.Error != ErrNotFound || n.Found || n.ID != "932855c1" {
		t.Fatalf("not-found view = %+v", n)
	}
}

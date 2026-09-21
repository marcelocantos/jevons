// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package commitattrib

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseMessageReadsTrailers(t *testing.T) {
	t.Parallel()
	body := "fix(T760): post-reap attribution\n\nJevons-Actor: jv-t760-commit-attribution\nJevons-Target: T760, T748\n"
	rec := ParseMessage(body)
	if rec.Actor != "jv-t760-commit-attribution" {
		t.Fatalf("actor = %q", rec.Actor)
	}
	if len(rec.Targets) != 2 || rec.Targets[0] != "T760" || rec.Targets[1] != "T748" {
		t.Fatalf("targets = %v", rec.Targets)
	}
}

func TestHasProductChanges(t *testing.T) {
	t.Parallel()
	if HasProductChanges([]string{"bullseye.yaml"}) {
		t.Fatal("ledger-only is not product")
	}
	if !HasProductChanges([]string{"internal/foo.go", "bullseye.yaml"}) {
		t.Fatal("mixed commit has product changes")
	}
}

func TestStampFileFromEnv(t *testing.T) {
	t.Setenv(ActorEnv, "jevons-po")
	t.Setenv(TargetEnv, "T98")
	dir := t.TempDir()
	msg := filepath.Join(dir, "msg")
	if err := WriteMessageFile(msg, "ledger: achieve T98\n"); err != nil {
		t.Fatal(err)
	}
	if err := StampFile(msg); err != nil {
		t.Fatal(err)
	}
	got, err := ReadMessageFile(msg)
	if err != nil {
		t.Fatal(err)
	}
	rec := ParseMessage(got)
	if rec.Actor != "jevons-po" {
		t.Fatalf("actor = %q", rec.Actor)
	}
	if len(rec.Targets) != 1 || rec.Targets[0] != "T98" {
		t.Fatalf("targets = %v", rec.Targets)
	}
}

func TestStampFileSkipsWhenAlreadyDeclared(t *testing.T) {
	t.Setenv(ActorEnv, "jevons-po")
	msg := filepath.Join(t.TempDir(), "msg")
	body := "ledger\n\nJevons-Actor: cl-t96-worker\n"
	if err := os.WriteFile(msg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := StampFile(msg); err != nil {
		t.Fatal(err)
	}
	got, _ := ReadMessageFile(msg)
	if rec := ParseMessage(got); rec.Actor != "cl-t96-worker" {
		t.Fatalf("overwrote actor: %q", rec.Actor)
	}
}

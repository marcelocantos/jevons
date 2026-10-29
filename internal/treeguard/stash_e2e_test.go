// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package treeguard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/commitattrib"
)

// 🎯T1134: git stash push in the shared clone from a fleet seat must be
// refused and must name the dirty paths stash would take.
func TestGitStashPushRefusedWhileSharedCloneDirty(t *testing.T) {
	dir := ledgerRepo(t)
	t.Setenv(commitattrib.ActorEnv, "jv-t1134-auto")
	if err := os.WriteFile(filepath.Join(dir, "bullseye.yaml"),
		[]byte("schema_version: 5\ntargets:\n  T1133:\n    name: filed\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	env := &Env{Store: &Store{Root: t.TempDir()}, RepoRoot: dir, Now: time.Now}
	p := &Payload{ToolName: ToolBash}
	p.ToolInput.Command = "git stash push -m integrate: temp stash"

	d, err := env.Pre(p)
	if err != nil {
		t.Fatal(err)
	}
	if d.Verdict != Deny {
		t.Fatalf("want Deny, got %v (%s)", d.Verdict, d.Reason)
	}
	if !strings.Contains(d.Message, "bullseye.yaml") {
		t.Fatalf("denial must name dirty paths: %q", d.Message)
	}
}

func TestGitStashListAllowedWithDirtyLedger(t *testing.T) {
	dir := ledgerRepo(t)
	t.Setenv(commitattrib.ActorEnv, "jv-t1134-auto")
	if err := os.WriteFile(filepath.Join(dir, "bullseye.yaml"),
		[]byte("schema_version: 5\ntargets:\n  T1133:\n    name: filed\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	env := &Env{Store: &Store{Root: t.TempDir()}, RepoRoot: dir, Now: time.Now}
	p := &Payload{ToolName: ToolBash}
	p.ToolInput.Command = "git stash list"

	d, err := env.Pre(p)
	if err != nil {
		t.Fatal(err)
	}
	if d.Verdict != Allow {
		t.Fatalf("stash list is read-only: %v %s", d.Verdict, d.Message)
	}
}

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

// 🎯T1149: every hand-rolled stash shape fleet workers use must trip the same
// shared-clone refusal as `git stash push` when paths are dirty.
func TestGitStashFormsRefusedWhileSharedCloneDirty(t *testing.T) {
	forms := []string{
		"git stash",
		"git stash -m integrate: temp",
		"git stash push -m integrate: temp stash",
		"git stash save integrate: legacy save",
		"git stash pop",
		"git stash apply",
	}
	for _, cmd := range forms {
		t.Run(cmd, func(t *testing.T) {
			dir := ledgerRepo(t)
			t.Setenv(commitattrib.ActorEnv, "jv-t1149-auto")
			if err := os.WriteFile(filepath.Join(dir, "bullseye.yaml"),
				[]byte("schema_version: 5\ntargets:\n  T1133:\n    name: filed\n"), 0o644); err != nil {
				t.Fatal(err)
			}

			env := &Env{Store: &Store{Root: t.TempDir()}, RepoRoot: dir, Now: time.Now}
			p := &Payload{ToolName: ToolBash}
			p.ToolInput.Command = cmd

			d, err := env.Pre(p)
			if err != nil {
				t.Fatal(err)
			}
			if d.Verdict != Deny {
				t.Fatalf("want Deny, got %v (%s) %s", d.Verdict, d.Reason, d.Message)
			}
			if !strings.Contains(d.Message, "bullseye.yaml") {
				t.Fatalf("denial must name dirty paths: %q", d.Message)
			}
		})
	}
}

func TestScanCommandStashForms(t *testing.T) {
	wantPush := []string{
		"git stash",
		"git stash -m msg",
		"git stash push",
		"git stash save",
		"git stash pop",
		"git stash apply",
	}
	for _, cmd := range wantPush {
		if !scanHasStashPush(cmd) {
			t.Errorf("%q should be FormGitStashPush", cmd)
		}
	}
	for _, cmd := range []string{"git stash list", "git stash show"} {
		if scanHasStashPush(cmd) {
			t.Errorf("%q should not be FormGitStashPush", cmd)
		}
	}
}

func scanHasStashPush(command string) bool {
	for _, w := range ScanCommand(command) {
		if w.Form == FormGitStashPush {
			return true
		}
	}
	return false
}

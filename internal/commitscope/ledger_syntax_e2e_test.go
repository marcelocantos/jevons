// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package commitscope_test

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestLedgerSyntaxHookReadsTheCommitIndex(t *testing.T) {
	r := newGuardedRepo(t)
	const valid = "schema_version: 5\ntargets: {}\n"
	const invalid = "targets: [\n"
	r.write(t, "bullseye.yaml", valid)
	if out, err := r.git(t, "add", "bullseye.yaml"); err != nil {
		t.Fatalf("seed add: %v\n%s", err, out)
	}
	if out, err := r.git(t, "commit", "--only", "bullseye.yaml", "-m", "seed ledger"); err != nil {
		t.Fatalf("seed: %v\n%s", err, out)
	}
	private := filepath.Join(r.dir, ".git", "index-ledger-syntax")
	env := []string{"GIT_INDEX_FILE=" + private}
	must := func(args ...string) string {
		t.Helper()
		out, err := r.gitEnv(t, env, args...)
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return out
	}
	must("read-tree", "HEAD")
	r.write(t, "bullseye.yaml", invalid)
	must("add", "bullseye.yaml")
	r.write(t, "bullseye.yaml", valid)
	before := r.head(t)
	out, err := r.gitEnv(t, env, "commit", "-m", "invalid staged ledger")
	if err == nil || !strings.Contains(out, "invalid staged bullseye.yaml YAML") || r.head(t) != before {
		t.Fatalf("invalid index bypassed by valid working tree: %v\n%s", err, out)
	}
	// The inverse: valid index content is judged on its own, even with a
	// concurrent malformed working-tree write waiting behind it.
	const repaired = "schema_version: 5\ntargets: {}\n# repaired\n"
	r.write(t, "bullseye.yaml", repaired)
	must("add", "bullseye.yaml")
	r.write(t, "bullseye.yaml", invalid)
	must("commit", "-m", "valid staged ledger")
	if got := must("show", "HEAD:bullseye.yaml"); got != repaired+"\n" {
		t.Fatalf("committed working-tree bytes: %q", got)
	}
	// Scoped commits must not inspect a foreign malformed shared-index row.
	if out, err := r.git(t, "add", "bullseye.yaml"); err != nil {
		t.Fatalf("stage foreign malformed ledger: %v\n%s", err, out)
	}
	r.write(t, "notes.txt", "unrelated change\n")
	if out, err := r.git(t, "add", "notes.txt"); err != nil {
		t.Fatalf("stage note: %v\n%s", err, out)
	}
	if out, err := r.git(t, "commit", "--only", "notes.txt", "-m", "unrelated note"); err != nil {
		t.Fatalf("unrelated scoped commit refused: %v\n%s", err, out)
	}
	if out, err := r.git(t, "show", ":bullseye.yaml"); err != nil || out != invalid+"\n" {
		t.Fatalf("foreign index row was altered: %q %v", out, err)
	}
}

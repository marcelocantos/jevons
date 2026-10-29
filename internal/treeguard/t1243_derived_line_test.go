// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package treeguard

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 🎯T1243: a bullseye tool write rewrites the ledger's content_hash header.
// The old hash line vanishing is not a loss; a real dropped row still is.
func TestT1243SweepIgnoresRegeneratedContentHash(t *testing.T) {
	repo := t.TempDir()
	store := &Store{Root: t.TempDir()}
	env := &Env{Store: store, RepoRoot: repo, Guarded: []string{"bullseye.yaml"}}
	ledger := filepath.Join(repo, "bullseye.yaml")
	write := func(s string) {
		if err := os.WriteFile(ledger, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()

	write("# content_hash: sha256:aaaa\ntargets:\n  T1: one\n")
	if _, err := env.Sweep(now); err != nil {
		t.Fatal(err)
	}
	write("# content_hash: sha256:bbbb\ntargets:\n  T1: one\n  T2: two\n")
	got, err := env.Sweep(now)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("regenerated content_hash reported as a loss: %+v", got)
	}

	write("# content_hash: sha256:bbb2\ntargets:\n  T1: one\n  T2: |-\n    two\n\n    more\n")
	got, err = env.Sweep(now)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("a field reflowed into a block scalar reported as a loss: %+v", got)
	}

	write("# content_hash: sha256:cccc\ntargets:\n  T2: |-\n    two\n\n    more\n")
	got, err = env.Sweep(now)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || len(got[0].Lost) != 1 || got[0].Lost[0] != "T1: one" {
		t.Fatalf("dropped row not reported alone: %+v", got)
	}
}

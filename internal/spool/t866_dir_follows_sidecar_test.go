// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package spool

import (
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia/omp"
)

// The reader follows the sidecar's own spool directory, so a claudia
// default change cannot leave Jevons reading a dead path (2026-10-22).
func TestDirFollowsTheSidecarDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv(DirEnv, "")
	t.Setenv("CLAUDIA_OMP_SPOOL_DIR", "")
	if got, want := Dir(), omp.SpoolDir(); got != want {
		t.Fatalf("Dir() = %q, want the sidecar's %q", got, want)
	}
	if got := Dir(); got == filepath.Join(home, ".jevons", "spool") {
		t.Fatalf("Dir() still defaults to the retired ~/.jevons/spool")
	}
	t.Setenv("CLAUDIA_OMP_SPOOL_DIR", "/tmp/sidecar-spool")
	if got := Dir(); got != "/tmp/sidecar-spool" {
		t.Fatalf("Dir() = %q, want CLAUDIA_OMP_SPOOL_DIR", got)
	}
	t.Setenv(DirEnv, "/tmp/jevons-spool")
	if got := Dir(); got != "/tmp/jevons-spool" {
		t.Fatalf("Dir() = %q, want JEVONS_SPOOL_DIR to win", got)
	}
}

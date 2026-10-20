// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 🎯T1090: an isolate daemon resolves no $HOME/.jevons path. The jobs
// registry and the owner role overlay used to join the home directory
// directly, so a journey daemon with its own state_dir read and wrote the
// development daemon's tree.

// t1090Universe gives each test a scratch HOME (so a stray home join is
// visible and harmless) and clears the roles env override.
func t1090Universe(t *testing.T) (home string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("JEVONS_ROLES_DIR", "")
	return home
}

func t1090AssertNoHomeJevons(t *testing.T, home string, paths ...string) {
	t.Helper()
	devTree := filepath.Join(home, ".jevons")
	for _, p := range paths {
		if p != "" && (p == devTree || strings.HasPrefix(p, devTree+string(filepath.Separator))) {
			t.Errorf("resolved %s under the development tree %s", p, devTree)
		}
	}
	if _, err := os.Stat(devTree); !os.IsNotExist(err) {
		t.Errorf("%s exists after the isolate ran (stat err=%v)", devTree, err)
	}
}

func TestT1090IsolateStatePathsFollowStateDir(t *testing.T) {
	home := t1090Universe(t)
	state := t.TempDir()
	s := New(state, nil, nil)
	s.SetDaemonStateDir(state)
	if err := s.OpenRoleAssignments(state); err != nil {
		t.Fatal(err)
	}

	reg := s.jobsRegistry()
	if want := filepath.Join(state, "jobs"); reg.dir != want {
		t.Fatalf("jobs dir = %q, want %q", reg.dir, want)
	}
	j := reg.start("jevons_t1090_probe", "t1090", func(context.Context) (string, error) { return "ok", nil })
	reg.await(j, 5*time.Second)
	record := reg.path(j.ID)
	if _, err := os.Stat(record); err != nil {
		t.Fatalf("job record not written under state_dir: %v", err)
	}

	owner := s.rolesCatalog("").OwnerDir
	if want := filepath.Join(state, "roles"); owner != want {
		t.Fatalf("role overlay = %q, want %q", owner, want)
	}
	// The overlay is live, not just a string: an override written under
	// state_dir/roles wins over the builtin.
	if err := os.MkdirAll(owner, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nrole: auditor\npurpose: work\nreadonly: true\nsummary: t1090\n---\n\nT1090 isolate override.\n"
	if err := os.WriteFile(filepath.Join(owner, "auditor.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	def, err := s.resolveRoleDef("auditor")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(def.Body, "T1090 isolate override") {
		t.Fatalf("override under state_dir/roles not resolved; got body %q", def.Body)
	}

	t1090AssertNoHomeJevons(t, home, reg.dir, record, owner)
}

// A Server that never learned its state dir (hermetic construction) keeps
// jobs in memory and has no owner overlay. It does not fall back to the
// development tree.
func TestT1090NoStateDirBorrowsNothingFromHome(t *testing.T) {
	home := t1090Universe(t)
	s := New(t.TempDir(), nil, nil)

	reg := s.jobsRegistry()
	if reg.dir != "" {
		t.Fatalf("jobs dir = %q, want memory-only", reg.dir)
	}
	j := reg.start("jevons_t1090_probe", "t1090", func(context.Context) (string, error) { return "ok", nil })
	reg.await(j, 5*time.Second)

	owner := s.rolesCatalog("").OwnerDir
	if owner != "" {
		t.Fatalf("role overlay = %q, want none", owner)
	}
	t1090AssertNoHomeJevons(t, home, reg.dir, owner)
}

// The development daemon's state_dir IS ~/.jevons, so it keeps the paths it
// always had. The fix moved the root, not the development layout.
func TestT1090DevelopmentStateDirKeepsItsPaths(t *testing.T) {
	home := t1090Universe(t)
	dev := filepath.Join(home, ".jevons")
	s := New(t.TempDir(), nil, nil)
	s.SetDaemonStateDir(dev)

	if got, want := s.jobsRegistry().dir, filepath.Join(dev, "jobs"); got != want {
		t.Fatalf("jobs dir = %q, want %q", got, want)
	}
	if got, want := s.rolesCatalog("").OwnerDir, filepath.Join(dev, "roles"); got != want {
		t.Fatalf("role overlay = %q, want %q", got, want)
	}
}

// JEVONS_ROLES_DIR still overrides, for isolates and development alike.
func TestT1090RolesEnvOverrideWins(t *testing.T) {
	t1090Universe(t)
	override := t.TempDir()
	t.Setenv("JEVONS_ROLES_DIR", override)
	s := New(t.TempDir(), nil, nil)
	s.SetDaemonStateDir(t.TempDir())
	if got := s.rolesCatalog("").OwnerDir; got != override {
		t.Fatalf("role overlay = %q, want env override %q", got, override)
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestT838MaterializeExclusiveGrokHome(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	t.Setenv("HOME", t.TempDir())
	sid := "4b7806bc-session"
	if err := MaterializeExclusiveGrokHome(sid); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(state, "claudia", "grok-homes", sid)
	if _, err := os.Stat(home); err != nil {
		t.Fatalf("home missing: %v", err)
	}
	cfg := filepath.Join(home, "config.toml")
	body, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "compat.claude") {
		t.Fatalf("config.toml missing compat stanza: %q", body)
	}
	if err := MaterializeExclusiveGrokHome(sid); err != nil {
		t.Fatalf("second materialize: %v", err)
	}
}

func TestT838MaterializeRefusesBadSessionPath(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if err := MaterializeExclusiveGrokHome("../escape"); err == nil {
		t.Fatal("wanted invalid session refusal")
	}
}

func TestT838MaterializeRefusesUnusableHome(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	sid := "dangling"
	home := filepath.Join(state, "claudia", "grok-homes", sid)
	if err := os.MkdirAll(filepath.Dir(home), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(home, []byte("not a dir"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := MaterializeExclusiveGrokHome(sid)
	if err == nil || !strings.Contains(err.Error(), "exclusive GROK_HOME is not a directory") {
		t.Fatalf("err=%v want not-a-directory refusal", err)
	}
}

// The real fleet caller must lay down the destination home before claudia
// changes the seat. A helper-only test cannot catch an unreachable guard.
func TestT838SwitchMaterializesHomeBeforeProviderChange(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	t.Setenv("HOME", t.TempDir())
	f, reg := switchFixture(t, nil)
	before := reg.Def("jevons-po")
	if _, err := f.SwitchModel("jevons-po", "grok", "grok-4", false); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(state, "claudia", "grok-homes", before.SessionID)
	if _, err := os.Stat(filepath.Join(home, "config.toml")); err != nil {
		t.Fatalf("switch did not materialize destination home: %v", err)
	}
	if after := reg.Def("jevons-po"); after.SessionID != before.SessionID || after.Provider == before.Provider {
		t.Fatalf("switch lost session or did not move: before=%+v after=%+v", before, after)
	}
}

func TestT838SwitchRefusesUnusableHomeBeforeChangingSeat(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	f, reg := switchFixture(t, nil)
	before := *reg.Def("jevons-po")
	home := filepath.Join(state, "claudia", "grok-homes", before.SessionID)
	if err := os.MkdirAll(filepath.Dir(home), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(home, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.SwitchModel("jevons-po", "grok", "grok-4", false); err == nil || !strings.Contains(err.Error(), "exclusive GROK_HOME") {
		t.Fatalf("switch error = %v, want named home refusal", err)
	}
	if after := reg.Def("jevons-po"); after.Provider != before.Provider || after.SessionID != before.SessionID {
		t.Fatalf("failed switch rewrote seat: before=%+v after=%+v", before, after)
	}
}

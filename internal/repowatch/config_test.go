// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package repowatch

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if !cfg.Enabled {
		t.Fatal("default enabled")
	}
	if cfg.IntervalDuration() != DefaultInterval {
		t.Fatalf("interval: got %v", cfg.IntervalDuration())
	}
	if cfg.EffectiveMaxItemsPerRepo() != DefaultMaxItemsPerRepo {
		t.Fatalf("max items: got %d", cfg.EffectiveMaxItemsPerRepo())
	}
}

func TestLoadSaveConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultConfig()
	cfg.IntervalSec = 900
	cfg.Repos = []RepoConfig{{Repo: "marcelocantos/mnemo", Workdir: "/tmp/mnemo", PO: "mnemo-po"}}
	if err := SaveConfig(dir, cfg); err != nil {
		t.Fatal(err)
	}
	got, err := LoadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.IntervalSec != 900 || len(got.Repos) != 1 || got.Repos[0].Repo != "marcelocantos/mnemo" {
		t.Fatalf("round-trip: %+v", got)
	}
}

func TestLoadConfigMissingIsDefault(t *testing.T) {
	got, err := LoadConfig(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !got.Enabled {
		t.Fatal("missing file should default enabled")
	}
}

func TestLoadConfigMalformedIsHardError(t *testing.T) {
	dir := t.TempDir()
	path := ConfigPath(dir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not-json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(dir); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestPatchConfigSetAndRemoveRepo(t *testing.T) {
	dir := t.TempDir()
	off := false
	cfg, err := PatchConfig(dir, ConfigPatch{
		SetRepo: &RepoConfig{Repo: "a/b", Workdir: "/w", PO: "a-po", Enabled: &off},
		UpdatedBy: "test",
		Now:       time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Repos) != 1 || cfg.Repos[0].EnabledOrDefault() {
		t.Fatalf("set: %+v", cfg.Repos)
	}
	if cfg.UpdatedBy != "test" || cfg.UpdatedAt == "" {
		t.Fatalf("audit: %+v", cfg)
	}
	cfg, err = PatchConfig(dir, ConfigPatch{RemoveRepo: "a/b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Repos) != 0 {
		t.Fatalf("remove left %v", cfg.Repos)
	}
}

func TestRepoByNameCaseInsensitive(t *testing.T) {
	cfg := Config{Repos: []RepoConfig{{Repo: "MarceloCantos/Jevons"}}}
	if _, ok := cfg.RepoByName("marcelocantos/jevons"); !ok {
		t.Fatal("case-insensitive lookup")
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/marcelocantos/jevons/internal/config"
	"github.com/marcelocantos/jevons/internal/provider"
)

// 🎯T27.2 oracle: config reload updates desired set without process restart.

func writeCfg(t *testing.T, dir, body string) string {
	t.Helper()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestConfigManagerLoadAndReload(t *testing.T) {
	dir := t.TempDir()
	path := writeCfg(t, dir, `
providers:
  - id: mnemo
    transport: connect
    params:
      url: http://127.0.0.1:8741
`)
	store, err := provider.OpenStore(filepath.Join(dir, "registry.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	var reloads atomic.Int32
	m, err := provider.NewConfigManager(provider.ConfigManagerArgs{
		ConfigPath: path,
		Store:      store,
		OnChange: func(d []config.ProviderDecl) {
			reloads.Add(1)
			if len(d) < 1 {
				t.Error("OnChange empty")
			}
		},
	})
	if err != nil {
		t.Fatalf("NewConfigManager: %v", err)
	}
	if reloads.Load() != 1 {
		t.Fatalf("initial OnChange count=%d", reloads.Load())
	}
	want := m.Desired()
	if len(want) != 1 || want[0].ID != "mnemo" || !want[0].Enabled() {
		t.Fatalf("desired=%+v", want)
	}
	en := m.Enabled()
	if len(en) != 1 {
		t.Fatalf("enabled=%+v", en)
	}
	// Store mirrored.
	rows, err := store.ListDesired()
	if err != nil || len(rows) != 1 || rows[0].ID != "mnemo" {
		t.Fatalf("store=%+v err=%v", rows, err)
	}

	// Mid-process reload: rewrite config (simulate owner edit) and Reload.
	writeCfg(t, dir, `
providers:
  - id: mnemo
    transport: connect
    enable: false
    params:
      url: http://127.0.0.1:8741
  - id: pulse
    transport: launch
    params:
      exec: /bin/pulse
      argv: ["--serve"]
`)
	if err := m.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if reloads.Load() != 2 {
		t.Fatalf("after reload OnChange count=%d", reloads.Load())
	}
	got := m.Desired()
	if len(got) != 2 {
		t.Fatalf("after reload desired len=%d", len(got))
	}
	byID := map[string]config.ProviderDecl{}
	for _, d := range got {
		byID[d.ID] = d
	}
	if byID["mnemo"].Enabled() {
		t.Fatal("mnemo should be disabled after reload")
	}
	if byID["pulse"].LaunchExec() != "/bin/pulse" {
		t.Fatalf("pulse=%+v", byID["pulse"])
	}
	if len(m.Enabled()) != 1 || m.Enabled()[0].ID != "pulse" {
		t.Fatalf("enabled after reload=%+v", m.Enabled())
	}
	rows, err = store.ListDesired()
	if err != nil || len(rows) != 2 {
		t.Fatalf("store after reload=%+v err=%v", rows, err)
	}
}

func TestConfigManagerReloadFailsClosed(t *testing.T) {
	dir := t.TempDir()
	path := writeCfg(t, dir, `
providers:
  - id: ok
    transport: connect
    params:
      url: http://x
`)
	store, err := provider.OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	m, err := provider.NewConfigManager(provider.ConfigManagerArgs{
		ConfigPath: path,
		Store:      store,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Bad config must not clobber previous desired set.
	writeCfg(t, dir, `
providers:
  - id: bad
    transport: connect
    params: {}
`)
	if err := m.Reload(); err == nil {
		t.Fatal("expected validation error")
	}
	got := m.Desired()
	if len(got) != 1 || got[0].ID != "ok" {
		t.Fatalf("desired clobbered on failed reload: %+v", got)
	}
}

func TestConfigManagerEmptyProviders(t *testing.T) {
	dir := t.TempDir()
	path := writeCfg(t, dir, "owner_name: Ada\n")
	store, err := provider.OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	m, err := provider.NewConfigManager(provider.ConfigManagerArgs{
		ConfigPath: path,
		Store:      store,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Desired()) != 0 {
		t.Fatalf("want empty, got %+v", m.Desired())
	}
}

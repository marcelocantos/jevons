// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0
package upgrade

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/cli"
	"github.com/marcelocantos/jevons/internal/seatreg"
)

// This is NOT a production Claude CLI journey. It exercises the boot seam
// against a surviving process in a private tmux socket, so the shared fleet,
// grants and development daemon are never touched. Adoption is stubbed.
func TestT1047PrivateTmuxSurvivesRegistryReloadWithoutTransportRelabel(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux unavailable")
	}
	dir, err := os.MkdirTemp("/tmp", "t1047-fixture-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	socket := filepath.Join(dir, "tmux.sock")
	t.Setenv("CLAUDIA_TMUX_SOCKET", socket)
	t.Setenv("CLAUDIA_NO_BROKER", "1")
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("tmux", append([]string{"-S", socket}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("tmux %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("new-session", "-d", "-s", "t1047", "sleep 90")
	t.Cleanup(func() { exec.Command("tmux", "-S", socket, "kill-server").Run() })
	win := run("display-message", "-p", "-t", "t1047", "#{window_id}")
	panePID, err := strconv.Atoi(run("display-message", "-p", "-t", "t1047", "#{pane_pid}"))
	if err != nil || panePID < 1 {
		t.Fatalf("pane pid=%d err=%v", panePID, err)
	}
	path := filepath.Join(t.TempDir(), "agents.json")
	original, err := claudia.NewRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	const name, sid = "isolated-legacy-cli", "fixture-session"
	if err := original.Register(claudia.AgentDef{Name: name, Provider: claudia.ProviderClaude, SessionID: sid, WorkDir: t.TempDir(), AutoStart: true}); err != nil {
		t.Fatal(err)
	}
	// A new coordinator reloads the persisted registry while the old tmux
	// process remains alive. Its handoff carries the actual private window.
	reg, err := claudia.NewRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	reg.SetDirect(true)
	h := Handle{Name: name, Provider: "claude", SessionID: sid, TmuxWindowID: win, Alive: true}
	if !HandoffTransportAlive(h) {
		t.Fatal("live private tmux handoff not preserved")
	}
	def := reg.Def(name)
	preserve := map[string]bool{}
	if def.Provider == claudia.Provider(h.Provider) && def.Provider != cli.SidecarLaunchProvider(def.Provider) && HandoffTransportAlive(h) {
		preserve[name] = true
	}
	if n, err := seatreg.RemintRegistryExcept(reg, t.TempDir(), preserve); err != nil || n != 0 {
		t.Fatalf("early remint n=%d err=%v", n, err)
	}
	starts, adopts := 0, 0
	reg.SetLaunchers(&claudia.RegistryLaunchers{
		Start: func(ctx context.Context, cfg claudia.Config) (*claudia.Agent, error) {
			starts++
			return claudia.StartStub(ctx, cfg, nil)
		},
		Adopt: func(cfg claudia.Config) (*claudia.Agent, error) {
			adopts++
			if cfg.Provider != claudia.ProviderClaude || cfg.SessionID != sid {
				t.Fatalf("adopt config: provider=%s session=%s", cfg.Provider, cfg.SessionID)
			}
			return claudia.StartStub(context.Background(), cfg, nil)
		},
	})
	if _, err := reg.AdoptOrLaunch(name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reg.Stop(name) })
	if starts != 0 || adopts != 1 {
		t.Fatalf("starts=%d adopts=%d", starts, adopts)
	}
	if n, err := seatreg.RemintRegistryExcept(reg, t.TempDir(), nil); err != nil || n != 0 {
		t.Fatalf("post-adopt remint n=%d err=%v", n, err)
	}
	disk, err := claudia.NewRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	if d := disk.Def(name); d.Provider != claudia.ProviderClaude || d.SessionID != sid {
		t.Fatalf("persisted transport changed: %+v", d)
	}
	if got := run("display-message", "-p", "-t", win, "#{pane_pid}"); got != strconv.Itoa(panePID) {
		t.Fatalf("surviving pane PID changed %d -> %s", panePID, got)
	}
}

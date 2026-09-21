// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package upgrade

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
)

func TestT541_1InstallGuardNilSafe(t *testing.T) {
	InstallCursorLaunchGuard(nil)
}

func TestT541_1GuardSkipsNonCursor(t *testing.T) {
	starts := 0
	start := GuardCursorStart(func(ctx context.Context, cfg claudia.Config) (*claudia.Agent, error) {
		starts++
		return claudia.StartStub(ctx, cfg, nil)
	})
	a, err := start(context.Background(), claudia.Config{
		Provider: claudia.ProviderGrok, WorkDir: t.TempDir(), TermLogPath: "-",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Stop)
	if starts != 1 {
		t.Fatalf("starts = %d", starts)
	}
}

func TestT541_1GuardRefusesLaunchWhileLeftoverHoldsStore(t *testing.T) {
	prev, prevNamed := waitCursorStoreClear, namedCursorLeftovers
	t.Cleanup(func() { waitCursorStoreClear, namedCursorLeftovers = prev, prevNamed })
	waitCursorStoreClear = func(string, int) bool { return false }
	namedCursorLeftovers = func(string) ([]int, bool) { return []int{424242}, true }

	starts := 0
	start := GuardCursorStart(func(ctx context.Context, cfg claudia.Config) (*claudia.Agent, error) {
		starts++
		return claudia.StartStub(ctx, cfg, nil)
	})
	_, err := start(context.Background(), claudia.Config{
		Provider: claudia.ProviderCursor, SessionID: "sid-held",
		WorkDir: t.TempDir(), TermLogPath: "-",
	})
	if !claudia.IsCursorResumeDenied(err) {
		t.Fatalf("err = %v, want ErrCursorResumeDenied", err)
	}
	if starts != 0 {
		t.Fatal("Launch minted a second ACP client on a held store")
	}
}

func TestT541_1GuardStartsOnceLeftoverGone(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX leftover")
	}
	sid, leftover := startCursorStoreHolder(t)

	starts := 0
	start := GuardCursorStart(func(ctx context.Context, cfg claudia.Config) (*claudia.Agent, error) {
		starts++
		if storeHeldBy(sid, leftover) {
			t.Fatal("Start ran while leftover still held store.db")
		}
		return claudia.StartStub(ctx, cfg, nil)
	})
	a, err := start(context.Background(), claudia.Config{
		Provider: claudia.ProviderCursor, SessionID: sid,
		ConnectPID: leftover, WorkDir: t.TempDir(), TermLogPath: "-",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Stop)
	if starts != 1 {
		t.Fatalf("starts = %d", starts)
	}
	if storeHeldBy(sid, leftover) {
		t.Fatalf("leftover pid %d still holds store.db after Launch", leftover)
	}
	if !a.Alive() {
		t.Fatal("bound process is not Alive")
	}
}

func TestT541_1AdoptOrLaunchDoesNotStackWriters(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX leftover")
	}
	sid, leftover := startCursorStoreHolder(t)

	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg.SetDirect(true)
	starts := 0
	reg.SetLaunchers(&claudia.RegistryLaunchers{
		Start: GuardCursorStart(func(ctx context.Context, cfg claudia.Config) (*claudia.Agent, error) {
			starts++
			if storeHeldBy(sid, leftover) {
				t.Fatal("fallback Launch ran while leftover held store.db")
			}
			return claudia.StartStub(ctx, cfg, nil)
		}),
	})
	if err := reg.Register(claudia.AgentDef{
		Name: "jevons", WorkDir: t.TempDir(), SessionID: sid,
		Provider: claudia.ProviderCursor, ConnectPID: leftover,
		AutoStart: true, TermLogPath: "-",
	}); err != nil {
		t.Fatal(err)
	}

	a, err := reg.AdoptOrLaunch("jevons")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reg.StopAll)
	if starts != 1 {
		t.Fatalf("starts = %d (want one Launch after leftover gone)", starts)
	}
	if storeHeldBy(sid, leftover) {
		t.Fatalf("leftover pid %d still holds store.db — second writer", leftover)
	}
	if a == nil || !a.Alive() {
		t.Fatal("successor is not bound")
	}
	if n := len(listStoreWriterPIDs(claudia.CursorACPStorePath(sid))); n > 1 {
		t.Fatalf("store.db writers = %d, want at most the successor", n)
	}
}

func TestT541_1ReattachFleetFailLoudDisablesAutoStart(t *testing.T) {
	prevWait, prevOrphan, prevNamed := waitCursorStoreClear, reapOrphanCursorACP, namedCursorLeftovers
	t.Cleanup(func() {
		waitCursorStoreClear = prevWait
		reapOrphanCursorACP = prevOrphan
		namedCursorLeftovers = prevNamed
	})
	waitCursorStoreClear = func(string, int) bool { return false }
	namedCursorLeftovers = func(string) ([]int, bool) { return []int{424242}, true }
	reapOrphanCursorACP = func() []int { return nil }

	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	starts := 0
	reg.SetLaunchers(&claudia.RegistryLaunchers{
		Start: func(ctx context.Context, cfg claudia.Config) (*claudia.Agent, error) {
			starts++
			return claudia.StartStub(ctx, cfg, nil)
		},
	})
	if err := reg.Register(claudia.AgentDef{
		Name: "jevons-po", WorkDir: t.TempDir(), SessionID: "sid-po",
		Provider: claudia.ProviderCursor, ConnectPID: 424242,
		AutoStart: true, TermLogPath: "-",
	}); err != nil {
		t.Fatal(err)
	}

	ReattachFleet(reg)
	if def := reg.Def("jevons-po"); def == nil || def.AutoStart {
		t.Fatalf("AutoStart still on after leftover survived reap: %+v", def)
	}
	if starts != 0 {
		t.Fatal("PreferAdopt minted a second client on a held store")
	}
}

func TestT541_1ReattachFleetReapsLiveLeftover(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX leftover")
	}
	sid, leftover := startCursorStoreHolder(t)

	prevOrphan := reapOrphanCursorACP
	t.Cleanup(func() { reapOrphanCursorACP = prevOrphan })
	reapOrphanCursorACP = func() []int { return nil }

	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jevons", WorkDir: t.TempDir(), SessionID: sid,
		Provider: claudia.ProviderCursor, ConnectPID: leftover,
		AutoStart: false,
	}); err != nil {
		t.Fatal(err)
	}

	ReattachFleet(reg)
	deadline := time.Now().Add(2 * time.Second)
	for storeHeldBy(sid, leftover) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if storeHeldBy(sid, leftover) {
		t.Fatalf("leftover pid %d still holds store.db after bounce", leftover)
	}
}

func storeHeldBy(sessionID string, pid int) bool {
	for _, w := range listStoreWriterPIDs(claudia.CursorACPStorePath(sessionID)) {
		if w == pid {
			return true
		}
	}
	return false
}

func startCursorStoreHolder(t *testing.T) (sid string, pid int) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 required to hold store.db")
	}
	if _, err := exec.LookPath("lsof"); err != nil {
		t.Skip("lsof required to observe store.db writers")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	sid = "t541-1-leftover"
	store := claudia.CursorACPStorePath(sid)
	if err := os.MkdirAll(filepath.Dir(store), 0o700); err != nil {
		t.Fatal(err)
	}
	script := "import sys, time\n" +
		"f = open(sys.argv[1], 'ab')\n" +
		"f.write(b'x')\n" +
		"f.flush()\n" +
		"time.sleep(60)\n"
	cmd := exec.Command("python3", "-c", script, store)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid = cmd.Process.Pid
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, w := range listStoreWriterPIDs(store) {
			if w == pid {
				return sid, pid
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("leftover pid %d never appeared as a store.db writer", pid)
	return sid, pid
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package upgrade

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"
)

// stubCursorGuard sets the wait to "never clears" and answers the holder
// question as given.
func stubCursorGuard(t *testing.T, pids []int, answered bool) {
	t.Helper()
	prevWait, prevNamed := waitCursorStoreClear, namedCursorLeftovers
	t.Cleanup(func() { waitCursorStoreClear, namedCursorLeftovers = prevWait, prevNamed })
	waitCursorStoreClear = func(string, int) bool { return false }
	namedCursorLeftovers = func(string) ([]int, bool) { return pids, answered }
}

func guardedStart(starts *int) func(context.Context, claudia.Config) (*claudia.Agent, error) {
	return GuardCursorStart(func(ctx context.Context, cfg claudia.Config) (*claudia.Agent, error) {
		*starts++
		return claudia.StartStub(ctx, cfg, nil)
	})
}

// On 2026-09-21 the overseer stayed down for hours behind "leftover
// cursor-agent [] still holds store": lsof had not answered inside its bound
// on a host at load 230, the guard reported that as a resume denial, and the
// Registry latches those until a restart. Nothing held the store.
//
// Not knowing still refuses this Launch. It must not be the latching error.
func TestUnansweredHolderProbeRefusesWithoutLatching(t *testing.T) {
	stubCursorGuard(t, nil, false)
	starts := 0
	_, err := guardedStart(&starts)(context.Background(), claudia.Config{
		Provider: claudia.ProviderCursor, SessionID: "sid-slow-lsof",
		WorkDir: t.TempDir(), TermLogPath: "-",
	})
	if !errors.Is(err, ErrCursorStoreUnconfirmed) {
		t.Fatalf("err = %v, want ErrCursorStoreUnconfirmed", err)
	}
	if claudia.IsCursorResumeDenied(err) {
		t.Fatalf("an unanswered probe produced the latching error: %v", err)
	}
	if starts != 0 {
		t.Fatal("Launch ran on a store nobody could vouch for")
	}
}

// The wait ran out, and by the time anyone looked the holder had exited:
// lsof answers and names nobody, which is the wait's own test for clear.
func TestAnsweredProbeNamingNobodyLaunches(t *testing.T) {
	stubCursorGuard(t, nil, true)
	starts := 0
	a, err := guardedStart(&starts)(context.Background(), claudia.Config{
		Provider: claudia.ProviderCursor, SessionID: "sid-holder-gone",
		WorkDir: t.TempDir(), TermLogPath: "-",
	})
	if err != nil {
		t.Fatalf("store is free but Launch was refused: %v", err)
	}
	t.Cleanup(a.Stop)
	if starts != 1 {
		t.Fatalf("starts = %d, want 1", starts)
	}
}

// Turning AutoStart off is a durable registry write. A slow lsof at boot
// must not still be deciding who starts after the next boot.
func TestUnansweredProbeAtBootLeavesAutoStartAlone(t *testing.T) {
	stubCursorGuard(t, nil, false)
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jevons", WorkDir: t.TempDir(), SessionID: "sid-overseer",
		Provider: claudia.ProviderCursor, AutoStart: true, TermLogPath: "-",
	}); err != nil {
		t.Fatal(err)
	}

	hushUnreapedCursorSeats(reg)
	if def := reg.Def("jevons"); def == nil || !def.AutoStart {
		t.Fatalf("AutoStart turned off with nobody named as holder: %+v", def)
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"fmt"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/marcelocantos/claudia"
)

// 🎯T936: the fleet feed relaunches a dead seat in the background. The
// request that saw it dead returns at once, however long the launch takes —
// during a broker restart every launch waits on the broker, and the fleet
// list went unanswered for minutes. A seat is relaunched once however many
// requests see it dead.
func TestT936FeedRelaunchIsOffTheRequestPath(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	var launches atomic.Int32
	prev := feedLaunch
	feedLaunch = func(*claudia.Registry, string) (*claudia.Agent, error) {
		launches.Add(1)
		<-release // a broker that is not back yet
		return nil, nil
	}
	t.Cleanup(func() { feedLaunch = prev })

	recovered := make(chan []string, 1)
	onRecovered := func(names []string) { recovered <- names }
	// Two feed requests see the seat dead while its launch is still waiting;
	// neither waits for it. `go test -timeout` is the clock if they did.
	relaunchDeadSeat(reg, "po", "", onRecovered, nil)
	relaunchDeadSeat(reg, "po", "", onRecovered, nil)
	close(release)
	if got := <-recovered; len(got) != 1 || got[0] != "po" {
		t.Fatalf("recovered %v", got)
	}
	if n := launches.Load(); n != 1 {
		t.Fatalf("launched %d times, want once", n)
	}
}

// 🎯T936: a relaunch that finds the seat's lifecycle held by another path
// (the health sweep relaunching it too) leaves the seat to that path. The
// inline relaunch used to stop it, stopping the other path's fresh process.
func TestT936FeedRelaunchLeavesABusySeatAlone(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	prev := feedLaunch
	done := make(chan struct{})
	feedLaunch = func(*claudia.Registry, string) (*claudia.Agent, error) {
		defer close(done)
		return nil, fmt.Errorf("launch: %w", claudia.ErrLifecycleInProgress)
	}
	t.Cleanup(func() { feedLaunch = prev })
	var noted atomic.Int32
	relaunchDeadSeat(reg, "po", "", nil, func(string, string, string) { noted.Add(1) })
	<-done
	// The goroutine has returned from feedLaunch; give it the slot back.
	for {
		if _, busy := feedRelaunching.Load(feedRelaunch{reg, "po"}); !busy {
			break
		}
		runtime.Gosched()
	}
	if noted.Load() != 0 {
		t.Fatal("a busy seat was reported dead")
	}
}

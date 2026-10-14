// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package upgrade

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
)

// 🎯T778: seats started one after another made one slow adopt delay the
// whole fleet. Each seat's start must overlap with the others', and a filter
// must confine the pass to the named seats (the overseer starts alone).
func TestT778SeatsReattachConcurrentlyAndFilterConfinesPass(t *testing.T) {
	const perSeat = 300 * time.Millisecond
	var mu sync.Mutex
	started := map[string]bool{}
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg.SetDirect(true)
	reg.SetLaunchers(&claudia.RegistryLaunchers{
		Start: func(_ context.Context, cfg claudia.Config) (*claudia.Agent, error) {
			time.Sleep(perSeat)
			mu.Lock()
			started[cfg.SessionID] = true
			mu.Unlock()
			return nil, fmt.Errorf("stub: no real seat")
		},
	})
	const seats = 4
	for i := 0; i < seats; i++ {
		if err := reg.Register(claudia.AgentDef{
			Name: fmt.Sprintf("seat%d", i), WorkDir: t.TempDir(), SessionID: fmt.Sprintf("sid%d", i),
			Provider: claudia.ProviderClaude, AutoStart: true,
		}); err != nil {
			t.Fatal(err)
		}
	}

	ReattachSeatsContext(context.Background(), reg, func(n string) bool { return n == "seat0" }, 1)
	mu.Lock()
	if len(started) != 1 {
		t.Fatalf("filtered pass started %v, want only seat0", started)
	}
	started = map[string]bool{}
	mu.Unlock()

	start := time.Now()
	ReattachSeatsContext(context.Background(), reg, nil, seats)
	elapsed := time.Since(start)
	if len(started) != seats {
		t.Fatalf("started %d seats, want %d", len(started), seats)
	}
	// Serial would take seats*perSeat (1.2s); concurrent ≈ perSeat.
	if elapsed > 2*perSeat {
		t.Fatalf("seats reattached serially: %s for %d seats of %s each", elapsed, seats, perSeat)
	}
}

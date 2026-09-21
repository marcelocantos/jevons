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

// 🎯T778: four Cursor seats each spent minutes in adopt/lsof before the
// converge loop started, because seats were started one after another.
// Each seat's slow step must now overlap with the others', and a filter
// must confine the pass to the named seats (the overseer starts alone).
func TestT778SeatsReattachConcurrentlyAndFilterConfinesPass(t *testing.T) {
	prevAvail, prevReap, prevWait := brokerAvailable, reapOrphanCursorACP, waitCursorStoreClear
	t.Cleanup(func() { brokerAvailable, reapOrphanCursorACP, waitCursorStoreClear = prevAvail, prevReap, prevWait })
	brokerAvailable = func() bool { return false }
	reapOrphanCursorACP = func() []int { return nil }
	const perSeat = 300 * time.Millisecond
	var mu sync.Mutex
	probed := map[string]bool{}
	waitCursorStoreClear = func(sid string, _ int) bool {
		time.Sleep(perSeat)
		mu.Lock()
		probed[sid] = true
		mu.Unlock()
		return true
	}

	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	const seats = 4
	for i := 0; i < seats; i++ {
		// AutoStart false: the slow step runs, but no real cursor-agent starts.
		if err := reg.Register(claudia.AgentDef{
			Name: fmt.Sprintf("seat%d", i), WorkDir: t.TempDir(), SessionID: fmt.Sprintf("sid%d", i),
			Provider: claudia.ProviderCursor,
		}); err != nil {
			t.Fatal(err)
		}
	}

	// Filter: only seat0 is touched.
	ReattachSeatsContext(context.Background(), reg, func(n string) bool { return n == "seat0" }, 1)
	mu.Lock()
	if len(probed) != 1 || !probed["sid0"] {
		t.Fatalf("filtered pass probed %v, want only sid0", probed)
	}
	probed = map[string]bool{}
	mu.Unlock()

	start := time.Now()
	ReattachSeatsContext(context.Background(), reg, nil, seats)
	elapsed := time.Since(start)
	if len(probed) != seats {
		t.Fatalf("probed %d seats, want %d", len(probed), seats)
	}
	// Serial would take seats*perSeat (1.2s); concurrent ≈ perSeat.
	if elapsed > 2*perSeat {
		t.Fatalf("seats reattached serially: %s for %d seats of %s each", elapsed, seats, perSeat)
	}
}

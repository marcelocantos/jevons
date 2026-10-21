// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/discovery"
)

// 🎯T1084: each 30 s seat-observation pass asked for the exclusive Grok
// homes several times per seat, and every ask listed $TMPDIR (35,100
// entries on the development host). A pass lists it at most once, and a
// quiet temp dir not at all — even while other processes churn it.
func TestT1084SeatObservationPassListsTempDirAtMostOnce(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	for i := 0; i < 20; i++ {
		if err := os.WriteFile(filepath.Join(tmp, fmt.Sprintf("leftover-%d", i)), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(tmp, "claudia-mcp-grok-1", "sessions"), 0o700); err != nil {
		t.Fatal(err)
	}
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	const seats = 8
	for i := 0; i < seats; i++ {
		if err := reg.Register(claudia.AgentDef{
			Name: fmt.Sprintf("jv-seat-%d", i), WorkDir: tmp, SessionID: fmt.Sprintf("s-%d", i),
			Materialized: true, Provider: "grok",
		}); err != nil {
			t.Fatal(err)
		}
	}
	s := &Server{registry: reg}
	pass := func(observers ...func(claudia.AgentDef)) int64 {
		before := discovery.TempDirListings()
		s.observeSeatTranscripts(observers...)
		return discovery.TempDirListings() - before
	}

	pass() // warm
	if n := pass(); n != 0 {
		t.Fatalf("a pass over a quiet temp dir listed it %d times for %d seats", n, seats)
	}
	// Another process creates a temp entry while each seat is observed.
	churn := 0
	touch := func(claudia.AgentDef) {
		churn++
		if err := os.WriteFile(filepath.Join(tmp, fmt.Sprintf("churn-%d", churn)), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	pass(touch) // the dir changed during the warm-up pass's predecessor
	if n := pass(touch); n > 1 {
		t.Fatalf("a pass over a churning temp dir listed it %d times for %d seats; want at most once", n, seats)
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"testing"
	"time"
)

// 🎯T977: a plan that becomes admissible again after it was not is announced
// once, so agents that stood work down for capacity resume.
func TestT977CapacityWatchAnnouncesARestorationOnce(t *testing.T) {
	now := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	th := DefaultThresholds()
	plan := func(provider string, used float64) Backend {
		return Backend{Provider: provider, Status: StatusAvailable,
			Windows: []Window{bandWindow(WindowWeekly, used, 100-used, now, 0.5)}}
	}
	hot := func(p string) Backend { return plan(p, 92) }
	ok := func(p string) Backend { return plan(p, 10) }
	if !MintIneligible(hot("claude"), now, th) || MintIneligible(ok("claude"), now, th) {
		t.Fatal("fixture: hot must be inadmissible and ok admissible")
	}
	snap := func(bs ...Backend) Snapshot { return Snapshot{Backends: bs} }
	var w CapacityWatch

	// Booting onto a healthy plan announces nothing.
	if got := w.Observe(snap(ok("codex")), now, th); len(got) != 0 {
		t.Fatalf("boot onto an ok plan announced %v", got)
	}
	// Every plan inadmissible: full exhaustion.
	if got := w.Observe(snap(hot("claude"), hot("grok"), hot("codex")), now, th); len(got) != 0 {
		t.Fatalf("exhaustion announced %v", got)
	}
	// A plan still hot stays quiet.
	if got := w.Observe(snap(hot("claude"), hot("grok"), hot("codex")), now, th); len(got) != 0 {
		t.Fatalf("a plan still hot announced %v", got)
	}
	// Two plans come back in the same reading: one notice naming both.
	got := w.Observe(snap(ok("claude"), hot("grok"), ok("codex")), now, th)
	if len(got) != 2 || got[0].Provider != "claude" || got[1].Provider != "codex" {
		t.Fatalf("restoration = %v, want claude and codex", got)
	}
	// Staying ok does not announce again.
	if got := w.Observe(snap(ok("claude"), hot("grok"), ok("codex")), now, th); len(got) != 0 {
		t.Fatalf("an ok plan announced again: %v", got)
	}
	// An unreadable reading is unknown: it neither arms nor fires.
	if got := w.Observe(snap(Backend{Provider: "grok", Status: "unavailable"}), now, th); len(got) != 0 {
		t.Fatalf("an unreadable plan announced %v", got)
	}
	// The owner's OK override counts as admissible.
	over := hot("grok")
	over.Override = &Override{Band: BandOK, Reason: "owner"}
	if got := w.Observe(snap(over), now, th); len(got) != 1 || got[0].Provider != "grok" {
		t.Fatalf("owner override to ok = %v, want grok restored", got)
	}
}

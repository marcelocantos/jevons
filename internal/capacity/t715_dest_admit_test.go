// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package capacity

import (
	"strings"
	"testing"
)

// 🎯T715: dest-aware admit. claude 12/12 must not refuse a grok mint;
// an explicit claude dest is still refused and names dests with headroom.
func TestT715AdmitSpawnDestGrokWhileClaudeAtCap(t *testing.T) {
	snap := saturatedProviderSnapshot()
	if d := AdmitSpawnDest(SpawnWorker, "grok", snap, DefaultPolicy()); !d.Admitted() {
		t.Fatalf("grok dest refused while claude at cap: %+v", d)
	}
	d := AdmitSpawnDest(SpawnWorker, "claude", snap, DefaultPolicy())
	if d.Admitted() {
		t.Fatalf("claude dest admitted at 12/12: %+v", d)
	}
	if d.Reason != ReasonSeatCount {
		t.Fatalf("reason %q, want %s", d.Reason, ReasonSeatCount)
	}
	if !strings.Contains(d.Detail, "claude") || !strings.Contains(d.Detail, "12") {
		t.Fatalf("refuse must name the exhausted dest: %s", d.Detail)
	}
	if !strings.Contains(d.Detail, "dests with headroom") || !strings.Contains(d.Detail, "grok") {
		t.Fatalf("explicit at-cap must name dests with headroom: %s", d.Detail)
	}
}

// Dest-unaware AdmitSpawn is the fan-out / early-gate question: refuse
// only when every published dest is at cap or the session census is full,
// not because the tightest other dest is full.
func TestT715AdmitSpawnDestUnawareDoesNotBindOnOtherDest(t *testing.T) {
	snap := saturatedProviderSnapshot()
	if d := AdmitSpawn(SpawnWorker, snap, DefaultPolicy()); !d.Admitted() {
		t.Fatalf("dest-unaware refused because claude is tightest: %+v — grok has room", d)
	}
}

func TestT715AllDestsAtCapRefusesBoth(t *testing.T) {
	snap := Snapshot{
		MaxSessions:      20,
		ActiveSessions:   18,
		ProviderLoad:     map[string]int{"claude": 12, "grok": 12, "codex": 6},
		ProviderSoftCaps: map[string]int{"claude": 12, "codex": 6, "grok": 12},
	}
	if d := AdmitSpawn(SpawnWorker, snap, DefaultPolicy()); d.Admitted() {
		t.Fatalf("dest-unaware admitted when every dest is at cap: %+v", d)
	}
	d := AdmitSpawnDest(SpawnWorker, "grok", snap, DefaultPolicy())
	if d.Admitted() {
		t.Fatalf("grok dest admitted at cap: %+v", d)
	}
	if !strings.Contains(d.Detail, "dests with headroom: none") {
		t.Fatalf("all-at-cap refuse must say none: %s", d.Detail)
	}
}

func TestT715SessionCensusStillBinds(t *testing.T) {
	snap := Snapshot{
		MaxSessions:      20,
		ActiveSessions:   20,
		ProviderLoad:     map[string]int{"claude": 8, "grok": 0},
		ProviderSoftCaps: map[string]int{"claude": 12, "grok": 12},
	}
	if d := AdmitSpawnDest(SpawnWorker, "grok", snap, DefaultPolicy()); d.Admitted() {
		t.Fatalf("session 20/20 admitted grok dest: %+v", d)
	}
	if d := AdmitSpawn(SpawnWorker, snap, DefaultPolicy()); d.Admitted() {
		t.Fatalf("dest-unaware admitted at session 20/20: %+v", d)
	}
}

func TestT715DestsWithHeadroomOrder(t *testing.T) {
	snap := saturatedProviderSnapshot()
	got := DestsWithHeadroom(snap, DefaultPolicy())
	joined := strings.Join(got, ", ")
	if !strings.Contains(joined, "grok") || !strings.Contains(joined, "codex") {
		t.Fatalf("headroom dests = %v, want grok and codex", got)
	}
	if strings.Contains(joined, "claude") {
		t.Fatalf("exhausted claude listed as headroom: %v", got)
	}
}

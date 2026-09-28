// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package seatactivity

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestLookupSidecarSeatsAreIndependentNotSharedFile is the 🎯T893
// acceptance: two anthropic/OMP sidecar seats writing into the same dated
// spool file must resolve to different Age/LastMove — the one still
// working stays fresh, the one that has gone quiet ages — rather than both
// reading the file's single shared mtime.
func TestLookupSidecarSeatsAreIndependentNotSharedFileMtime(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("JEVONS_SPOOL_DIR", dir)
	log := filepath.Join(dir, "events-2026-09-28.log")
	body := `{"ts":"2026-09-28T18:00:00.000Z","seat":"jv-idle-po","type":"text","text":"a"}` + "\n" +
		`{"ts":"2026-09-28T18:46:50.000Z","seat":"jv-busy-worker","type":"text","text":"b"}` + "\n"
	if err := os.WriteFile(log, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 28, 18, 47, 0, 0, time.UTC)

	idle := Lookup(Query{Name: "jv-idle-po", Now: now})
	busy := Lookup(Query{Name: "jv-busy-worker", Now: now})

	if idle.Verdict != VerdictKnown || busy.Verdict != VerdictKnown {
		t.Fatalf("idle=%+v busy=%+v want both known", idle, busy)
	}
	if idle.LastMove.Equal(busy.LastMove) {
		t.Fatalf("idle and busy resolved to the identical LastMove %s — shared-file bug is back", idle.LastMove)
	}
	if idle.Age == busy.Age {
		t.Fatalf("idle and busy resolved to the identical age %s — shared-file bug is back", idle.Age)
	}
	// idle has been quiet ~47 minutes; busy moved 10s ago.
	if idle.Age < 40*time.Minute {
		t.Fatalf("idle age=%s want >40min (last wrote at 18:00:00, now 18:47:00)", idle.Age)
	}
	if busy.Age > time.Minute {
		t.Fatalf("busy age=%s want <1min (last wrote at 18:46:50, now 18:47:00)", busy.Age)
	}
}

// TestLookupSidecarSeatUnknownWithoutPerSeatSource: a sidecar seat with no
// readable per-seat ts must report unknown, not borrow another seat's
// timestamp or the file's mtime as if it were its own.
func TestLookupSidecarSeatUnknownWithoutPerSeatSource(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("JEVONS_SPOOL_DIR", dir)
	log := filepath.Join(dir, "events-2026-09-28.log")
	if err := os.WriteFile(log, []byte(`{"seat":"no-ts-seat","type":"text","text":"a"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := Lookup(Query{Name: "no-ts-seat"})
	if got.Verdict != VerdictUnknown {
		t.Fatalf("no-ts-seat: %+v want unknown", got)
	}
	if !got.LastMove.IsZero() || got.Age != 0 {
		t.Fatalf("unknown reading must leave LastMove/Age zero-value, got %+v", got)
	}
}

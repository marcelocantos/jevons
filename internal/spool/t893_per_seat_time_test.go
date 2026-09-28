// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package spool

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestLatestSeatTimeIsPerSeatNotSharedFileMtime is the 🎯T893 reproduction:
// two sidecar seats write into the SAME dated file (as the real sidecar
// does — one events-YYYY-MM-DD.log per day, every seat that talked that
// day). LatestPath returns the identical shared path for both, so an
// os.Stat-based reader (the pre-fix behaviour) reports the identical age
// for both regardless of which seat actually just moved. LatestSeatTime
// must read the per-line "ts" field instead and give each seat its own
// answer.
func TestLatestSeatTimeIsPerSeatNotSharedFileMtime(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "events-2026-09-28.log")
	body := `{"ts":"2026-09-28T18:00:00.000Z","seat":"busy-seat","type":"text","text":"a"}` + "\n" +
		`{"ts":"2026-09-28T18:00:05.000Z","seat":"idle-seat","type":"text","text":"b"}` + "\n" +
		// busy-seat writes again, much later; idle-seat does not.
		`{"ts":"2026-09-28T18:45:00.000Z","seat":"busy-seat","type":"text","text":"c"}` + "\n"
	if err := os.WriteFile(log, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	// The file's own mtime (what the OLD os.Stat path would have used for
	// BOTH seats) is whatever the write above left it at — some instant at
	// or after the last append, identical for every seat naming this file.
	fi, err := os.Stat(log)
	if err != nil {
		t.Fatal(err)
	}
	sharedMtime := fi.ModTime()

	if got := LatestPath(dir, "busy-seat"); got != log {
		t.Fatalf("busy-seat LatestPath=%q want %q (shared file, confirms the shared-source shape)", got, log)
	}
	if got := LatestPath(dir, "idle-seat"); got != log {
		t.Fatalf("idle-seat LatestPath=%q want %q (shared file, confirms the shared-source shape)", got, log)
	}

	busyTS, ok := LatestSeatTime(dir, "busy-seat")
	if !ok {
		t.Fatal("busy-seat: LatestSeatTime not ok")
	}
	idleTS, ok := LatestSeatTime(dir, "idle-seat")
	if !ok {
		t.Fatal("idle-seat: LatestSeatTime not ok")
	}

	wantBusy := time.Date(2026, 9, 28, 18, 45, 0, 0, time.UTC)
	wantIdle := time.Date(2026, 9, 28, 18, 0, 5, 0, time.UTC)
	if !busyTS.Equal(wantBusy) {
		t.Fatalf("busy-seat ts=%s want %s", busyTS, wantBusy)
	}
	if !idleTS.Equal(wantIdle) {
		t.Fatalf("idle-seat ts=%s want %s", idleTS, wantIdle)
	}
	if busyTS.Equal(idleTS) {
		t.Fatal("busy-seat and idle-seat resolved to the identical instant — the T893 bug is back")
	}

	// The whole point: neither per-seat answer is merely the shared file
	// mtime relabeled. A regression that quietly falls back to os.Stat on
	// LatestPath's result would pass "different from each other" only by
	// accident on unlucky timing, so pin each seat's ts against the file's
	// own mtime too — the ts fields were both stamped hours before "now"
	// (this test running), and mtime is "now".
	if busyTS.Equal(sharedMtime) || idleTS.Equal(sharedMtime) {
		t.Fatalf("a per-seat ts equalled the shared file mtime %s — suspect a stat fallback", sharedMtime)
	}
}

// TestLatestSeatTimeUnknownWhenNoTSField: a spool line naming a seat but
// with no parseable "ts" must not resolve to time.Now() masquerading as
// fresh (🎯T677's null-not-zero rule extended to the ts source).
func TestLatestSeatTimeUnknownWhenNoTSField(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "events-2026-09-28.log")
	if err := os.WriteFile(log, []byte(`{"seat":"no-ts-seat","type":"text","text":"a"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := LatestSeatTime(dir, "no-ts-seat"); ok {
		t.Fatal("no-ts-seat: LatestSeatTime ok=true with no ts field in the record")
	}
	if _, ok := LatestSeatTime(dir, "never-seen-seat"); ok {
		t.Fatal("never-seen-seat: LatestSeatTime ok=true for a seat with no spool history at all")
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package spool

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 🎯T1067: a windowed read skips day files written before the window, and
// decodes only lines that name the seat — while still returning every
// record the seat owns, and none whose text merely mentions it.
func TestT1067ReadSeatSinceWindowsAndFilters(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "events-2026-10-10.log")
	cur := filepath.Join(dir, "events-2026-10-12.log")
	write := func(path, body string, mtime time.Time) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2026, 10, 12, 8, 0, 0, 0, time.UTC)
	write(old, `{"ts":"2026-10-10T01:00:00Z","seat":"w","type":"turn_end","text":"old"}`+"\n", now.Add(-48*time.Hour))
	write(cur, `{"ts":"2026-10-12T07:00:00Z","seat":"w","type":"text","text":"hi"}`+"\n"+
		`{"ts":"2026-10-12T07:00:01Z","seat":"other","type":"text","text":"talking about \"seat\":\"w\""}`+"\n"+
		`{"ts":"2026-10-12T07:00:02Z","seat":"w","type":"turn_end","text":"done"}`+"\n", now)

	all, err := ReadSeat(dir, "w")
	if err != nil || len(all) != 3 {
		t.Fatalf("ReadSeat = %d records (%v); want 3 across both days", len(all), err)
	}
	recent, err := ReadSeatSince(dir, "w", now.Add(-6*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 2 || recent[0].Text != "hi" || recent[1].Text != "done" {
		t.Fatalf("ReadSeatSince = %+v; want today's two records for w only", recent)
	}
	if !recent[1].TS.Equal(time.Date(2026, 10, 12, 7, 0, 2, 0, time.UTC)) {
		t.Fatalf("ts not parsed: %v", recent[1].TS)
	}
}

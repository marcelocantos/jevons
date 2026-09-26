// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package spool

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadSeatOlderDatesFirst(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "events-2026-09-26.log"), []byte(`{"ts":"2026-09-26T00:00:00.000Z","seat":"s","type":"text","text":"new"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "events-2026-09-24.log"), []byte(`{"ts":"2026-09-24T00:00:00.000Z","seat":"s","type":"text","text":"old"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	recs, err := ReadSeat(dir, "s")
	if err != nil || len(recs) != 2 {
		t.Fatalf("ReadSeat = %+v %v", recs, err)
	}
	if recs[0].Text != "old" || recs[1].Text != "new" {
		t.Fatalf("order = %q then %q", recs[0].Text, recs[1].Text)
	}
	if !SeatHasHistory(dir, "s") || SeatHasHistory(dir, "missing") {
		t.Fatal("SeatHasHistory")
	}
	if LatestPath(dir, "s") != filepath.Join(dir, "events-2026-09-26.log") {
		t.Fatalf("LatestPath = %q", LatestPath(dir, "s"))
	}
}

func TestSeatIndexSkipsSnapshotBody(t *testing.T) {
	dir := t.TempDir()
	var b strings.Builder
	b.WriteString(`{"ts":"2026-09-26T00:00:00.000Z","seat":"jevons","type":"turn_end","snapshot":`)
	b.WriteString(strings.Repeat("x", 256*1024))
	b.WriteByte('\n')
	if err := os.WriteFile(filepath.Join(dir, "events-2026-09-26.log"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	if !SeatHasHistory(dir, "jevons") || SeatHasHistory(dir, "other") {
		t.Fatal("index must see jevons and not the snapshot body")
	}
	if got := LatestPath(dir, "jevons"); got != filepath.Join(dir, "events-2026-09-26.log") {
		t.Fatalf("LatestPath = %q", got)
	}
}

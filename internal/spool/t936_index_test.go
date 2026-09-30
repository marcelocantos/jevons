// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package spool

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 🎯T936: the seat index reads only what was appended since it last looked.
// A full rescan of every day file on each append took /api/agents past a
// minute once the spool reached 2 GB, and every fleet read queued behind it.
func TestT936SeatIndexReadsOnlyAppendedBytes(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "events-2026-09-29.log")
	day := filepath.Join(dir, "events-2026-09-30.log")
	line := func(seat, ts string) string {
		return `{"seat":"` + seat + `","ts":"` + ts + `","type":"text","text":"` + strings.Repeat("x", 200) + `"}` + "\n"
	}
	if err := os.WriteFile(old, []byte(line("po", "2026-09-29T10:00:00Z")+line("gone", "2026-09-29T11:00:00Z")), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(day, []byte(line("po", "2026-09-30T09:00:00Z")), 0o644); err != nil {
		t.Fatal(err)
	}
	appendTo := func(s string) {
		f, err := os.OpenFile(day, os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if _, err := f.WriteString(s); err != nil {
			t.Fatal(err)
		}
	}
	check := func(seat, wantTS, wantFile string) {
		t.Helper()
		if got := LatestPath(dir, seat); filepath.Base(got) != wantFile {
			t.Fatalf("%s path %q, want %s", seat, got, wantFile)
		}
		got, ok := LatestSeatTime(dir, seat)
		if !ok || !got.Equal(mustTS(t, wantTS)) {
			t.Fatalf("%s ts %v (ok=%v), want %s", seat, got, ok, wantTS)
		}
	}
	check("po", "2026-09-30T09:00:00Z", "events-2026-09-30.log")
	check("gone", "2026-09-29T11:00:00Z", "events-2026-09-29.log")

	// A quiet poll reads nothing.
	before := scannedBytes.Load()
	check("po", "2026-09-30T09:00:00Z", "events-2026-09-30.log")
	if n := scannedBytes.Load() - before; n != 0 {
		t.Fatalf("a quiet poll read %d bytes", n)
	}

	// An append reads the appended line only; a line still being written
	// is not taken, and is read whole once it is finished.
	next := line("worker", "2026-09-30T09:05:00Z")
	half := line("po", "2026-09-30T09:06:00Z")
	appendTo(next + half[:40])
	time.Sleep(10 * time.Millisecond) // a distinct mtime on coarse filesystems
	before = scannedBytes.Load()
	check("worker", "2026-09-30T09:05:00Z", "events-2026-09-30.log")
	check("po", "2026-09-30T09:00:00Z", "events-2026-09-30.log")
	if n := scannedBytes.Load() - before; n != int64(len(next)+40) {
		t.Fatalf("the append read %d bytes, want %d (the new bytes only)", n, len(next)+40)
	}
	appendTo(half[40:])
	check("po", "2026-09-30T09:06:00Z", "events-2026-09-30.log")
}

func mustTS(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return ts
}

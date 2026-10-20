// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package turndepth

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/spool"
)

// 🎯T1085: Depth reads the seat's lines, not the whole spool, and counts what
// the full read counted.
func TestT1085DepthReadsSeatBytesOnly(t *testing.T) {
	dir := t.TempDir()
	own := map[string]int64{}
	var total int64
	snap := strings.Repeat("s", 80000)
	for day := 18; day <= 19; day++ {
		var b strings.Builder
		for turn := 0; turn < 3; turn++ {
			for s := 0; s < 30; s++ {
				seat := fmt.Sprintf("jv-seat-%02d", s)
				lines := []string{
					fmt.Sprintf(`{"ts":"2026-10-%dT01:02:03Z","seat":%q,"type":"tool_call","name":"a"}`, day, seat),
					fmt.Sprintf(`{"ts":"2026-10-%dT01:02:04Z","seat":%q,"type":"tool_call","name":"b"}`, day, seat),
				}
				if !(day == 19 && turn == 2) {
					lines = append(lines, fmt.Sprintf(`{"ts":"2026-10-%dT01:02:05Z","seat":%q,"type":"turn_end","snapshot":{"s":%q}}`, day, seat, snap))
				}
				for _, l := range lines {
					own[seat] += int64(len(l) + 1)
					b.WriteString(l + "\n")
				}
			}
		}
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("events-2026-10-%d.log", day)), []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		total += int64(b.Len())
	}
	const seat = "jv-seat-05"
	spool.SeatHasHistory(dir, seat)

	before := spool.ScannedBytes()
	got := Depth(dir, seat)
	read := spool.ScannedBytes() - before
	if limit := 2 * own[seat]; read > limit {
		t.Fatalf("Depth read %d bytes of a %d-byte spool; the seat's own lines are %d (limit %d)", read, total, own[seat], limit)
	}
	recs, err := spool.ReadSeat(dir, seat)
	if err != nil {
		t.Fatal(err)
	}
	want := 0
	for _, rec := range recs {
		switch rec.Type {
		case "tool_call":
			want++
		case "turn_end":
			want = 0
		}
	}
	if want != 2 || got != want {
		t.Fatalf("Depth = %d, full read %d (fixture wants 2)", got, want)
	}
}

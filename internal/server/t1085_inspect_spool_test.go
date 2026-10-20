// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/spool"
)

// t1085Spool writes a many-seat spool shaped like the real one: small text
// and tool lines, and turn_ends whose snapshots are most of the bytes. It
// returns the spool's size and each seat's own bytes.
func t1085Spool(t *testing.T, dir string, seats int) (total int64, own map[string]int64) {
	t.Helper()
	own = map[string]int64{}
	snap := strings.Repeat("s", 80000)
	for day := 17; day <= 19; day++ {
		var b strings.Builder
		for turn := 0; turn < 4; turn++ {
			for s := 0; s < seats; s++ {
				seat := fmt.Sprintf("jv-seat-%02d", s)
				lines := []string{
					fmt.Sprintf(`{"ts":"2026-10-%dT01:02:03.000Z","seat":%q,"type":"accepted"}`, day, seat),
					fmt.Sprintf(`{"ts":"2026-10-%dT01:02:04.000Z","seat":%q,"type":"text","text":"working on %d"}`, day, seat, turn),
					fmt.Sprintf(`{"ts":"2026-10-%dT01:02:05.000Z","seat":%q,"type":"tool_call","call_id":"c%d","name":"Bash","text":"{\"cmd\":\"ls\"}"}`, day, seat, turn),
					fmt.Sprintf(`{"ts":"2026-10-%dT01:02:06.000Z","seat":%q,"type":"turn_end","text":"turn %d done","snapshot":{"s":%q}}`, day, seat, turn, snap),
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
	return total, own
}

func withoutTimestamps(t *testing.T, lines []string) []map[string]any {
	t.Helper()
	out := make([]map[string]any, 0, len(lines))
	for _, l := range lines {
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("%v: %s", err, l)
		}
		delete(m, "timestamp")
		out = append(out, m)
	}
	return out
}

// 🎯T1085: opening a sidecar seat's inspect stream reads that seat's lines,
// not the whole spool, and paints what the full read painted.
func TestT1085InspectHydrationReadsSeatBytesOnly(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(spool.DirEnv, dir)
	total, own := t1085Spool(t, dir, 30)
	const seat = "jv-seat-11"
	spool.SeatHasHistory(dir, seat) // the index a running daemon already holds

	s := &Server{}
	before := spool.ScannedBytes()
	got := s.reconstructedInspectLines(seat)
	read := spool.ScannedBytes() - before
	if limit := 2 * own[seat]; read > limit {
		t.Fatalf("inspect hydration read %d bytes of a %d-byte spool; the seat's own lines are %d (limit %d)", read, total, own[seat], limit)
	}

	recs, err := spool.ReadSeat(dir, seat)
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	for _, line := range chatWireFromSpool(recs) {
		want = append(want, stampConversationName(line, seat))
	}
	if len(want) != 3*4*3 {
		t.Fatalf("fixture: full read paints %d lines", len(want))
	}
	if g, w := withoutTimestamps(t, got), withoutTimestamps(t, want); !reflect.DeepEqual(g, w) {
		t.Fatalf("inspect hydration differs from the full read:\n got %v\nwant %v", g, w)
	}
}

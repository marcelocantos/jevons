// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package transcript

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/discovery"
	"github.com/marcelocantos/jevons/internal/spool"
)

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
					fmt.Sprintf(`{"ts":"2026-10-%dT01:02:06.000Z","seat":%q,"type":"turn_end","text":"turn %d done","costUSD":0.25,"snapshot":{"s":%q}}`, day, seat, turn, snap),
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

// 🎯T1085: the transcript tool's spool view reads the seat's lines, not the
// whole spool, and yields the turns the full read yielded.
func TestT1085TranscriptReadsSeatBytesOnly(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(spool.DirEnv, dir)
	total, own := t1085Spool(t, dir, 30)
	const seat = "jv-seat-23"
	spool.SeatHasHistory(dir, seat)

	reader := NewReaderRoots(discovery.Roots{})
	before := spool.ScannedBytes()
	got, err := reader.ReadForSeat(seat, "")
	if err != nil {
		t.Fatal(err)
	}
	read := spool.ScannedBytes() - before
	if limit := 2 * own[seat]; read > limit {
		t.Fatalf("transcript read %d bytes of a %d-byte spool; the seat's own lines are %d (limit %d)", read, total, own[seat], limit)
	}

	recs, err := spool.ReadSeat(dir, seat)
	if err != nil {
		t.Fatal(err)
	}
	ref := filepath.Join(t.TempDir(), "ref.jsonl")
	if err := os.WriteFile(ref, spool.AsJSONL(recs), 0o600); err != nil {
		t.Fatal(err)
	}
	want, err := reader.readPath(ref)
	if err != nil {
		t.Fatal(err)
	}
	if len(want) < 12 {
		t.Fatalf("fixture: full read yields %d turns", len(want))
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("transcript differs from the full read:\n got %v\nwant %v", got, want)
	}
}

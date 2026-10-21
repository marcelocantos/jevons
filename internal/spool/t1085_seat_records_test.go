// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package spool

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

// 🎯T1085 oracles. The reference is ReadSeat, the full scan every request
// reader used; SeatRecords must return its records, less Snapshot and
// UsageFact, from the seat's own lines.

func fullScanRecords(t *testing.T, dir, seat string) []Record {
	t.Helper()
	recs, err := ReadSeat(dir, seat)
	if err != nil {
		t.Fatalf("ReadSeat(%q): %v", seat, err)
	}
	for i := range recs {
		recs[i].Snapshot, recs[i].UsageFact = nil, nil
	}
	return recs
}

func checkSeatRecords(t *testing.T, label, dir string, seats []string) {
	t.Helper()
	check := func(when string) {
		for _, seat := range seats {
			want := fullScanRecords(t, dir, seat)
			got, err := SeatRecords(dir, seat)
			if err != nil {
				t.Fatalf("%s %s: SeatRecords(%q): %v", label, when, seat, err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("%s %s: seat %q:\n got %d records %+v\nwant %d records %+v", label, when, seat, len(got), got, len(want), want)
			}
		}
	}
	check("warm")
	check("warm again") // big lines now come from memory
	forgetIndex(dir)
	check("cold")
}

func bigTurnEnd(seat string, size int) string {
	return fmt.Sprintf(`{"ts":"2026-10-19T01:02:03.000Z","seat":%q,"type":"turn_end","text":"done","snapshot":{"s":"%s"},"usage_fact":{"n":1}}`+"\n",
		seat, strings.Repeat("s", size))
}

func TestT1085SeatRecordsBoundaryCases(t *testing.T) {
	const d1, d2 = "events-2026-10-18.log", "events-2026-10-19.log"
	type step struct{ file, body string }
	seats := []string{"w", "other", "a<b", "x y"}
	cases := map[string][]step{
		"no records": {{d1, spoolLine("other", "text", "hi")}},
		"a seat across a day-file boundary": {
			{d1, spoolLine("w", "text", "start") + spoolLine("other", "text", "o")},
			{d2, spoolLine("w", "accepted", "") + bigTurnEnd("w", 70000) + spoolLine("w", "text", "next")},
		},
		"small and large lines interleaved between seats": {
			{d1, spoolLine("w", "text", "1") + spoolLine("w", "text", "2") + bigTurnEnd("other", 100000) +
				spoolLine("w", "text", "3") + bigTurnEnd("w", 80000) + bigTurnEnd("w", 1000) + spoolLine("other", "text", "o")},
		},
		"a partial last line is not a record until it is finished": {
			{d1, spoolLine("w", "text", "busy")},
			{d1, bigTurnEnd("w", 90000)[:300]},
			{d1, bigTurnEnd("w", 90000)[300:]},
		},
		"a partial last line of another seat is not read whole": {
			{d1, spoolLine("w", "text", "busy") + bigTurnEnd("other", 90000)[:70000]},
		},
		"a whole record with no newline yet counts, as the full scan counts it": {
			{d1, spoolLine("w", "text", "busy")},
			{d1, strings.TrimSuffix(bigTurnEnd("w", 70000), "\n")},
			{d1, "\n" + spoolLine("w", "text", "again")},
		},
		"an older day file that ends mid-line": {
			{d1, spoolLine("w", "text", "a") + strings.TrimSuffix(spoolLine("w", "text", "no newline"), "\n")},
			{d2, spoolLine("w", "text", "b")},
		},
		"malformed lines are skipped": {
			{d1, spoolLine("w", "text", "ok") + `{"ts":"x","seat":"w","type":"turn_end","text":` + "\n" +
				`{"ts":"x","seat":"w","type":"text","costUSD":"bad","text":"y"}` + "\n" +
				`{"ts":"x","seat":"w","type":"turn_end","snapshot":{"s":"` + strings.Repeat("q", 70000) + "\n"},
		},
		"a line that mentions a seat is not that seat's": {
			{d1, spoolLine("other", "text", `"seat":"w"`) + `{"meta":{"seat":"w"},"seat":"other","type":"text","text":"n"}` + "\n"},
		},
		"lines whose head does not plainly name the seat": {
			{d1, `{"ts":"2026-10-19T01:02:03Z","seat": "w","type":"text","text":"spaced"}` + "\n" +
				`{"text":"` + strings.Repeat("t", 600) + `","seat":"w","type":"text"}` + "\n" +
				`{"seat":"a\u003cb","type":"text","text":"escaped"}` + "\n" +
				`{"seat":"a<b","type":"text","text":"not the key json writes"}` + "\n" +
				`{"seat":"x y","type":"text","text":"space in the name"}` + "\n" +
				"\n\r\n   \n" + `[{"seat":"w"}]` + "\n" + `"seat":"w"` + "\n"},
		},
		"timestamps": {
			{d1, `{"ts":"2026-10-19T01:02:03Z","seat":"w","type":"accepted"}` + "\n" +
				`{"ts":"2026-10-19T01:02:03.123456789+10:00","seat":"w","type":"accepted"}` + "\n" +
				`{"ts":"yesterday","seat":"w","type":"accepted"}` + "\n" + `{"seat":"w","type":"accepted"}` + "\n"},
		},
	}
	for name, steps := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			for i, st := range steps {
				appendFile(t, filepath.Join(dir, st.file), st.body)
				time.Sleep(time.Millisecond)
				checkSeatRecords(t, fmt.Sprintf("step %d", i), dir, seats)
			}
		})
	}
}

// The fixture's lines must reach the reference, or the equality above says
// nothing.
func TestT1085BoundaryFixturesHaveRecords(t *testing.T) {
	dir := t.TempDir()
	appendFile(t, filepath.Join(dir, "events-2026-10-19.log"),
		`{"text":"`+strings.Repeat("t", 600)+`","seat":"w","type":"text"}`+"\n"+
			`{"seat":"a\u003cb","type":"text","text":"escaped"}`+"\n"+bigTurnEnd("w", 70000))
	if n := len(fullScanRecords(t, dir, "w")); n != 2 {
		t.Fatalf("full scan: %d records for w, want 2", n)
	}
	if n := len(fullScanRecords(t, dir, "a<b")); n != 1 {
		t.Fatalf("full scan: %d records for a<b, want 1", n)
	}
}

func TestT1085SeatRecordsMatchFullScanOnRandomSpools(t *testing.T) {
	seats := []string{"a", "b", "jv-c"}
	gen := func(r *rand.Rand) string {
		seat := seats[r.Intn(len(seats))]
		switch r.Intn(12) {
		case 0, 1, 2:
			return spoolLine(seat, "text", "word")
		case 3:
			return spoolLine(seat, "tool_call", `{"x":1}`)
		case 4, 5:
			return bigTurnEnd(seat, r.Intn(140000))
		case 6:
			return spoolLine(seat, []string{"accepted", "ready", "tool_start", "turn", "error"}[r.Intn(5)], "")
		case 7:
			return fmt.Sprintf(`{"ts":"x","seat":%q,"type":"turn_end","text":`+"\n", seat) // malformed
		case 8:
			return fmt.Sprintf(`{"text":"%s","seat":%q,"type":"text"}`+"\n", strings.Repeat("h", r.Intn(1000)), seat)
		case 9:
			return "\n"
		default:
			return spoolLine(seat, "text", `quoting "seat":"`+seats[r.Intn(len(seats))]+`"`)
		}
	}
	for iter := 0; iter < 40; iter++ {
		r := rand.New(rand.NewSource(int64(iter)))
		dir := t.TempDir()
		files := []string{"events-2026-10-17.log", "events-2026-10-18.log", "events-2026-10-19.log"}[:1+r.Intn(3)]
		for _, name := range files {
			var b strings.Builder
			for i := 0; i < 5+r.Intn(40); i++ {
				b.WriteString(gen(r))
			}
			body := b.String()
			for len(body) > 0 {
				cut := 1 + r.Intn(len(body))
				appendFile(t, filepath.Join(dir, name), body[:cut])
				body = body[cut:]
				time.Sleep(time.Millisecond)
				for _, seat := range seats {
					got, err := SeatRecords(dir, seat)
					if err != nil {
						t.Fatal(err)
					}
					if want := fullScanRecords(t, dir, seat); !reflect.DeepEqual(got, want) {
						t.Fatalf("iter %d: seat %q: %d records, full scan %d", iter, seat, len(got), len(want))
					}
				}
			}
		}
		checkSeatRecords(t, fmt.Sprintf("iter %d final", iter), dir, seats)
	}
}

func TestT1085EachSeatRecordStopsEarly(t *testing.T) {
	dir := t.TempDir()
	appendFile(t, filepath.Join(dir, "events-2026-10-18.log"), spoolLine("w", "text", "first")+bigTurnEnd("w", 100000))
	appendFile(t, filepath.Join(dir, "events-2026-10-19.log"), bigTurnEnd("w", 100000))
	SeatHasHistory(dir, "w") // build the index
	before := ScannedBytes()
	var seen []string
	if err := EachSeatRecord(dir, "w", func(rec Record) bool {
		seen = append(seen, rec.Text)
		return false
	}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(seen, []string{"first"}) {
		t.Fatalf("visited %v", seen)
	}
	if n := ScannedBytes() - before; n > 1000 {
		t.Fatalf("stopping at the first record read %d bytes", n)
	}
}

// manySeatSpool writes a spool shaped like the real one: many seats, small
// text lines, and turn_ends carrying large snapshots that are most of the
// bytes. It returns the total size and, per seat, its bytes and the bytes of
// its lines over bigLineBytes.
func manySeatSpool(t *testing.T, dir string, seats int) (total int64, own, big map[string]int64) {
	t.Helper()
	own, big = map[string]int64{}, map[string]int64{}
	for day := 17; day <= 19; day++ {
		var b strings.Builder
		for turn := 0; turn < 4; turn++ {
			for s := 0; s < seats; s++ {
				seat := fmt.Sprintf("jv-seat-%02d", s)
				for k := 0; k < 3; k++ {
					line := spoolLine(seat, "text", "delta")
					own[seat] += int64(len(line))
					b.WriteString(line)
				}
				line := bigTurnEnd(seat, 80000)
				own[seat] += int64(len(line))
				big[seat] += int64(len(line))
				b.WriteString(line)
			}
		}
		appendFile(t, filepath.Join(dir, fmt.Sprintf("events-2026-10-%d.log", day)), b.String())
		total += int64(b.Len())
	}
	return total, own, big
}

// Bytes read: with the index built, a request costs the seat's own lines,
// and once its large lines are remembered, only its small ones. The full
// scan it replaces reads the whole spool.
func TestT1085SeatRecordsReadsSeatBytesOnly(t *testing.T) {
	dir := t.TempDir()
	total, own, big := manySeatSpool(t, dir, 30)
	const seat = "jv-seat-07"
	SeatHasHistory(dir, seat) // the index, built once for every seat

	before := ScannedBytes()
	recs, err := SeatRecords(dir, seat)
	if err != nil {
		t.Fatal(err)
	}
	first := ScannedBytes() - before
	if len(recs) != 3*4*4 {
		t.Fatalf("%d records", len(recs))
	}
	if first > own[seat] {
		t.Fatalf("first request read %d bytes; the seat's own lines are %d of a %d-byte spool", first, own[seat], total)
	}

	before = ScannedBytes()
	if _, err := SeatRecords(dir, seat); err != nil {
		t.Fatal(err)
	}
	if again := ScannedBytes() - before; again > own[seat]-big[seat] {
		t.Fatalf("second request read %d bytes; the seat's small lines are %d", again, own[seat]-big[seat])
	}

	before = ScannedBytes()
	if _, err := ReadSeat(dir, seat); err != nil {
		t.Fatal(err)
	}
	if full := ScannedBytes() - before; full < total {
		t.Fatalf("control: the full scan read %d bytes of %d", full, total)
	}
}

// TestT1085RealSpoolSample is the equivalence oracle on real records, never
// committed: set JEVONS_T1085_REAL_SPOOL to a spool directory and the test
// copies up to JEVONS_T1085_REAL_BUDGET_MB (default 400) of its smallest
// day files into a temp dir, read-only on the source, and holds every seat's
// SeatRecords to its ReadSeat, warm, warm again and cold.
func TestT1085RealSpoolSample(t *testing.T) {
	src := os.Getenv("JEVONS_T1085_REAL_SPOOL")
	if src == "" {
		t.Skip("set JEVONS_T1085_REAL_SPOOL to a spool directory to run the real-sample oracle")
	}
	budget := int64(400) << 20
	if v := os.Getenv("JEVONS_T1085_REAL_BUDGET_MB"); v != "" {
		var mb int64
		if _, err := fmt.Sscan(v, &mb); err != nil {
			t.Fatal(err)
		}
		budget = mb << 20
	}
	names, err := listDayFiles(src)
	if err != nil {
		t.Fatal(err)
	}
	type file struct {
		name string
		size int64
	}
	var files []file
	for _, n := range names {
		st, err := os.Stat(filepath.Join(src, n))
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, file{n, st.Size()})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].size < files[j].size })
	dir := t.TempDir()
	var total int64
	var picked []string
	for _, f := range files {
		if total+f.size > budget {
			break
		}
		b, err := os.ReadFile(filepath.Join(src, f.name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, f.name), b, 0o600); err != nil {
			t.Fatal(err)
		}
		total += f.size
		picked = append(picked, f.name)
	}
	if len(picked) == 0 {
		t.Fatal("no day file fits the budget")
	}
	hits, _, err := seatIndex(dir)
	if err != nil {
		t.Fatal(err)
	}
	var seats []string
	records := 0
	for s := range hits {
		seats = append(seats, s)
		records += len(fullScanRecords(t, dir, s))
	}
	sort.Strings(seats)
	checkSeatRecords(t, "real sample", dir, seats)
	t.Logf("real sample: %d files (%s), %d MB, %d seats, %d records — all equal",
		len(picked), strings.Join(picked, " "), total>>20, len(seats), records)
}

// TestT1085RealSpoolSeatCost measures request reads over a real spool in
// place, read-only: build the index, then each seat in
// JEVONS_T1085_COST_SEATS (comma-separated) read twice by SeatRecords and
// once by the full scan it replaces. Opt-in (JEVONS_T1085_COST_SPOOL)
// because it reads the whole spool.
func TestT1085RealSpoolSeatCost(t *testing.T) {
	dir := os.Getenv("JEVONS_T1085_COST_SPOOL")
	if dir == "" {
		t.Skip("set JEVONS_T1085_COST_SPOOL to a spool directory to measure request reads")
	}
	forgetIndex(dir)
	start, before := time.Now(), ScannedBytes()
	if _, _, err := seatIndex(dir); err != nil {
		t.Fatal(err)
	}
	t.Logf("index: %v, %d MB", time.Since(start), (ScannedBytes()-before)>>20)
	for _, seat := range strings.Split(os.Getenv("JEVONS_T1085_COST_SEATS"), ",") {
		if seat = strings.TrimSpace(seat); seat == "" {
			continue
		}
		var got [2][]Record
		for i := range got {
			start, before = time.Now(), ScannedBytes()
			recs, err := SeatRecords(dir, seat)
			if err != nil {
				t.Fatal(err)
			}
			got[i] = recs
			t.Logf("%s SeatRecords #%d: %d records, %v, %d MB", seat, i+1, len(recs), time.Since(start), (ScannedBytes()-before)>>20)
		}
		start, before = time.Now(), ScannedBytes()
		want := fullScanRecords(t, dir, seat)
		t.Logf("%s ReadSeat: %d records, %v, %d MB", seat, len(want), time.Since(start), (ScannedBytes()-before)>>20)
		if !reflect.DeepEqual(got[0], want) || !reflect.DeepEqual(got[1], want) {
			t.Fatalf("%s: SeatRecords differs from ReadSeat", seat)
		}
	}
}

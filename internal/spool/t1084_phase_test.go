// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package spool

import (
	"bytes"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/turnev"
)

// 🎯T1084 oracles. The reference is the pre-T1084 product reading: fold the
// seat's whole dated spool. The new reading folds only the seat's newest
// phase-moving record, which the index finds from appended bytes.

func fullScanPhase(dir, seat string) turnev.Phase {
	recs, err := ReadSeat(dir, seat)
	if err != nil || len(recs) == 0 {
		return turnev.PhaseUnknown
	}
	return turnev.ClassifyPhase(turnev.DecodeAll(bytes.NewReader(AsJSONL(recs))))
}

func latestPhase(dir, seat string) turnev.Phase {
	rec, ok, err := LatestPhaseRecord(dir, seat)
	if err != nil || !ok {
		return turnev.PhaseUnknown
	}
	return turnev.ClassifyPhase(turnev.DecodeAll(bytes.NewReader(AsJSONL([]Record{rec}))))
}

// forgetIndex is a daemon restart: the next read starts from a cold index.
func forgetIndex(dir string) {
	seatIndexMu.Lock()
	delete(seatIndexes, dir)
	seatIndexMu.Unlock()
}

func appendFile(t *testing.T, path, s string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(s); err != nil {
		t.Fatal(err)
	}
}

func spoolLine(seat, typ, text string) string {
	return fmt.Sprintf(`{"ts":"2026-10-19T01:02:03.000Z","seat":%q,"type":%q,"text":%q}`+"\n", seat, typ, text)
}

// checkSeats holds every seat's warm reading, and its cold one, to the full
// scan.
func checkSeats(t *testing.T, label, dir string, seats []string) {
	t.Helper()
	for _, seat := range seats {
		want := fullScanPhase(dir, seat)
		if got := latestPhase(dir, seat); got != want {
			t.Fatalf("%s: seat %q warm reading %s, full scan %s", label, seat, got, want)
		}
	}
	forgetIndex(dir)
	for _, seat := range seats {
		want := fullScanPhase(dir, seat)
		if got := latestPhase(dir, seat); got != want {
			t.Fatalf("%s: seat %q cold reading %s, full scan %s", label, seat, got, want)
		}
	}
}

func TestT1084PhaseBoundaryCases(t *testing.T) {
	type step struct {
		file, body string
		want       map[string]turnev.Phase
	}
	const d1, d2 = "events-2026-10-18.log", "events-2026-10-19.log"
	snap := `,"snapshot":{"messages":["` + strings.Repeat("s", 4096) + `"]}`
	turnEnd := func(seat string) string {
		return fmt.Sprintf(`{"ts":"2026-10-19T01:02:03.000Z","seat":%q,"type":"turn_end","text":"done"%s}`+"\n", seat, snap)
	}
	U, I, W := turnev.PhaseUnknown, turnev.PhaseIdle, turnev.PhaseWorking
	cases := map[string][]step{
		"a seat with no records": {
			{d1, spoolLine("other", "text", "hi"), map[string]turnev.Phase{"w": U, "other": W}},
		},
		"a seat with only non-phase records": {
			{d1, spoolLine("w", "accepted", "") + spoolLine("w", "ready", ""), map[string]turnev.Phase{"w": U}},
		},
		"a turn spanning a day-file boundary ends idle": {
			{d1, spoolLine("w", "text", "start"), map[string]turnev.Phase{"w": W}},
			{d2, spoolLine("w", "accepted", "") + turnEnd("w"), map[string]turnev.Phase{"w": I}},
		},
		"a turn that started yesterday is still working": {
			{d1, turnEnd("w") + spoolLine("w", "text", "next turn"), map[string]turnev.Phase{"w": W}},
			{d2, spoolLine("w", "ready", "") + spoolLine("other", "text", "x"), map[string]turnev.Phase{"w": W, "other": W}},
		},
		"rotation to a new day file": {
			{d1, turnEnd("w"), map[string]turnev.Phase{"w": I}},
			{d2, spoolLine("w", "text", "new day"), map[string]turnev.Phase{"w": W}},
			{d2, turnEnd("w"), map[string]turnev.Phase{"w": I}},
		},
		"blank and stop-token text do not move the phase": {
			{d1, turnEnd("w") + spoolLine("w", "text", "  \n ") + spoolLine("w", "text", "<|im_end|>") + spoolLine("w", "text", ""),
				map[string]turnev.Phase{"w": I}},
		},
		"a tool call is working": {
			{d1, turnEnd("w") + spoolLine("w", "tool_call", `{"cmd":"ls"}`), map[string]turnev.Phase{"w": W}},
		},
		"a partial last line is not a record until it is finished": {
			{d1, spoolLine("w", "text", "busy"), map[string]turnev.Phase{"w": W}},
			{d1, turnEnd("w")[:200], map[string]turnev.Phase{"w": W}},
			{d1, turnEnd("w")[200:], map[string]turnev.Phase{"w": I}},
		},
		"a whole record with no newline yet counts, as the full scan counts it": {
			{d1, spoolLine("w", "text", "busy"), map[string]turnev.Phase{"w": W}},
			{d1, strings.TrimSuffix(turnEnd("w"), "\n"), map[string]turnev.Phase{"w": I}},
			{d1, "\n" + spoolLine("w", "text", "again"), map[string]turnev.Phase{"w": W}},
		},
		"a malformed newest turn_end falls back to the record before it": {
			{d1, spoolLine("w", "text", "busy") + `{"ts":"x","seat":"w","type":"turn_end","text":` + "\n",
				map[string]turnev.Phase{"w": W}},
			{d2, `{"ts":"x","seat":"w","type":"turn_end","costUSD":"not a number"}` + "\n", map[string]turnev.Phase{"w": W}},
		},
		"a line that mentions a seat is not that seat's": {
			{d1, turnEnd("w") + spoolLine("other", "text", `"seat":"w"`), map[string]turnev.Phase{"w": I, "other": W}},
		},
	}
	for name, steps := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			for i, st := range steps {
				appendFile(t, filepath.Join(dir, st.file), st.body)
				for seat, want := range st.want {
					if got := fullScanPhase(dir, seat); got != want {
						t.Fatalf("step %d: fixture: full scan says %s for %q, case wants %s", i, got, seat, want)
					}
				}
				seats := make([]string, 0, len(st.want))
				for seat := range st.want {
					seats = append(seats, seat)
				}
				checkSeats(t, fmt.Sprintf("step %d", i), dir, seats)
			}
		})
	}
}

// Random spools, grown by random cuts (often mid-line), read warm after
// every cut and cold at the end.
func TestT1084PhaseMatchesFullScanOnRandomSpools(t *testing.T) {
	seats := []string{"a", "b", "jv-c"}
	gen := func(r *rand.Rand) string {
		seat := seats[r.Intn(len(seats))]
		switch r.Intn(12) {
		case 0, 1, 2:
			return spoolLine(seat, "text", "word")
		case 3:
			return spoolLine(seat, "text", []string{"", " ", "\n", "<|eot_id|>", " <|im_end|> "}[r.Intn(5)])
		case 4, 5:
			return fmt.Sprintf(`{"ts":"2026-10-19T01:02:03Z","seat":%q,"type":"turn_end","text":"t","snapshot":{"s":"%s"}}`+"\n",
				seat, strings.Repeat("z", r.Intn(70000)))
		case 6:
			return spoolLine(seat, "tool_call", `{"x":1}`)
		case 7:
			return spoolLine(seat, []string{"accepted", "ready", "tool_start", "turn", "error"}[r.Intn(5)], "")
		case 8:
			return fmt.Sprintf(`{"ts":"x","seat":%q,"type":"turn_end","text":`+"\n", seat) // malformed
		case 9:
			return fmt.Sprintf(`{"ts":"x","seat":%q,"type":"text","costUSD":"bad","text":"y"}`+"\n", seat)
		case 10:
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
				time.Sleep(time.Millisecond) // a distinct mtime per append
				for _, seat := range seats {
					if got, want := latestPhase(dir, seat), fullScanPhase(dir, seat); got != want {
						t.Fatalf("iter %d: seat %q reads %s, full scan %s", iter, seat, got, want)
					}
				}
			}
		}
		checkSeats(t, fmt.Sprintf("iter %d final", iter), dir, seats)
	}
}

// Bytes read: a quiet call reads nothing, an append reads the append (and a
// new turn_end once more, to decode it), a cold start reads the spool once
// for all seats rather than once per seat.
func TestT1084LatestPhaseRecordReadsAppendedBytesOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events-2026-10-19.log")
	var b strings.Builder
	seats := []string{"a", "b", "c", "d"}
	for i := 0; i < 50; i++ {
		for _, seat := range seats {
			b.WriteString(spoolLine(seat, "text", "w"))
			fmt.Fprintf(&b, `{"ts":"2026-10-19T01:02:03Z","seat":%q,"type":"turn_end","text":"t","snapshot":{"s":"%s"}}`+"\n",
				seat, strings.Repeat("z", 20000))
		}
	}
	appendFile(t, path, b.String())
	size := int64(b.Len())

	before := scannedBytes.Load()
	for _, seat := range seats {
		if got := latestPhase(dir, seat); got != turnev.PhaseIdle {
			t.Fatalf("seat %s: %s, want idle", seat, got)
		}
	}
	cold := scannedBytes.Load() - before
	// One scan of the file, plus each seat's newest turn_end decoded once.
	if limit := size + int64(len(seats))*21000; cold > limit {
		t.Fatalf("cold start read %d bytes of a %d-byte spool (limit %d)", cold, size, limit)
	}

	before = scannedBytes.Load()
	for _, seat := range seats {
		latestPhase(dir, seat)
	}
	if n := scannedBytes.Load() - before; n != 0 {
		t.Fatalf("a quiet call read %d bytes", n)
	}

	add := spoolLine("b", "text", "more")
	appendFile(t, path, add)
	time.Sleep(time.Millisecond)
	before = scannedBytes.Load()
	for _, seat := range seats {
		latestPhase(dir, seat)
	}
	if got := latestPhase(dir, "b"); got != turnev.PhaseWorking {
		t.Fatalf("b after append: %s, want working", got)
	}
	if n := scannedBytes.Load() - before; n != int64(len(add)) {
		t.Fatalf("an append of %d bytes read %d", len(add), n)
	}
}

// TestT1084RealSpoolSample is the equivalence oracle on real records. Real
// spool content is conversation text, so it is never committed: set
// JEVONS_T1084_REAL_SPOOL to a spool directory and the test copies up to
// JEVONS_T1084_REAL_BUDGET_MB (default 400) of its smallest day files into a
// temp dir, read-only on the source. It then grows a copy of the three
// smallest by cuts that land mid-line, checking every seat after each cut.
func TestT1084RealSpoolSample(t *testing.T) {
	src := os.Getenv("JEVONS_T1084_REAL_SPOOL")
	if src == "" {
		t.Skip("set JEVONS_T1084_REAL_SPOOL to a spool directory to run the real-sample oracle")
	}
	budget := int64(400) << 20
	if v := os.Getenv("JEVONS_T1084_REAL_BUDGET_MB"); v != "" {
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
	var picked []file
	var total int64
	for _, f := range files {
		if total+f.size > budget {
			break
		}
		picked = append(picked, f)
		total += f.size
	}
	if len(picked) == 0 {
		t.Fatal("no day file fits the budget")
	}
	copyPrefix := func(dst, name string, n int64) {
		in, err := os.Open(filepath.Join(src, name))
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		out, err := os.OpenFile(filepath.Join(dst, name), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		defer out.Close()
		if _, err := io.CopyN(out, in, n); err != nil {
			t.Fatal(err)
		}
	}
	seatsIn := func(dir string) []string {
		hits, _, err := seatIndex(dir)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for s := range hits {
			out = append(out, s)
		}
		sort.Strings(out)
		return out
	}

	// Whole files: every seat, warm and cold.
	dir := t.TempDir()
	var pickedNames []string
	for _, f := range picked {
		copyPrefix(dir, f.name, f.size)
		pickedNames = append(pickedNames, f.name)
	}
	seats := seatsIn(dir)
	counts := map[turnev.Phase]int{}
	for _, seat := range seats {
		counts[fullScanPhase(dir, seat)]++
	}
	checkSeats(t, "whole sample", dir, seats)
	t.Logf("whole sample: %d files (%s), %d MB, %d seats, full-scan phases %v — all equal",
		len(picked), strings.Join(pickedNames, " "), total>>20, len(seats), counts)

	// Growth: the three smallest files, re-copied as growing prefixes cut
	// at random offsets (mostly mid-line), in date order.
	grow := picked
	if len(grow) > 3 {
		grow = grow[:3]
	}
	sort.Slice(grow, func(i, j int) bool { return grow[i].name < grow[j].name })
	gdir := t.TempDir()
	r := rand.New(rand.NewSource(1084))
	checks := 0
	for _, f := range grow {
		cuts := []int64{f.size}
		for i := 0; i < 8; i++ {
			cuts = append(cuts, r.Int63n(f.size+1))
		}
		sort.Slice(cuts, func(i, j int) bool { return cuts[i] < cuts[j] })
		for _, cut := range cuts {
			copyPrefix(gdir, f.name, cut)
			time.Sleep(time.Millisecond)
			for _, seat := range seatsIn(gdir) {
				if got, want := latestPhase(gdir, seat), fullScanPhase(gdir, seat); got != want {
					t.Fatalf("growth %s cut %d: seat %q reads %s, full scan %s", f.name, cut, seat, got, want)
				}
				checks++
			}
		}
	}
	t.Logf("growth: %d seat readings over %d cuts of %d files — all equal", checks, 9*len(grow), len(grow))
}

// TestT1084RealSpoolColdCost measures a cold start over a real spool in
// place, read-only: every seat's reading from an empty index, then a quiet
// second pass. Opt-in (JEVONS_T1084_COLD_SPOOL) because it reads the whole
// spool.
func TestT1084RealSpoolColdCost(t *testing.T) {
	dir := os.Getenv("JEVONS_T1084_COLD_SPOOL")
	if dir == "" {
		t.Skip("set JEVONS_T1084_COLD_SPOOL to a spool directory to measure a cold start")
	}
	forgetIndex(dir)
	start, before := time.Now(), scannedBytes.Load()
	hits, _, err := seatIndex(dir)
	if err != nil {
		t.Fatal(err)
	}
	indexed, indexBytes := time.Since(start), scannedBytes.Load()-before
	counts := map[turnev.Phase]int{}
	for seat := range hits {
		counts[latestPhase(dir, seat)]++
	}
	cold, coldBytes := time.Since(start), scannedBytes.Load()-before
	start, before = time.Now(), scannedBytes.Load()
	for seat := range hits {
		latestPhase(dir, seat)
	}
	t.Logf("cold: index %v (%d MB), all %d seats %v (%d MB), phases %v; warm pass %v (%d bytes)",
		indexed, indexBytes>>20, len(hits), cold, coldBytes>>20, counts, time.Since(start), scannedBytes.Load()-before)
}

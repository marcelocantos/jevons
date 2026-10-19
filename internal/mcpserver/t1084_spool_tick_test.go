// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/discovery"
	"github.com/marcelocantos/jevons/internal/spool"
	"github.com/marcelocantos/jevons/internal/turnev"
)

// fullScanSpoolPhase is the pre-🎯T1084 answer: decode the seat's whole
// dated spool and fold it. The equivalence oracles hold the product path to
// it.
func fullScanSpoolPhase(t *testing.T, dir, seat string) turnev.Phase {
	t.Helper()
	recs, err := spool.ReadSeat(dir, seat)
	if err != nil || len(recs) == 0 {
		return turnev.PhaseUnknown
	}
	return turnev.ClassifyPhase(turnev.DecodeAll(bytes.NewReader(spool.AsJSONL(recs))))
}

// 🎯T1084: the 30-second seat-observation tick classified every sidecar seat
// by reading its whole dated spool. With 55 seats over 11 GB a pass never
// fitted in its interval. A tick must cost what was appended since the last
// one, not what the spool weighs.
func TestT1084SeatObservationTickReadsOnlyAppendedBytes(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(spool.DirEnv, dir)

	// A turn_end carries the seat's whole agent state, which is most of
	// the spool's weight.
	snapshot := `{"messages":["` + strings.Repeat("s", 64*1024) + `"]}`
	const seats = 40
	days := []string{"events-2026-10-17.log", "events-2026-10-18.log", "events-2026-10-19.log"}
	for di, day := range days {
		var b strings.Builder
		for turn := 0; turn < 3; turn++ {
			for i := 0; i < seats; i++ {
				seat := fmt.Sprintf("jv-seat-%02d", i)
				ts := fmt.Sprintf("2026-10-%02dT%02d:00:00.000Z", 17+di, turn)
				fmt.Fprintf(&b, `{"ts":%q,"seat":%q,"type":"accepted"}`+"\n", ts, seat)
				fmt.Fprintf(&b, `{"ts":%q,"seat":%q,"type":"text","text":"working on it"}`+"\n", ts, seat)
				// Odd seats end the last turn of the last day mid-flight.
				if di == len(days)-1 && turn == 2 && i%2 == 1 {
					continue
				}
				fmt.Fprintf(&b, `{"ts":%q,"seat":%q,"type":"turn_end","text":"done","snapshot":%s}`+"\n", ts, seat, snapshot)
			}
		}
		if err := os.WriteFile(filepath.Join(dir, day), []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var total int64
	for _, day := range days {
		st, err := os.Stat(filepath.Join(dir, day))
		if err != nil {
			t.Fatal(err)
		}
		total += st.Size()
	}

	defs := make([]claudia.AgentDef, seats)
	for i := range defs {
		defs[i] = claudia.AgentDef{Name: fmt.Sprintf("jv-seat-%02d", i), Provider: "anthropic", SessionID: "sid"}
	}
	tick := func() (map[string]turnev.Phase, int64) {
		before := spool.ScannedBytes()
		got := map[string]turnev.Phase{}
		for _, d := range defs {
			got[d.Name] = classifyAgentSessionPhase(d, discovery.Roots{})
		}
		return got, spool.ScannedBytes() - before
	}
	want := func() map[string]turnev.Phase {
		out := map[string]turnev.Phase{}
		for _, d := range defs {
			out[d.Name] = fullScanSpoolPhase(t, dir, d.Name)
		}
		return out
	}
	same := func(label string, got, want map[string]turnev.Phase) {
		t.Helper()
		for name, w := range want {
			if got[name] != w {
				t.Fatalf("%s: %s classified %s, full scan says %s", label, name, got[name], w)
			}
		}
	}

	first, cold := tick()
	same("cold tick", first, want())
	if first["jv-seat-00"] != turnev.PhaseIdle || first["jv-seat-01"] != turnev.PhaseWorking {
		t.Fatalf("fixture readings %s/%s, want idle/working", first["jv-seat-00"], first["jv-seat-01"])
	}
	t.Logf("cold tick read %d bytes of a %d-byte spool for %d seats", cold, total, seats)

	// A quiet tick: nothing was appended, so nothing is read.
	quiet, n := tick()
	same("quiet tick", quiet, first)
	if n != 0 {
		t.Fatalf("a tick with no appends read %d bytes (spool is %d bytes)", n, total)
	}

	// A small append: the tick reads about what was appended.
	last := filepath.Join(dir, days[len(days)-1])
	appended := `{"ts":"2026-10-19T03:00:00.000Z","seat":"jv-seat-01","type":"turn_end","text":"done","snapshot":{}}` + "\n" +
		`{"ts":"2026-10-19T03:00:01.000Z","seat":"jv-seat-02","type":"text","text":"again"}` + "\n"
	f, err := os.OpenFile(last, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(appended); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	grown, n := tick()
	same("tick after append", grown, want())
	if grown["jv-seat-01"] != turnev.PhaseIdle || grown["jv-seat-02"] != turnev.PhaseWorking {
		t.Fatalf("after append %s/%s, want idle/working", grown["jv-seat-01"], grown["jv-seat-02"])
	}
	// The appended bytes are scanned once; a new turn_end is decoded once
	// more to confirm it is a record. Nothing older is read.
	if limit := int64(2 * len(appended)); n > limit {
		t.Fatalf("a tick after a %d-byte append read %d bytes (limit %d, spool %d)", len(appended), n, limit, total)
	}
}

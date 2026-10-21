// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/spool"
)

// 🎯T1085: the materialized-model recovery check asks whether the seat has
// conversation in the dated spool. It reads the seat's lines up to the first
// answer, not the whole spool, and answers as the full read answered.
func TestT1085SpoolConversationEvidence(t *testing.T) {
	old := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano)
	recent := time.Now().UTC().Format(time.RFC3339Nano)
	snap := strings.Repeat("s", 80000)
	line := func(seat, ts, typ string) string {
		if typ == "turn_end" {
			return fmt.Sprintf(`{"ts":%q,"seat":%q,"type":"turn_end","text":"t","snapshot":{"s":%q}}`, ts, seat, snap) + "\n"
		}
		return fmt.Sprintf(`{"ts":%q,"seat":%q,"type":%q,"text":"x"}`, ts, seat, typ) + "\n"
	}
	dir := t.TempDir()
	var total int64
	own := map[string]int64{}
	write := func(name, body string) {
		f, err := os.OpenFile(filepath.Join(dir, name), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if _, err := f.WriteString(body); err != nil {
			t.Fatal(err)
		}
		total += int64(len(body))
	}
	add := func(name, seat, ts, typ string) {
		l := line(seat, ts, typ)
		own[seat] += int64(len(l))
		write(name, l)
	}
	// Thirty busy seats whose turn_ends are most of the spool.
	for day := 17; day <= 19; day++ {
		name := fmt.Sprintf("events-2026-10-%d.log", day)
		for turn := 0; turn < 3; turn++ {
			for s := 0; s < 30; s++ {
				seat := fmt.Sprintf("jv-seat-%02d", s)
				add(name, seat, old, "accepted")
				add(name, seat, old, "text")
				add(name, seat, old, "turn_end")
			}
		}
	}
	// The cases.
	add("events-2026-10-18.log", "only-old-accepted", old, "accepted")
	add("events-2026-10-18.log", "recent-accepted", recent, "accepted")
	add("events-2026-10-19.log", "recent-accepted", recent, "text")
	add("events-2026-10-18.log", "tool-result", old, "ready")
	add("events-2026-10-19.log", "tool-result", old, "tool_result")
	add("events-2026-10-19.log", "zero-ts-accepted", "", "accepted")
	add("events-2026-10-19.log", "unrelated", old, "ready")

	const evidence = "conversation evidence in dated spool; preserve history and recover the model with jevons_agent_switch_model"
	const inFlight = "accepted send may still be in flight; wait for a model reply or inspect history before retry"
	cases := map[string]string{
		"jv-seat-04":        evidence,
		"only-old-accepted": "",
		"recent-accepted":   inFlight,
		"tool-result":       evidence,
		"zero-ts-accepted":  inFlight,
		"unrelated":         "",
		"no-such-seat":      "",
	}
	spool.SeatHasHistory(dir, "jv-seat-04")
	for seat, want := range cases {
		before := spool.ScannedBytes()
		got := spoolConversationEvidence(dir, seat)
		read := spool.ScannedBytes() - before
		if got != want {
			t.Errorf("seat %q: %q, want %q", seat, got, want)
		}
		if limit := 2 * own[seat]; read > limit {
			t.Errorf("seat %q: read %d bytes of a %d-byte spool; its own lines are %d (limit %d)", seat, read, total, own[seat], limit)
		}
	}
}

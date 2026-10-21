// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package transcript

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/discovery"
	"github.com/marcelocantos/jevons/internal/spool"
)

// 🎯T1099: a sidecar seat streams one spool "text" line per token delta.
// jevons_transcript_read must fold consecutive deltas of one assistant
// message into a single turn — the same row boundaries the cockpit shows —
// not report one turn per delta.
func TestT1099DeltaStreamFoldsIntoOneTurnPerMessage(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(spool.DirEnv, dir)

	const seat = "jv-seat-delta"
	deltas1 := []string{"The ", "capital ", "of ", "France ", "is ", "Paris."}
	deltas2 := []string{"Second ", "message ", "after ", "a ", "tool ", "call."}

	var b strings.Builder
	write := func(line string) { b.WriteString(line + "\n") }
	write(`{"ts":"2026-10-20T01:00:00.000Z","seat":"jv-seat-delta","type":"accepted"}`)
	for i, d := range deltas1 {
		write(fmt.Sprintf(`{"ts":"2026-10-20T01:00:%02d.000Z","seat":"jv-seat-delta","type":"text","text":%q}`, i+1, d))
	}
	write(`{"ts":"2026-10-20T01:00:10.000Z","seat":"jv-seat-delta","type":"turn_end","text":"","costUSD":0.01}`)
	write(`{"ts":"2026-10-20T01:00:11.000Z","seat":"jv-seat-delta","type":"tool_call","call_id":"c1","name":"Bash","text":"{\"cmd\":\"ls\"}"}`)
	for i, d := range deltas2 {
		write(fmt.Sprintf(`{"ts":"2026-10-20T01:00:%02d.000Z","seat":"jv-seat-delta","type":"text","text":%q}`, 20+i, d))
	}
	write(`{"ts":"2026-10-20T01:00:30.000Z","seat":"jv-seat-delta","type":"turn_end","text":"","costUSD":0.01}`)

	if err := os.WriteFile(filepath.Join(dir, "events-2026-10-20.log"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	reader := NewReaderRoots(discovery.Roots{})
	turns, err := reader.ReadForSeat(seat, "")
	if err != nil {
		t.Fatal(err)
	}

	if len(turns) != 2 {
		t.Fatalf("turns = %d, want 2 (one per message, not one per delta): %#v", len(turns), turns)
	}

	want1 := strings.Join(deltas1, "")
	want2 := strings.Join(deltas2, "")
	if got, _ := turns[0]["text"].(string); got != want1 {
		t.Fatalf("turn 1 text = %q, want %q", got, want1)
	}
	if got, _ := turns[1]["text"].(string); got != want2 {
		t.Fatalf("turn 2 text = %q, want %q", got, want2)
	}
}

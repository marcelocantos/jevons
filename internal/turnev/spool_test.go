// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package turnev

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/marcelocantos/jevons/internal/spool"
)

func TestClassifyPhaseReadsDatedSpool(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "events-2026-09-25.log"), []byte(
		`{"ts":"2026-09-25T00:00:00.000Z","seat":"w","type":"text","text":"working"}`+"\n"+
			`{"ts":"2026-09-25T00:00:01.000Z","seat":"w","type":"turn_end","text":"done"}`+"\n",
	), 0o644); err != nil {
		t.Fatal(err)
	}
	recs, err := spool.ReadSeat(dir, "w")
	if err != nil || len(recs) == 0 {
		t.Fatalf("ReadSeat: %v %#v", err, recs)
	}
	got := ClassifyPhase(DecodeAll(bytes.NewReader(spool.AsJSONL(recs))))
	if got != PhaseIdle {
		t.Fatalf("spool turn_end classified %s, want idle", got)
	}
}

func TestPhaseFromFileReadsSpoolView(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "events-2026-09-25.log"), []byte(
		`{"ts":"2026-09-25T00:00:00.000Z","seat":"w","type":"tool_call","name":"jevons_agent_list","call_id":"c1","text":"{}"}`+"\n",
	), 0o644); err != nil {
		t.Fatal(err)
	}
	path, err := spool.EnsureView(dir, "w")
	if err != nil || path == "" {
		t.Fatalf("EnsureView: %v %q", err, path)
	}
	if got := PhaseFromFile(path); got != PhaseWorking {
		t.Fatalf("spool view classified %s, want working", got)
	}
}

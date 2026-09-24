// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package transcript

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/marcelocantos/jevons/internal/discovery"
)

func TestReadForSeatUsesSpoolNotSessionID(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("JEVONS_SPOOL_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "events-2026-09-25.log"), []byte(
		`{"ts":"2026-09-25T00:00:00.000Z","seat":"jevons-po","type":"text","text":"from spool"}`+"\n"+
			`{"ts":"2026-09-25T00:00:01.000Z","seat":"jevons-po","type":"turn_end","text":"from spool"}`+"\n",
	), 0o644); err != nil {
		t.Fatal(err)
	}
	reader := NewReaderRoots(discovery.Roots{})
	turns, err := reader.ReadForSeat("jevons-po", "00000000-0000-4000-8000-000000000000")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, turn := range turns {
		if text, _ := turn["text"].(string); text == "from spool" {
			found = true
		}
	}
	if !found {
		t.Fatalf("ReadForSeat missed the spool: %#v", turns)
	}
}

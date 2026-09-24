// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package turndepth

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDepthReadsSpoolNotVendorJSONL(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "events-2026-09-25.log"), []byte(
		`{"ts":"2026-09-25T00:00:00.000Z","seat":"w","type":"tool_call","name":"jevons_job"}`+"\n"+
			`{"ts":"2026-09-25T00:00:01.000Z","seat":"w","type":"tool_call","name":"jevons_agent_send"}`+"\n",
	), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := Depth(dir, "w"); got != 2 {
		t.Fatalf("Depth = %d, want 2", got)
	}
	if got := Depth(dir, "missing"); got != 0 {
		t.Fatalf("missing seat Depth = %d", got)
	}
}

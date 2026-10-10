// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package ownercomms

import (
	"encoding/json"
	"os"
	"testing"
)

func TestRealSessionSampleAccounting(t *testing.T) {
	b, err := os.ReadFile("testdata/t1054_sample.json")
	if err != nil {
		t.Fatal(err)
	}
	var sample struct {
		StopCandidates []struct {
			Line       int      `json:"line"`
			StopReason string   `json:"stop_reason"`
			SHA        string   `json:"text_sha256"`
			Category   Category `json:"category"`
		} `json:"stop_candidates"`
		InterimFragments []struct {
			Line       int    `json:"line"`
			StopReason string `json:"stop_reason"`
		} `json:"interim_fragments"`
	}
	if err := json.Unmarshal(b, &sample); err != nil {
		t.Fatal(err)
	}
	if len(sample.StopCandidates) != 36 || len(sample.InterimFragments) != 4 {
		t.Fatalf("sample units: stops=%d interim=%d", len(sample.StopCandidates), len(sample.InterimFragments))
	}
	send, none := 0, 0
	for _, row := range sample.StopCandidates {
		if row.StopReason != "stop" || len(row.SHA) != 64 {
			t.Fatalf("invalid provenance on line %d", row.Line)
		}
		if row.Category.Send() {
			send++
		} else if row.Category == Routine {
			none++
		} else {
			t.Fatalf("unknown category line %d", row.Line)
		}
	}
	for _, f := range sample.InterimFragments {
		if f.StopReason != "toolUse" {
			t.Fatalf("line %d is not interim", f.Line)
		}
	}
	if send != 1 || none != 35 {
		t.Fatalf("provisional stop-only count send=%d none=%d", send, none)
	}
}

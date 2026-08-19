// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package ctxcap

import (
	"strings"
	"testing"
	"time"
)

func TestFormatUnworkableNoticeNamesSizeCeilingCadence(t *testing.T) {
	d := Decision{
		Agent:   "jv-t417-ceiling",
		Verdict: VerdictHold,
		Context: 121_586,
		Ceiling: 100_000,
		Reason:  "context 121586 exceeds ceiling 100000 but last rotation was 7m59s ago (min 30m0s) — holding rather than thrashing",
	}
	text := FormatUnworkableNotice(d, 30*time.Minute, 7*time.Minute+59*time.Second)
	for _, want := range []string{
		"unworkable",
		"jv-t417-ceiling",
		"121586",
		"100000",
		"30m0s", // cadence
		"7m59s", // since last
		"not being re-run on a loop",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("notice missing %q:\n%s", want, text)
		}
	}
}

func TestFormatUnworkableNoticeUnknownRotation(t *testing.T) {
	d := Decision{Agent: "a", Verdict: VerdictCompact, Context: 200_000, Ceiling: 100_000}
	text := FormatUnworkableNotice(d, DefaultMinInterval, 0)
	if !strings.Contains(text, "none recorded") {
		t.Fatalf("want unknown-rotation wording:\n%s", text)
	}
	if !strings.Contains(text, DefaultMinInterval.Round(time.Second).String()) {
		t.Fatalf("want default cadence in notice:\n%s", text)
	}
}

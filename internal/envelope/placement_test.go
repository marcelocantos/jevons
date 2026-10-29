// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package envelope

import (
	"strings"
	"testing"
)

func TestPlacementProseAfterTrailingFence(t *testing.T) {
	mid := trailingFinish + "\nThat was the other seat's report, not mine.\n"
	reason, ok := PlacementRejectReason(mid)
	if !ok || reason != PlacementProseAfterFence {
		t.Fatalf("reason=%q ok=%v", reason, ok)
	}
	if n := PlacementCorrectionNotice(mid); !strings.Contains(n, "placement_prose_after_fence") {
		t.Fatalf("notice missing reason: %q", n)
	}
}

func TestPlacementNoNoticeWithoutFence(t *testing.T) {
	prose := "Re-verifying after restart with no envelope."
	if _, ok := PlacementRejectReason(prose); ok {
		t.Fatal("prose without fence must not get placement correction")
	}
	if PlacementCorrectionNotice(prose) != "" {
		t.Fatal("notice on prose")
	}
}

func TestPlacementNotLine1OrTrailingIncomplete(t *testing.T) {
	incomplete := "Summary first.\n\n```jevons\njevons: kind finish-report\njevons: target T1190\n"
	reason, ok := PlacementRejectReason(incomplete)
	if !ok || reason != PlacementNotLine1OrTrailing {
		t.Fatalf("reason=%q ok=%v", reason, ok)
	}
}

func TestPlacementSlotErrorUsesCorrectionNoticeNotPlacement(t *testing.T) {
	bad := "```jevons\njevons: kind finish-report\njevons: status done\njevons: target T1\njevons: oracle sha=2ca7a7d4186c5d83b5fac4c467d1da9c192892b7\njevons: silent-ledger none\n```\n"
	if _, ok := PlacementRejectReason(bad); ok {
		t.Fatal("slot error must not classify as placement reject")
	}
	if PlacementCorrectionNotice(bad) != "" {
		t.Fatal("slot error must use CorrectionNotice path")
	}
	if CorrectionNotice(bad) == "" {
		t.Fatal("want slot correction notice")
	}
}

func TestTrailingValidNoPlacementReject(t *testing.T) {
	if _, ok := PlacementRejectReason(trailingFinish); ok {
		t.Fatal("accepted trailing envelope must not be placement-rejected")
	}
}

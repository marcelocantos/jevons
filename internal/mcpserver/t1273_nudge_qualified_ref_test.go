// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"regexp"
	"strings"
	"testing"
)

// 🎯T1273: idle nudges reach product owners of other repositories. A bare
// "🎯T104" there reads as a target in that repo's ledger: on 2026-10-29
// pigeon-po parked itself because the nudge "repeatedly specifies 🎯T104,
// absent from pigeon bullseye ledger". The Jevons doctrine reference is
// qualified so it cannot be read as the recipient's own target.
func TestT1273IdleNudgeQualifiesJevonsDoctrineRef(t *testing.T) {
	bare := regexp.MustCompile(`🎯T104\b`)
	for _, kind := range []IdleNudgeKind{IdleNudgeKindContinue, IdleNudgeKindFullBrief} {
		// The standing fleet brief is Jevons doctrine under its own headings;
		// the nudge's own lines are what read as the recipient's mission.
		text := strings.TrimPrefix(FormatIdleNudgeText(IdleNudgeTextArgs{Name: "pigeon-po", Kind: kind}), FleetStandingBrief)
		for _, m := range bare.FindAllStringIndex(text, -1) {
			if m[0] < len("jevons/") || text[m[0]-len("jevons/"):m[0]] != "jevons/" {
				t.Fatalf("%s nudge carries an unqualified 🎯T104: %q", kind, text[max(0, m[0]-40):min(len(text), m[1]+20)])
			}
		}
	}
}

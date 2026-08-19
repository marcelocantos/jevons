// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate

import (
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/shaevidence"
)

func TestFlagUnreachableSHAs(t *testing.T) {
	check := shaevidence.CheckFunc(func(sha string) shaevidence.Reachability {
		switch sha {
		case "aa11111":
			return shaevidence.Ancestor
		case "bb22222":
			return shaevidence.Rewritten
		case "cc33333":
			return shaevidence.Missing
		default:
			return shaevidence.Missing
		}
	})
	report := strings.Join([]string{
		"Done. SHA aa11111 lands the fix.",
		"Also cited SHA bb22222 which was amended away.",
		"And commit cc33333 never existed here.",
		"GATE t exit=0 GREEN id=9f13c0a2",
	}, "\n")

	flags := FlagUnreachableSHAs(report, check)
	if len(flags) != 2 {
		t.Fatalf("got %d flags %#v, want 2", len(flags), flags)
	}
	for _, f := range flags {
		if f.Kind != FlagSHAUnreachable {
			t.Errorf("kind=%s, want sha_unreachable", f.Kind)
		}
		if !strings.Contains(f.Detail, "🎯T427") {
			t.Errorf("detail missing T427: %s", f.Detail)
		}
	}
	if !strings.Contains(flags[0].Detail, "rewritten") {
		t.Errorf("first flag should name rewritten: %s", flags[0].Detail)
	}
	if !strings.Contains(flags[1].Detail, "does not exist") {
		t.Errorf("second flag should name missing: %s", flags[1].Detail)
	}
}

func TestFlagUnreachableSHAsNilCheckIsNoop(t *testing.T) {
	if flags := FlagUnreachableSHAs("SHA deadbee landed", nil); flags != nil {
		t.Fatalf("nil check must skip, got %#v", flags)
	}
}

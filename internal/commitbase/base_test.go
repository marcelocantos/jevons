// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package commitbase_test

import (
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/commitbase"
)

func TestDecideAllowsMatchingHEAD(t *testing.T) {
	v := commitbase.Decide(commitbase.Check{
		BaseSHA: "abc123",
		HeadSHA: "abc123",
	})
	if v.Refused {
		t.Fatalf("matching HEAD was refused:\n%s", v.Message)
	}
}

func TestDecideRefusesMovedHEAD(t *testing.T) {
	v := commitbase.Decide(commitbase.Check{
		BaseSHA:   "aaa111",
		HeadSHA:   "bbb222",
		LostPaths: []string{"cmd/detach/main.go", "internal/supervise/agent.go"},
	})
	if !v.Refused {
		t.Fatal("moved HEAD was allowed")
	}
	for _, want := range []string{"🎯T432", "aaa111", "bbb222", "cmd/detach/main.go", "re-seed"} {
		if !strings.Contains(v.Message, want) {
			t.Errorf("refusal missing %q:\n%s", want, v.Message)
		}
	}
	if !strings.Contains(v.Message, "update-ref") {
		t.Errorf("refusal must explain why update-ref CAS is not enough:\n%s", v.Message)
	}
}

func TestDecideRefusesEmptySeed(t *testing.T) {
	v := commitbase.Decide(commitbase.Check{HeadSHA: "bbb222"})
	if !v.Refused {
		t.Fatal("empty seed was allowed")
	}
	if !strings.Contains(v.Message, "no seed SHA") {
		t.Errorf("refusal missing empty-seed diagnosis:\n%s", v.Message)
	}
}

func TestDecideHonoursExplicitOptOut(t *testing.T) {
	v := commitbase.Decide(commitbase.Check{
		BaseSHA:  "aaa",
		HeadSHA:  "bbb",
		Disabled: true,
	})
	if v.Refused {
		t.Fatalf("disabled guard still refused:\n%s", v.Message)
	}
}

func TestOffValue(t *testing.T) {
	for _, v := range []string{"off", "OFF", "0", "false", "no", "disable"} {
		if !commitbase.OffValue(v) {
			t.Errorf("OffValue(%q) = false, want true", v)
		}
	}
	for _, v := range []string{"", "on", "1", "yes"} {
		if commitbase.OffValue(v) {
			t.Errorf("OffValue(%q) = true, want false", v)
		}
	}
}

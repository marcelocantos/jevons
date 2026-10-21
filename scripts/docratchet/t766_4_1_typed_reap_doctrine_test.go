// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/envelope"
)

// 🎯T766.4.1: the instruction surfaces describe the typed reap, stop and
// idle behaviour the code has, and stay in step with the envelope schema.

var t76641DoctrineSurfaces = []string{
	"AGENTS.md",
	"internal/config/persona.md",
	"agents-guide.md",
	"internal/cli/help_agent.md",
	"internal/mcpserver/fleet_brief.go",
}

func TestT76641TypedReapDoctrine(t *testing.T) {
	for _, rel := range t76641DoctrineSurfaces {
		body := readRepo(t, rel)
		lower := strings.ToLower(body)
		for _, need := range []string{
			"🎯T766.4.1",
			"active-wait",        // workers declare an active external wait
			"disposition",        // a stop reason is a declared disposition
			"role=product-owner", // POs are spawned with their role
			"unknown",            // a report that declares nothing is unknown
			"in-progress",        // the instructed mid-wait shape
		} {
			if !strings.Contains(body, need) {
				t.Errorf("%s: typed reap doctrine is missing %q", rel, need)
			}
		}
		for _, stale := range []string{
			"findactiveexternalwait",        // the sentence detector is gone
			"imperfect bare done",           // a bare done is unknown, not a reap
			"reads as finished work",        // a stop reason's English is not classified
			"fall back to prose heuristics", // prose is not a decision fallback
		} {
			if strings.Contains(lower, stale) {
				t.Errorf("%s still describes the pre-🎯T766.4.1 prose behaviour: %q", rel, stale)
			}
		}
	}
}

// TestT76641DocsDoNotInventWaitClasses: every active-wait class an
// instruction surface names parses in the schema, and the worker-facing
// surfaces name every class the schema has.
func TestT76641DocsDoNotInventWaitClasses(t *testing.T) {
	slot := regexp.MustCompile(`jevons: active-wait ([a-z]+)\b`)
	for _, rel := range t76641DoctrineSurfaces {
		for _, m := range slot.FindAllStringSubmatch(readRepo(t, rel), -1) {
			if _, ok := envelope.ParseWaitClass(m[1]); !ok {
				t.Errorf("%s names active-wait class %q, which internal/envelope does not have", rel, m[1])
			}
		}
	}
	for _, rel := range []string{"agents-guide.md", "internal/mcpserver/fleet_brief.go"} {
		body := readRepo(t, rel)
		for _, c := range envelope.AllWaitClasses() {
			if !strings.Contains(body, string(c)) {
				t.Errorf("%s does not name wait class %q", rel, c)
			}
		}
	}
}

// TestT76641SchemaOwnsTheWaitSlot: the slot and its contradiction rule live
// in internal/envelope, where the docs point.
func TestT76641SchemaOwnsTheWaitSlot(t *testing.T) {
	body := readRepo(t, "internal/envelope/active_wait.go")
	for _, want := range []string{"WaitClass", "ActiveWait", "ActiveWaitIn", "ParseWaitClass"} {
		if !strings.Contains(body, want) {
			t.Errorf("internal/envelope/active_wait.go missing %q", want)
		}
	}
	text := "```jevons\njevons: kind finish-report\njevons: target T1\njevons: oracle sha=abcdef0\njevons: active-wait gate t1-slow\njevons: silent-ledger none\n```\n"
	if envelope.ClassifyDone(text) != envelope.DoneUnknown {
		t.Error("a finish-report carrying active-wait must classify Unknown, as the docs say")
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/envelope"
)

// 🎯T1083: two read-only pilot workers wrote `status done`, then
// `status achieved`, because the only product-done example in the guide
// carried `status in-progress` (not done) and nothing named the done shape.
// These ratchet every instruction-file envelope example against the parser
// and the classifier, so an example a worker copies is one the daemon reaps.

var t1083Docs = []string{
	"agents-guide.md",
	"internal/cli/help_agent.md",
	"internal/config/persona.md",
	"AGENTS.md",
	"docs/architecture-current.md",
}

var t1083Fence = regexp.MustCompile("(?s)```jevons\n(.*?)\n```")

// TestT1083EnvelopeExamplesParse: every fenced jevons example in an
// instruction file parses, and every finish-report example that is not
// blocked is one ClassifyDone answers DoneYes for.
func TestT1083EnvelopeExamplesParse(t *testing.T) {
	for _, doc := range t1083Docs {
		body := readRepo(t, doc)
		for _, m := range t1083Fence.FindAllStringSubmatch(body, -1) {
			block := "```jevons\n" + m[1] + "\n```\n"
			msg, err := envelope.Parse(block)
			if msg == nil || err != nil {
				t.Errorf("%s: example does not parse (%v):\n%s", doc, err, block)
				continue
			}
			if msg.Kind != envelope.KindFinishReport || msg.Status == envelope.ProgressBlocked {
				continue
			}
			if got := envelope.ClassifyDone(block); got != envelope.DoneYes {
				t.Errorf("%s: finish-report example classifies %s, not done — a worker copying it is never reaped:\n%s", doc, got, block)
			}
		}
	}
}

// TestT1083StatusMentionsAreSchemaValues: "jevons: status X" in instruction
// text names a value the parser accepts.
func TestT1083StatusMentionsAreSchemaValues(t *testing.T) {
	re := regexp.MustCompile(`jevons: status ([A-Za-z0-9_-]+)`)
	for _, doc := range append(append([]string{}, t1083Docs...), "internal/mcpserver/fleet_brief.go") {
		for _, m := range re.FindAllStringSubmatch(readRepo(t, doc), -1) {
			if _, ok := envelope.ParseProgress(m[1]); !ok {
				t.Errorf("%s writes %q, which is not a status value", doc, m[0])
			}
		}
	}
}

// TestT1083DoneShapeTaught: the worker-facing surfaces show the done shape
// and the status enum exactly as the schema has them.
func TestT1083DoneShapeTaught(t *testing.T) {
	var vals []string
	for _, p := range envelope.ProgressValues() {
		vals = append(vals, string(p))
	}
	enum := strings.Join(vals, " | ")

	for _, doc := range []string{"agents-guide.md", "internal/cli/help_agent.md"} {
		body := readRepo(t, doc)
		if !strings.Contains(body, envelope.DoneShape) {
			t.Errorf("%s does not show envelope.DoneShape verbatim", doc)
		}
	}
	for _, doc := range []string{"agents-guide.md", "internal/config/persona.md"} {
		body := readRepo(t, doc)
		for _, want := range []string{"🎯T1083", enum, "envelope.DoneShape", "read-only"} {
			if !strings.Contains(body, want) {
				t.Errorf("%s missing T1083 marker %q", doc, want)
			}
		}
	}

	brief := readRepo(t, "internal/mcpserver/fleet_brief.go")
	if !strings.Contains(brief, "🎯T1083") || !strings.Contains(brief, "Omit status") {
		t.Error("fleet_brief.go lost the T1083 done-shape section")
	}
	if !strings.Contains(strings.Join(strings.Fields(brief), " "), enum) {
		t.Errorf("fleet_brief.go does not list the status enum %q", enum)
	}
	// The brief writes the shape as indented slot lines (no nested fence);
	// each is a DoneShape line.
	for _, line := range strings.Split(envelope.DoneShape, "\n") {
		if !strings.HasPrefix(line, envelope.Sigil) {
			continue
		}
		if !strings.Contains(brief, "    "+line+"\n") {
			t.Errorf("fleet_brief.go done shape missing slot line %q", line)
		}
	}
}

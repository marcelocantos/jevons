// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/envelope"
)

// 🎯T1122 clause 3: "Owner-visible changes carry the question in their
// review: a PR / finish-report touching ui/ or the overseer's owner-facing
// speech states what the owner does differently because of it, and the PO
// refuses one that cannot say." This ratchets the instruction text and the
// envelope schema against each other, the way 🎯T1083 does for status.

// t1122OwnerImpactDocs are the surfaces the brief required (clause 3).
var t1122OwnerImpactDocs = []string{
	"internal/roles/builtin/worker.md",
	"internal/roles/builtin/product-owner.md",
	"agents-guide.md",
	"internal/cli/help_agent.md",
	"internal/mcpserver/fleet_brief.go",
}

// TestT1122OwnerImpactDocsMentionTarget: every named surface cites 🎯T1122
// and the slot name.
func TestT1122OwnerImpactDocsMentionTarget(t *testing.T) {
	for _, doc := range t1122OwnerImpactDocs {
		body := readRepo(t, doc)
		for _, want := range []string{"🎯T1122", "owner-impact"} {
			if !strings.Contains(body, want) {
				t.Errorf("%s missing T1122 owner-impact marker %q", doc, want)
			}
		}
	}
}

// TestT1122OwnerImpactEnumMatchesSchema: the enum values named in the docs
// are exactly the values envelope.ParseOwnerImpact accepts, in sync so a
// doc cannot drift from the parser (the 🎯T1083 ratchet shape).
func TestT1122OwnerImpactEnumMatchesSchema(t *testing.T) {
	const enum = "decision|fault|plan-change|none"
	for _, want := range strings.Split(enum, "|") {
		if _, ok := envelope.ParseOwnerImpact(want); !ok {
			t.Fatalf("enum value %q used in docs does not parse as an OwnerImpact", want)
		}
	}
	// Every parser value (other than absent) appears in the enum string,
	// so the docs are not stale against a value the schema added later.
	for _, v := range []envelope.OwnerImpact{
		envelope.OwnerImpactNoneSlot, envelope.OwnerImpactDecision,
		envelope.OwnerImpactFault, envelope.OwnerImpactPlanChange,
	} {
		if !strings.Contains(enum, string(v)) {
			t.Fatalf("OwnerImpact value %q is not in the doc enum %q", v, enum)
		}
	}
	for _, doc := range []string{
		"internal/roles/builtin/worker.md",
		"internal/roles/builtin/product-owner.md",
		"agents-guide.md",
		"internal/cli/help_agent.md",
		"internal/mcpserver/fleet_brief.go",
	} {
		body := readRepo(t, doc)
		if !strings.Contains(body, enum) {
			t.Errorf("%s does not state the owner-impact enum %q", doc, enum)
		}
	}
}

// TestT1122OwnerImpactExampleParses: the agents-guide.md example (shared
// verbatim with help_agent.md) parses and declares owner-impact decision.
func TestT1122OwnerImpactExampleParses(t *testing.T) {
	const example = "```jevons\n" +
		"jevons: kind finish-report\n" +
		"jevons: target T1122\n" +
		"jevons: oracle sha=…\n" +
		"jevons: owner-impact decision\n" +
		"jevons: silent-ledger none\n" +
		"```"
	for _, doc := range []string{"agents-guide.md", "internal/cli/help_agent.md"} {
		body := readRepo(t, doc)
		if !strings.Contains(body, example) {
			t.Errorf("%s does not show the owner-impact example verbatim", doc)
		}
	}
	m, err := envelope.Parse(example)
	if m == nil || err != nil {
		t.Fatalf("the shown example does not parse: %v %v", m, err)
	}
	if !m.OwnerImpact.Declared() {
		t.Fatalf("the shown example's owner-impact %q does not classify Declared", m.OwnerImpact)
	}
}

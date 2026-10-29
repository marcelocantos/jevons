// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"strings"
	"testing"
)

// t1182DoctrineFiles are the three standing surfaces named in 🎯T1182.
var t1182DoctrineFiles = []string{
	"internal/config/persona.md",
	"agents-guide.md",
	"internal/mcpserver/fleet_brief.go",
}

// t1182PinnedPhrases are shared across all three surfaces (fleet_brief omits
// role=auditor per 🎯T536.2 universal-brief rule; persona/agents-guide add it).
var t1182PinnedPhrases = []string{
	"🎯T1182",
	"Independent verification",
	"bin/gate -clean` through the heavy lease",
	"Never choose a non-clean gate to avoid the heavy lease",
}

// TestT1182POVerificationDoctrinePinsSentence ratchets 🎯T1182: PO independent
// verification doctrine is pinned in persona, agents-guide, and fleet_brief.
func TestT1182POVerificationDoctrinePinsSentence(t *testing.T) {
	for _, doc := range t1182DoctrineFiles {
		body := readRepo(t, doc)
		for _, want := range t1182PinnedPhrases {
			if !strings.Contains(body, want) {
				t.Errorf("%s missing T1182 pin %q", doc, want)
			}
		}
		if strings.Contains(strings.ToLower(body), "if you are an auditor") {
			t.Errorf("%s must not carry auditor if-you-are doctrine", doc)
		}
	}
	guide := readRepo(t, "agents-guide.md")
	persona := readRepo(t, "internal/config/persona.md")
	for _, want := range []string{"role=auditor", "jevons_agent_start"} {
		if !strings.Contains(guide, want) {
			t.Errorf("agents-guide.md missing spawn marker %q", want)
		}
		if !strings.Contains(persona, want) {
			t.Errorf("persona.md missing spawn marker %q", want)
		}
	}
	brief := readRepo(t, "internal/mcpserver/fleet_brief.go")
	if strings.Contains(strings.ToLower(brief), "role=auditor") {
		t.Error("fleet_brief.go must not mention role=auditor in FleetStandingBrief (🎯T536.2)")
	}
}

func TestT1182POQueueBacklogMechanismPinned(t *testing.T) {
	poqueue := readRepo(t, "internal/poqueue/poqueue.go")
	for _, want := range []string{"MaxDirectAge = 5 * time.Minute", "🎯T1182"} {
		if !strings.Contains(poqueue, want) {
			t.Errorf("internal/poqueue/poqueue.go missing %q", want)
		}
	}
	policy := readRepo(t, "internal/staffops/policy.go")
	if !strings.Contains(policy, "po_queue_backlog") {
		t.Error("staffops policy missing po_queue_backlog kind")
	}
}

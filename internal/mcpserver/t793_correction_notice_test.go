// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/envelope"
)

const t793Worker = "jv-t793-probe"
const t793Parent = "jevons-po"

func t793MalformedFinish() string {
	// Only defect: silent-decision confidence written as a 1-10 rank.
	return "```jevons\n" +
		"jevons: kind finish-report\n" +
		"jevons: target T793\n" +
		"jevons: oracle sha=abcdef0123456\n" +
		"jevons: verdict GREEN\n" +
		"jevons: silent-ledger ranked\n" +
		"jevons: silent-decision confidence=4 choice=\"a rank not a probability\"\n" +
		"```\n\nDone."
}

// TestT793MalformedConfidenceNotifiesAuthorAndSkipsReap is the acceptance
// hermetic for 🎯T793: a finish-report whose only defect is a
// silent-decision confidence outside [0,1] gets the AUTHOR told the exact
// field/range on its own channel in the same cycle, and the author is not
// reaped out from under itself before it can resend.
func TestT793MalformedConfidenceNotifiesAuthorAndSkipsReap(t *testing.T) {
	self := &fakeSender{alive: true}
	parent := &fakeSender{alive: true}
	s, _ := chainServer(t, map[string]*fakeSender{
		t793Worker: self,
		t793Parent: parent,
	})
	s.registry = newLineageRegistry(t, map[string]string{
		t793Parent: "jevons",
		t793Worker: t793Parent,
	})
	reportDir := t.TempDir()
	s.SetAgentReportDir(reportDir)

	raw := t793MalformedFinish()
	if _, err := envelope.Parse(raw); err == nil {
		t.Fatal("fixture must be malformed (confidence out of range)")
	}

	sink := s.agentEventSink(t793Worker)
	sink(claudia.Event{Type: "assistant", Text: raw, StopReason: "end_turn"})

	if s.registry.Def(t793Worker) == nil {
		t.Fatal("malformed finish-report must NOT reap the author — it needs a turn to resend")
	}
	if len(self.sent) == 0 {
		t.Fatal("author must receive a correction notice on its own channel")
	}
	got := strings.ToLower(self.sent[len(self.sent)-1])
	for _, want := range []string{"confidence", "0 to 1", "resend"} {
		if !strings.Contains(got, want) {
			t.Errorf("correction notice missing %q: %q", want, got)
		}
	}
}

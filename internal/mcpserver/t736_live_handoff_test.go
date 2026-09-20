// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"os"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/envelope"
)

// 🎯T736 — the scout-to-implement handoff is admitted on the live send path,
// not only on a tidy hermetic specimen.
//
// 🎯T721 landed phaseAdvanceHandoff green and the product refused the very
// first real handoff: jevons-po sent jv-t731-stale-report a phase-implement
// spawn-brief for T731 while that seat's latest stored report was a
// scout-report for T731, and the refusal it got back ended by describing the
// exemption it was failing to apply. The stored specimen is pinned in
// testdata: its terminal envelope opens at byte 698 and its closing prose
// mentions a ```jevons fence at byte 6147, so the LastIndex reader parsed the
// sentence, failed, and answered "no scout-report".

func t736Specimen(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("testdata/t736_scout_report_specimen.txt")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// The specimen is only a specimen while it still has the two properties the
// bug needed: a real terminal scout-report, and a later prose mention of the
// fence. Pin both, so a future trim cannot quietly defang the tape.
func TestT736SpecimenShape(t *testing.T) {
	text := t736Specimen(t)
	if !strings.Contains(text, "jevons: kind scout-report") {
		t.Fatal("specimen lost its scout-report envelope")
	}
	mention := strings.LastIndex(text, "```"+envelope.FenceInfo)
	starts := envelope.FenceStarts(text)
	if len(starts) != 1 {
		t.Fatalf("specimen must have exactly one real fence opener, got %v", starts)
	}
	if mention <= starts[0] {
		t.Fatalf("specimen lost the prose fence mention after the envelope (last=%d fence=%d)", mention, starts[0])
	}
}

// The reader the T721 fix shipped: LastIndex over the raw string. Kept here
// as an executable mutant, because it is the code that was green in the
// hermetic and red in the product.
func t736LastIndexReader(text string) *envelope.Message {
	if m, err := envelope.Parse(text); m != nil && err == nil {
		return m
	}
	i := strings.LastIndex(strings.ToLower(text), "```"+envelope.FenceInfo)
	if i < 0 {
		return nil
	}
	m, err := envelope.Parse(text[i:])
	if m != nil && err == nil {
		return m
	}
	return nil
}

func TestT736LastFenceMutantGoesRed(t *testing.T) {
	text := t736Specimen(t)
	if t736LastIndexReader(text) != nil {
		t.Fatal("mutant setup: the LastIndex reader was supposed to fail on this specimen")
	}
	m := parseTerminalEnvelope(text)
	if m == nil || m.Kind != envelope.KindScoutReport {
		t.Fatalf("parseTerminalEnvelope must find the buried scout-report, got %+v", m)
	}
	if NormalizeTargetID(m.Target) != "T731" {
		t.Fatalf("scout-report target = %q, want T731", m.Target)
	}
}

// The live path, end to end through handleAgentSend: the specimen report in
// the seat's store, a workdir touch that would refuse under T597 alone, and
// the implement brief the PO actually sent.
func TestT736SpecimenReportAdmitsImplementBriefOnSendPath(t *testing.T) {
	const name = "jv-t731-stale-report"
	s, sender, _ := t597Fixture(t, name)
	t721SaveReport(t, s, name, t736Specimen(t))

	res := t597Send(t, s, name, t721ImplementBrief("T731"), false)
	if res.IsError {
		t.Fatalf("the specimen handoff must be delivered by the send path: %s", toolText(res))
	}
	if len(sender.sent) != 1 {
		t.Fatalf("handoff must reach the seat once: %v", sender.sent)
	}
}

// Second half of the acceptance: the incoming payload is read after whatever
// precedes the fence. A caller who opens with a line of prose still sent a
// spawn-brief, and the gate's verdict must not turn on their formatting.
func TestT736IncomingBriefBehindProseIsStillAHandoff(t *testing.T) {
	const name = "jv-t731-prose-prefix"
	s, sender, _ := t597Fixture(t, name)
	t721SaveReport(t, s, name, t736Specimen(t))

	buried := "Implement brief for 🎯T731 — you scouted this; build from your own ledger.\n\n" +
		t721ImplementBrief("T731")
	if m, err := envelope.Parse(buried); m != nil && err == nil {
		t.Fatal("setup: this payload was supposed to fail a line-1-only parse")
	}
	res := t597Send(t, s, name, buried, false)
	if res.IsError {
		t.Fatalf("prose-prefixed handoff must be delivered: %s", toolText(res))
	}
	if len(sender.sent) != 1 {
		t.Fatalf("handoff must reach the seat once: %v", sender.sent)
	}
}

// The guard did not go soft: a prose-prefixed spawn-brief is still a
// re-brief when it is not a same-target phase advance.
func TestT736ProsePrefixedRebriefStillRefused(t *testing.T) {
	const name = "jv-t731-prose-rebrief"
	s, sender, _ := t597Fixture(t, name)
	t721SaveReport(t, s, name, t736Specimen(t))

	buried := "Fresh mission for you.\n\n" + t721ImplementBrief("T999")
	res := t597Send(t, s, name, buried, false)
	if !res.IsError {
		t.Fatalf("a different-target spawn-brief is still a re-brief: %s", toolText(res))
	}
	if !strings.Contains(toolText(res), "re-brief refused") {
		t.Fatalf("refusal text lost: %s", toolText(res))
	}
	if len(sender.sent) != 0 {
		t.Fatalf("refused brief must not reach the seat: %v", sender.sent)
	}
}

// FenceStarts is the discriminator both readers share, so tape it directly:
// a fence opener owns its line, a sentence about one does not.
func TestT736FenceStartsIgnoresProseMentions(t *testing.T) {
	text := "intro\n" +
		"```jevons\njevons: kind ack\n```\n" +
		"prose about a ```jevons ` fence, e.g. T582's shape\n" +
		"  ```JEVONS  \njevons: kind ack\n```\n"
	got := envelope.FenceStarts(text)
	if len(got) != 2 {
		t.Fatalf("FenceStarts=%v, want the two real openers", got)
	}
	for _, off := range got {
		if !strings.HasPrefix(strings.ToLower(text[off:]), "```jevons") {
			t.Fatalf("offset %d does not point at a fence: %q", off, text[off:off+12])
		}
	}
	if envelope.FenceStarts("no fences here ```jevons inline") != nil {
		t.Fatal("an inline mention is not a fence opener")
	}
}

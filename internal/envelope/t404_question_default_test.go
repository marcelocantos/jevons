// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0
package envelope

import (
	"strings"
	"testing"
)

func t404Question() *Message {
	return &Message{Kind: KindFinishReport, Target: "T404", Status: ProgressBlocked, Blocker: "owner-decision-on-answer-surface", BlockClass: BlockOwnerDecision, QuestionRepo: "/repo", QuestionID: "answer-surface", QuestionVersion: "1", Question: "Cockpit panel or Slack thread for owner answers?", QuestionAsker: "jv-t404-auto", AnswerRoute: "jv-t404-auto", QuestionOptions: []string{"cockpit", "slack thread"}, QuestionDefault: "proceeding with cockpit unless you say otherwise", SilentLedger: SilentLedgerEmpty}
}

// 🎯T404 acceptance 1 and 3: the default-on-silence and the closed-form
// options travel as slots, survive a format/parse round trip, and change
// the envelope's fingerprint when they change.
func TestT404QuestionDefaultAndOptionsRoundTrip(t *testing.T) {
	m := t404Question()
	raw := Format(m)
	for _, want := range []string{"jevons: question-option cockpit", "jevons: question-option \"slack thread\"", "jevons: question-default \"proceeding with cockpit unless you say otherwise\""} {
		if !strings.Contains(raw, want) {
			t.Fatalf("formatted envelope lacks %q:\n%s", want, raw)
		}
	}
	parsed, err := Parse(raw)
	if err != nil {
		t.Fatalf("parse: %v\n%s", err, raw)
	}
	if parsed.QuestionDefault != m.QuestionDefault || len(parsed.QuestionOptions) != 2 || parsed.QuestionOptions[0] != "cockpit" || parsed.QuestionOptions[1] != "slack thread" {
		t.Fatalf("slots lost in round trip: %+v", parsed)
	}
	if Format(parsed) != raw {
		t.Fatalf("format is not stable:\n%s\n---\n%s", raw, Format(parsed))
	}
	changed := t404Question()
	changed.QuestionDefault = "proceeding with slack unless you say otherwise"
	if changed.SlotsFingerprint() == m.SlotsFingerprint() {
		t.Fatal("a different default must be a different envelope")
	}
	// The spelling the 🎯T404 ledger uses is accepted as an alias.
	alias, err := Parse(strings.Replace(raw, "jevons: question-default", "jevons: default-on-silence", 1))
	if err != nil || alias.QuestionDefault != m.QuestionDefault {
		t.Fatalf("default-on-silence alias: %v %+v", err, alias)
	}
}

// A question without a default is still a valid owner-decision block: the
// intake records it and the view flags it as stalling (refusing it here
// would drop the question, which is the 🎯T378 failure).
func TestT404MissingDefaultIsFlaggedNotRefused(t *testing.T) {
	m := t404Question()
	m.QuestionDefault = ""
	m.QuestionOptions = nil
	if _, err := Parse(Format(m)); err != nil {
		t.Fatalf("owner-decision block without a default refused: %v", err)
	}
}

// The slots belong to the owner-decision contract: an ordinary block
// carrying them is malformed, the same rule as the other question slots.
func TestT404DefaultSlotRequiresOwnerDecisionClass(t *testing.T) {
	m := &Message{Kind: KindFinishReport, Target: "T1", Status: ProgressBlocked, Blocker: "cross-repo", QuestionDefault: "carry on", SilentLedger: SilentLedgerEmpty}
	if _, err := Parse(Format(m)); err == nil || !strings.Contains(err.Error(), "owner-decision") {
		t.Fatalf("default on an ordinary block accepted: %v", err)
	}
	m.QuestionDefault = ""
	m.QuestionOptions = []string{"a", "b"}
	if _, err := Parse(Format(m)); err == nil || !strings.Contains(err.Error(), "owner-decision") {
		t.Fatalf("options on an ordinary block accepted: %v", err)
	}
}

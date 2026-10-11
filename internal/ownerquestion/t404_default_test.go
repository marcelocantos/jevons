// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0
package ownerquestion

import (
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/envelope"
)

// 🎯T404: the typed intake carries the asker's default-on-silence and
// options, and the paged body shows both — while a question without them
// pages byte-for-byte as it did before, so a deploy re-notifies nothing.
func TestT404IntakeCarriesDefaultAndOptions(t *testing.T) {
	repo := t.TempDir()
	m := &envelope.Message{Kind: envelope.KindFinishReport, Target: "T404", Status: envelope.ProgressBlocked, Blocker: "surface choice", BlockClass: envelope.BlockOwnerDecision, QuestionRepo: repo, QuestionID: "surface", QuestionVersion: "1", Question: "Cockpit or Slack?", QuestionAsker: "jv-t404-auto", AnswerRoute: "jv-t404-auto", QuestionOptions: []string{"cockpit", "slack"}, QuestionDefault: "cockpit unless you say otherwise", SilentLedger: envelope.SilentLedgerEmpty}
	q, ok, err := FromBlockedReport(envelope.Format(m))
	if err != nil || !ok {
		t.Fatalf("intake: %v %v", ok, err)
	}
	if q.Default != "cockpit unless you say otherwise" || len(q.Options) != 2 || q.Options[1] != "slack" {
		t.Fatalf("default/options lost: %+v", q)
	}
	if got := q.SilenceOutcome(); !strings.HasPrefix(got, "proceeds on default: cockpit") {
		t.Fatalf("silence outcome = %q", got)
	}
	body := q.NoticeBody()
	if !strings.Contains(body, "Options: cockpit | slack") || !strings.Contains(body, "If unanswered: cockpit unless you say otherwise") {
		t.Fatalf("notice body lacks options/default:\n%s", body)
	}

	m.QuestionOptions, m.QuestionDefault = nil, ""
	bare, ok, err := FromBlockedReport(envelope.Format(m))
	if err != nil || !ok {
		t.Fatalf("bare intake: %v %v", ok, err)
	}
	if bare.NoticeBody() != bare.Text {
		t.Fatalf("a question without default/options must page as plain text, got:\n%s", bare.NoticeBody())
	}
	if bare.SilenceOutcome() != NoSilenceDefault {
		t.Fatalf("missing default not flagged: %q", bare.SilenceOutcome())
	}
}

// An owner gate is closed-form by construction: accept or reject.
func TestT404GateIsClosedForm(t *testing.T) {
	q, err := FromOwnerGate(t.TempDir(), "T22", "Does the aside selection stay put after a hard reload?", "jevons-po")
	if err != nil {
		t.Fatal(err)
	}
	if len(q.Options) != 2 || q.Options[0] != "accept" || q.Options[1] != "reject" {
		t.Fatalf("gate options = %v", q.Options)
	}
}

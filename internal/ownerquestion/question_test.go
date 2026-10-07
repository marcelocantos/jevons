// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0
package ownerquestion

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/envelope"
)

func TestBlockedReportOwnerDecisionIntake(t *testing.T) {
	repo := t.TempDir()
	m := &envelope.Message{Kind: envelope.KindFinishReport, Target: "T1028.1", Status: envelope.ProgressBlocked, Blocker: "waiting for a scope call", BlockClass: envelope.BlockOwnerDecision, QuestionRepo: repo, QuestionID: "scope", QuestionVersion: "2", Question: "Do we ship both shapes?", QuestionAsker: "jv-worker", AnswerRoute: "jevons-po", SilentLedger: envelope.SilentLedgerEmpty}
	raw := envelope.Format(m)
	canonical, err := CanonicalRepo(repo)
	if err != nil {
		t.Fatal(err)
	}
	got, ok, err := FromBlockedReport(raw)
	if err != nil || !ok {
		t.Fatalf("typed intake: %v %v", ok, err)
	}
	if got.Identity.Repo != canonical || got.Identity.Target != "T1028.1" || got.Identity.ID != "scope" || got.Identity.Version != "2" || got.Asker != "jv-worker" || got.AnswerRoute != "jevons-po" || got.State != Open {
		t.Fatalf("wrong question: %+v", got)
	}
	parsed, err := envelope.Parse(raw)
	if err != nil || parsed.Question != m.Question || envelope.Format(parsed) != raw {
		t.Fatalf("round trip: %v\n%s", err, raw)
	}
	m.QuestionAsker = ""
	if _, ok, err := FromBlockedReport(envelope.Format(m)); ok || err == nil || !strings.Contains(err.Error(), "question-asker") {
		t.Fatalf("missing asker: ok=%v err=%v", ok, err)
	}
	m.QuestionAsker = "jv-worker"
	m.QuestionRepo = filepath.Join(repo, "nonexistent")
	if _, _, err := FromBlockedReport(envelope.Format(m)); err == nil {
		t.Fatal("invalid repo accepted")
	}
}

func TestOrdinaryBlocksNeverCreateOwnerQuestions(t *testing.T) {
	for _, blocker := range []string{"cross-repo claudia implementation", "design-gated pending design discussion", "owner go-ahead on restart"} {
		m := &envelope.Message{Kind: envelope.KindFinishReport, Target: "T1", Status: envelope.ProgressBlocked, Blocker: blocker, SilentLedger: envelope.SilentLedgerEmpty}
		for _, class := range []envelope.BlockClass{"", envelope.BlockExternal} {
			m.BlockClass = class
			if _, ok, err := FromBlockedReport(envelope.Format(m)); ok || err != nil {
				t.Fatalf("%q class=%q promoted: %v %v", blocker, class, ok, err)
			}
		}
	}
}

func TestGateIdentityStableAcrossRepeatsAndChanges(t *testing.T) {
	repo := t.TempDir()
	a, err := FromOwnerGate(repo, "T2", "Does this look right?", "jevons-po")
	if err != nil {
		t.Fatal(err)
	}
	b, err := FromOwnerGate(repo, "T2", "Does this look right?", "jevons-po")
	if err != nil || a.Identity != b.Identity {
		t.Fatalf("repeat drift: %+v %+v %v", a, b, err)
	}
	c, err := FromOwnerGate(repo, "T2", "Does this look right after reload?", "jevons-po")
	if err != nil || a.Identity.Version == c.Identity.Version || a.Identity.ID != c.Identity.ID {
		t.Fatalf("change not versioned: %+v %+v %v", a, c, err)
	}
}

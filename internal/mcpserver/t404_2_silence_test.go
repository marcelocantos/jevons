// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0
package mcpserver

import (
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/envelope"
	"github.com/marcelocantos/jevons/internal/ownerquestionview"
)

// 🎯T404.2 acceptance 1: an owner-decision block with no question-default
// parses fine (it is not refused — refusing would drop the question, the
// 🎯T378 failure) but the author is told on its own channel, same shape as
// 🎯T793, naming the failing slot; the question is still recorded and paged.
func TestT4042MissingDefaultGetsNoticeButIsStillRecorded(t *testing.T) {
	asker := &fakeSender{alive: true}
	s, dir := t404Server(t, map[string]*fakeSender{"jv-t404-auto": asker}, map[string]string{"jv-t404-auto": ""})
	repo := t.TempDir()
	report := t404Report(repo, []string{"cockpit", "slack"}, "") // no default
	s.storeAgentReport("jv-t404-auto", report)

	rows, err := ownerquestionview.New(dir).List(true)
	if err != nil || len(rows) != 1 || rows[0].State != ownerquestionview.Open {
		t.Fatalf("question not recorded/open: %+v %v", rows, err)
	}
	if !strings.HasPrefix(rows[0].SilenceOutcome(), "STALLS") {
		t.Fatalf("silence outcome = %q, want STALLS mark", rows[0].SilenceOutcome())
	}

	notice := envelope.MissingDefaultNotice(report)
	if notice == "" || !strings.Contains(notice, "question-default") || !strings.Contains(notice, "🎯T404.2") {
		t.Fatalf("missing-default notice = %q", notice)
	}
	// A report that DOES carry a default gets no notice.
	if got := envelope.MissingDefaultNotice(t404Report(repo, nil, "proceed with cockpit")); got != "" {
		t.Fatalf("report with a default should get no notice, got %q", got)
	}
	// A report that is not an owner-decision block gets no notice either.
	plain := envelope.Format(&envelope.Message{Kind: envelope.KindFinishReport, Target: "T1", SHA: "abc1234", SilentLedger: envelope.SilentLedgerEmpty})
	if got := envelope.MissingDefaultNotice(plain); got != "" {
		t.Fatalf("plain finish-report should get no notice, got %q", got)
	}
}

// 🎯T404.2 acceptance 2: after the configured silence window, an unanswered
// question that carries a default is retired as moot: proceeded on default
// and the asker is told to proceed on it; a question with no default stays
// open and keeps its STALLS mark regardless of age.
func TestT4042SilenceSweepResolvesDefaultedQuestionOnly(t *testing.T) {
	asker := &fakeSender{alive: true}
	stalled := &fakeSender{alive: true}
	s, dir := t404Server(t, map[string]*fakeSender{"jv-t404-auto": asker, "jv-t404-stall": stalled},
		map[string]string{"jv-t404-auto": "", "jv-t404-stall": ""})
	repo := t.TempDir()

	withDefault := envelope.Format(&envelope.Message{Kind: envelope.KindFinishReport, Target: "T404", Status: envelope.ProgressBlocked, Blocker: "owner-decision", BlockClass: envelope.BlockOwnerDecision, QuestionRepo: repo, QuestionID: "with-default", QuestionVersion: "1", Question: "Pick a color?", QuestionAsker: "jv-t404-auto", AnswerRoute: "jv-t404-auto", QuestionDefault: "blue", SilentLedger: envelope.SilentLedgerEmpty})
	noDefault := envelope.Format(&envelope.Message{Kind: envelope.KindFinishReport, Target: "T404", Status: envelope.ProgressBlocked, Blocker: "owner-decision", BlockClass: envelope.BlockOwnerDecision, QuestionRepo: repo, QuestionID: "no-default", QuestionVersion: "1", Question: "Pick a shape?", QuestionAsker: "jv-t404-stall", AnswerRoute: "jv-t404-stall", SilentLedger: envelope.SilentLedgerEmpty})
	s.storeAgentReport("jv-t404-auto", withDefault)
	s.storeAgentReport("jv-t404-stall", noDefault)

	// Immediately after recording, the sweep (long window) should not fire.
	s.SetOwnerQuestionSilenceWindow(time.Hour)
	s.sweepOwnerQuestionSilence(time.Now())
	rows, err := ownerquestionview.New(dir).List(false)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.State != ownerquestionview.Open {
			t.Fatalf("resolved too early: %+v", r)
		}
	}

	// Use a vanishingly small window so "now" is already past it relative
	// to the UpdatedAt stamp Record just set, without needing to reach
	// into the store's file format to backdate records by hand.
	s.lastOwnerQuestionSilenceSweep = time.Time{}
	s.SetOwnerQuestionSilenceWindow(time.Millisecond)
	time.Sleep(2 * time.Millisecond)
	s.sweepOwnerQuestionSilence(time.Now())

	rows, err = ownerquestionview.New(dir).List(false)
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows: %+v %v", rows, err)
	}
	var withDefRow, noDefRow ownerquestionview.Question
	for _, r := range rows {
		switch r.Identity.ID {
		case "with-default":
			withDefRow = r
		case "no-default":
			noDefRow = r
		}
	}
	if withDefRow.State != ownerquestionview.Moot || withDefRow.Resolution != "moot: proceeded on default" {
		t.Fatalf("defaulted question not resolved: %+v", withDefRow)
	}
	if noDefRow.State != ownerquestionview.Open {
		t.Fatalf("no-default question should stay open: %+v", noDefRow)
	}
	if !strings.HasPrefix(noDefRow.SilenceOutcome(), "STALLS") {
		t.Fatalf("no-default question lost its STALLS mark: %q", noDefRow.SilenceOutcome())
	}

	sent := asker.sentSnapshot()
	if len(sent) != 1 || !strings.Contains(sent[0], "proceed on your stated default") {
		t.Fatalf("asker not told to proceed on default: %q", sent)
	}
	if len(stalled.sentSnapshot()) != 0 {
		t.Fatalf("stalling asker should not have been routed anything: %q", stalled.sentSnapshot())
	}
}

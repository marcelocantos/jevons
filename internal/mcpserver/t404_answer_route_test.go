// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0
package mcpserver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/envelope"
	"github.com/marcelocantos/jevons/internal/ownerquestions"
	"github.com/marcelocantos/jevons/internal/ownerquestionview"
)

// t404Server is a chain server whose fleet arm resolves to fakes and whose
// owner-question index and outbox live in one temp dir, with a fake blurter
// on PATH so intake can spool without touching the owner's sinks.
func t404Server(t *testing.T, fleet map[string]*fakeSender, parents map[string]string) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "blurter"), []byte("#!/bin/sh\necho /spool/accepted\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	s, _ := chainServer(t, fleet)
	s.registry = newLineageRegistry(t, parents)
	s.SetAgentReportDir(dir)
	s.stateDir = dir
	s.ownerQuestionsDir = dir
	return s, dir
}

func t404Report(repo string, options []string, def string) string {
	return envelope.Format(&envelope.Message{Kind: envelope.KindFinishReport, Target: "T404", Status: envelope.ProgressBlocked, Blocker: "owner-decision-on-answer-surface", BlockClass: envelope.BlockOwnerDecision, QuestionRepo: repo, QuestionID: "surface", QuestionVersion: "1", Question: "Cockpit panel or Slack thread for owner answers?", QuestionAsker: "jv-t404-auto", AnswerRoute: "jv-t404-auto", QuestionOptions: options, QuestionDefault: def, SilentLedger: envelope.SilentLedgerEmpty})
}

func t404Req(op, repo string, extra map[string]any) mcp.CallToolRequest {
	req := mcp.CallToolRequest{}
	args := map[string]any{"op": op, "repo": repo, "target": "T404", "id": "surface", "version": "1"}
	for k, v := range extra {
		args[k] = v
	}
	req.Params.Arguments = args
	return req
}

// 🎯T404 acceptance 4: the owner's answer reaches the asking seat through
// the daemon, not through the overseer composing a relay by hand. The
// question closes as answered and leaves the notification outbox.
func TestT404AnswerRoutesBackToAsker(t *testing.T) {
	asker := &fakeSender{alive: true}
	s, dir := t404Server(t, map[string]*fakeSender{"jv-t404-auto": asker, "jevons-po": {alive: true}}, map[string]string{"jevons-po": "", "jv-t404-auto": "jevons-po"})
	repo := t.TempDir()
	s.storeAgentReport("jv-t404-auto", t404Report(repo, []string{"cockpit", "slack"}, "cockpit unless you say otherwise"))

	// The list shows the question with its silence outcome and options.
	listed, err := s.handleOwnerQuestions(context.Background(), t404Req("list", repo, nil))
	if err != nil || listed.IsError {
		t.Fatalf("list: %+v %v", listed, err)
	}
	text := targetFileToolText(listed)
	if !strings.Contains(text, `"silence_outcome": "proceeds on default: cockpit unless you say otherwise"`) || !strings.Contains(text, `"slack"`) {
		t.Fatalf("list lacks the 🎯T404 contract:\n%s", text)
	}

	answered, err := s.handleOwnerQuestions(context.Background(), t404Req("answer", repo, map[string]any{"answer": "slack", "actor": "jevons"}))
	if err != nil || answered.IsError {
		t.Fatalf("answer: %+v %v", answered, err)
	}
	if out := targetFileToolText(answered); !strings.Contains(out, "routed: answered to jv-t404-auto (sent)") {
		t.Fatalf("answer result does not say where the answer went:\n%s", out)
	}
	sent := asker.sentSnapshot()
	if len(sent) != 1 || !strings.Contains(sent[0], "Owner answer to your") || !strings.Contains(sent[0], "surface@1") || !strings.Contains(sent[0], ": slack") {
		t.Fatalf("asker did not receive the answer: %q", sent)
	}
	rows, err := ownerquestionview.New(dir).List(false)
	if err != nil || len(rows) != 1 || rows[0].State != ownerquestionview.Answered || rows[0].Resolution != "slack" {
		t.Fatalf("durable state after answer: %+v %v", rows, err)
	}
	entries, _, err := ownerquestions.New(dir).Snapshot()
	if err != nil || len(entries) != 0 {
		t.Fatalf("answered question still in the outbox: %+v %v", entries, err)
	}

	// Answering twice is refused by the lifecycle, and nothing is re-sent.
	again, _ := s.handleOwnerQuestions(context.Background(), t404Req("answer", repo, map[string]any{"answer": "cockpit"}))
	if again == nil || !again.IsError || len(asker.sentSnapshot()) != 1 {
		t.Fatalf("second answer: %+v sent=%d", again, len(asker.sentSnapshot()))
	}
}

// A free-form answer to a closed-form question is accepted but labelled, so
// the asker can tell a typo from a new decision.
func TestT404FreeFormAnswerToClosedQuestionIsLabelled(t *testing.T) {
	asker := &fakeSender{alive: true}
	s, _ := t404Server(t, map[string]*fakeSender{"jv-t404-auto": asker}, map[string]string{"jv-t404-auto": ""})
	repo := t.TempDir()
	s.storeAgentReport("jv-t404-auto", t404Report(repo, []string{"cockpit", "slack"}, "cockpit unless you say otherwise"))
	res, err := s.handleOwnerQuestions(context.Background(), t404Req("answer", repo, map[string]any{"answer": "both, cockpit first"}))
	if err != nil || res.IsError {
		t.Fatalf("answer: %+v %v", res, err)
	}
	if sent := asker.sentSnapshot(); len(sent) != 1 || !strings.Contains(sent[0], "both, cockpit first (free-form; options were cockpit | slack)") {
		t.Fatalf("free-form answer not labelled: %q", sent)
	}
}

// 🎯T404 acceptance 6: a question that became moot retires without an
// answer and the asker is told so, with its stated default to fall back on.
func TestT404MootRetirementTellsTheAsker(t *testing.T) {
	asker := &fakeSender{alive: true}
	s, dir := t404Server(t, map[string]*fakeSender{"jv-t404-auto": asker}, map[string]string{"jv-t404-auto": ""})
	repo := t.TempDir()
	s.storeAgentReport("jv-t404-auto", t404Report(repo, nil, "cockpit unless you say otherwise"))
	res, err := s.handleOwnerQuestions(context.Background(), t404Req("resolve", repo, map[string]any{"state": "moot", "note": "surface decided by T1044"}))
	if err != nil || res.IsError {
		t.Fatalf("moot: %+v %v", res, err)
	}
	if sent := asker.sentSnapshot(); len(sent) != 1 || !strings.Contains(sent[0], "retired as moot") || !strings.Contains(sent[0], "surface decided by T1044") || !strings.Contains(sent[0], "proceed on your stated default") {
		t.Fatalf("asker not told: %q", sent)
	}
	rows, _ := ownerquestionview.New(dir).List(false)
	if len(rows) != 1 || rows[0].State != ownerquestionview.Moot {
		t.Fatalf("state after moot: %+v", rows)
	}
}

// An asker that is already gone is not an error: the outcome stays in the
// durable index and the result names the gap instead of claiming delivery.
func TestT404AnswerToReapedAskerIsNotClaimedDelivered(t *testing.T) {
	s, _ := t404Server(t, map[string]*fakeSender{}, map[string]string{})
	repo := t.TempDir()
	s.storeAgentReport("jv-t404-auto", t404Report(repo, nil, ""))
	res, err := s.handleOwnerQuestions(context.Background(), t404Req("answer", repo, map[string]any{"answer": "cockpit"}))
	if err != nil || res.IsError {
		t.Fatalf("answer: %+v %v", res, err)
	}
	out := targetFileToolText(res)
	if !strings.Contains(out, "routed: nowhere") || !strings.Contains(out, "jv-t404-auto") || strings.Contains(out, "(sent)") {
		t.Fatalf("delivery claimed to a seat that is gone:\n%s", out)
	}
}

// The free-text asker and route slots have carried role suffixes and tool
// spellings; candidates are the seat names inside them, in asker-first order.
func TestT404RouteCandidates(t *testing.T) {
	for _, tc := range []struct {
		asker, route string
		want         []string
	}{
		{"jv-t404-auto", "jv-t404-auto", []string{"jv-t404-auto"}},
		{"jevons-po / T1049 scout", "jevons_owner_questions op=answer", []string{"jevons-po"}},
		{"historical T1028.2 scout / repo PO", "jevons-po", []string{"historical", "jevons-po"}},
		{"jv-worker", "jevons-po", []string{"jv-worker", "jevons-po"}},
		{"", "", nil},
	} {
		got := questionRouteCandidates(ownerquestionview.Question{Asker: tc.asker, AnswerRoute: tc.route})
		if strings.Join(got, ",") != strings.Join(tc.want, ",") {
			t.Errorf("asker=%q route=%q: got %v want %v", tc.asker, tc.route, got, tc.want)
		}
	}
}

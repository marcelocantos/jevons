// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
)

// 🎯T439 fixtures. The report is jv-t435-reap-event's actual stored report
// 20260814T223137Z-2de59c77, copied verbatim out of the agent-report store —
// the one the daemon reaped one second later as bare_done_completion. It opens
// by refusing to work without a brief and closes with read-only recon notes
// that happen to contain the word "done".
func loadBriefRequestReport(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "t439_jv_t435_brief_request_report.md"))
	if err != nil {
		t.Fatalf("read brief-request report fixture: %v", err)
	}
	if len(b) < 1000 {
		t.Fatalf("brief-request fixture is too short to be the real one: %d bytes", len(b))
	}
	return string(b)
}

// t439Registry is one plain work leaf — no descendants, no durable role — so
// the only thing that can change the reap decision is the report itself.
func t439Registry(t *testing.T, name string) *claudia.Registry {
	t.Helper()
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: dir, SessionID: "s-w",
		Purpose: claudia.PurposeWork, Parent: "jevons-po",
		Materialized: true, Provider: "grok",
	}); err != nil {
		t.Fatal(err)
	}
	return reg
}

// TestT439BriefRequestIsTheIncidentShape pins the two properties that made the
// real report reap: it carries a completion word ("recon done meanwhile"), and
// none of the 🎯T395 classes fire on it — no decision fork, no explicit
// statement of non-completion, no closing question. If the fixture ever loses
// either property it stops being a regression test for anything.
func TestT439BriefRequestIsTheIncidentShape(t *testing.T) {
	report := loadBriefRequestReport(t)
	if !hasCompletionClaim(strings.ToLower(report)) {
		t.Fatal("fixture no longer carries a completion word — it cannot reproduce the incident")
	}
	if !strings.Contains(report, "Please resend it before I write anything") {
		t.Fatal("fixture no longer carries the brief request")
	}
	for _, class := range []struct {
		name    string
		markers []string
	}{
		{"decision request", decisionRequestMarkers},
		{"explicit incomplete", explicitIncompleteMarkers},
	} {
		if m, i := firstMarker(asciiLower(report), class.markers); i >= 0 {
			t.Fatalf("fixture matches a 🎯T395 %s marker %q — the T439 class is not what saves it", class.name, m)
		}
	}
	if closingQuestionIndex(report) >= 0 {
		t.Fatal("fixture closes on a question — the 🎯T395 closing-question class would already have saved it")
	}
}

// TestT439BriefRequestDoesNotReap is the acceptance oracle for the red
// direction: the real misclassified report, run through the whole reap
// decision over a registry with one plain work agent.
func TestT439BriefRequestDoesNotReap(t *testing.T) {
	report := loadBriefRequestReport(t)

	if got := ClassifyReportAsk(report); got != AskRequest {
		t.Fatalf("ClassifyReportAsk = %s, want %s", got, AskRequest)
	}
	if LooksLikeFinishedWorkReport(report) {
		t.Fatal("🎯T439: a report that asks for its brief is not a finished-work report")
	}

	reg := t439Registry(t, "jv-t435-reap-event")
	ok, reason := ShouldAutoReapDoneWorkAgent(reg, "jv-t435-reap-event", report, nil)
	if ok {
		t.Fatalf("🎯T439: brief request reaped as %s", reason)
	}
	if reason != "awaits_overseer_request" {
		t.Fatalf("non-reap reason = %q, want awaits_overseer_request", reason)
	}
	if reg.Def("jv-t435-reap-event") == nil {
		t.Fatal("agent left the registry")
	}
}

// TestT439RequestsAndFinishes runs both directions in one table, because a
// classifier that stops reaping everything would satisfy the red fixture alone.
// The finish rows are 🎯T165 / 🎯T195 behaviour and they must survive this
// narrowing untouched, including the imperfect bare done and the polite
// hand-back that offers more work without asking for any input.
func TestT439RequestsAndFinishes(t *testing.T) {
	cases := []struct {
		name     string
		report   string
		wantAsk  ReportAskClass
		wantReap bool
	}{
		{
			name:     "brief never arrived",
			report:   "I did not receive your detailed brief — only the target id. Please resend it. Recon done meanwhile.",
			wantAsk:  AskRequest,
			wantReap: false,
		},
		{
			name:     "asks for a resend",
			report:   "Analysis complete. The brief was truncated; re-send the acceptance clauses and I'll continue.",
			wantAsk:  AskRequest,
			wantReap: false,
		},
		{
			name:     "blocked on an unblock",
			report:   "Done with everything I can reach. Please unblock the credentials and I'll finish the rest.",
			wantAsk:  AskRequest,
			wantReap: false,
		},
		{
			name:     "waiting on an answer",
			report:   "Groundwork complete. Still waiting for the schema you were going to send.",
			wantAsk:  AskRequest,
			wantReap: false,
		},
		{
			name:     "imperfect bare done still reaps (🎯T165 / 🎯T195)",
			report:   "Done.",
			wantAsk:  AskNone,
			wantReap: true,
		},
		{
			name:     "finish with SHA and green oracle reaps",
			report:   "🎯T439 done. Commit 4f2b8c1; `go test ./internal/mcpserver/ -run T439` PASS.",
			wantAsk:  AskNone,
			wantReap: true,
		},
		{
			name:     "polite offer of more work is still a finish",
			report:   "All finished, ready for review. Let me know if you want the follow-up target filed.",
			wantAsk:  AskNone,
			wantReap: true,
		},
		{
			name:     "asking the parent to review is not asking for an input",
			report:   "Complete — commit 9ab3f21, make test green. Needs your review before the retire.",
			wantAsk:  AskNone,
			wantReap: true,
		},
		{
			name:     "sending something is not requesting something",
			report:   "Done. I sent the summary to jevons-po and filed the residual as 🎯T440.",
			wantAsk:  AskNone,
			wantReap: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyReportAsk(tc.report); got != tc.wantAsk {
				t.Fatalf("ClassifyReportAsk = %s, want %s", got, tc.wantAsk)
			}
			reg := t439Registry(t, "jv-worker")
			ok, reason := ShouldAutoReapDoneWorkAgent(reg, "jv-worker", tc.report, nil)
			if ok != tc.wantReap {
				t.Fatalf("ShouldAutoReapDoneWorkAgent = %v (%s), want %v", ok, reason, tc.wantReap)
			}
		})
	}
}

// TestT439RequestVetoIsOneWay is the 🎯T395 safety property extended to the new
// class: appending a request to any report — one that would reap and one that
// would not — must leave it not reaping. A future marker can make the
// classifier wrong here, but not in the direction that destroys an agent that
// is waiting on its parent.
func TestT439RequestVetoIsOneWay(t *testing.T) {
	reports := []string{
		"Done.",
		"Complete — commit 9ab3f21, make test green.",
		"All finished, ready for review.",
		"still reading the tree",
		"",
	}
	requests := []string{
		"I did not receive your detailed brief.",
		"Please resend the acceptance clauses.",
		"Please provide the credentials.",
		"Still waiting for your answer on the boundary.",
		"I need you to send the fixture path.",
	}
	for _, r := range reports {
		for _, q := range requests {
			if ClassifyReportAsk(q) != AskRequest {
				t.Fatalf("request fixture %q is not classified as a request — the property is vacuous", q)
			}
			withRequest := strings.TrimSpace(r + "\n\n" + q)
			reg := t439Registry(t, "jv-worker")
			ok, _ := ShouldAutoReapDoneWorkAgent(reg, "jv-worker", withRequest, nil)
			if ok {
				t.Errorf("🎯T439: %q + request %q reaps", r, q)
			}
		}
	}
}

// TestT439SpanLocatesTheDecision covers the span itself: it must be findable
// again in the report at the offset given, and must contain the phrase the
// classifier fired on. A span that does not point at the text it claims to
// quote is worse than no span, because it reads as evidence.
func TestT439SpanLocatesTheDecision(t *testing.T) {
	report := loadBriefRequestReport(t)

	ask := ClassifyReportAskDetail(report)
	if ask.Class != AskRequest {
		t.Fatalf("class = %s, want %s", ask.Class, AskRequest)
	}
	if ask.Marker != "did not receive" {
		t.Fatalf("marker = %q, want the first request phrase in the report", ask.Marker)
	}
	if !strings.Contains(ask.Span, "did not receive your detailed brief") {
		t.Fatalf("span %q does not quote the phrase that fired", ask.Span)
	}
	if ask.Offset < 0 || ask.Offset+len(ask.Span) > len(report) ||
		report[ask.Offset:ask.Offset+len(ask.Span)] != ask.Span {
		t.Fatalf("span is not at offset %d in the report", ask.Offset)
	}
	// 🎯T470: decision spans are the matched sentence/clause, not a 200-byte
	// window — a long sentence is still the right diagnostic unit.
	if !strings.Contains(ask.Span, ask.Marker) {
		t.Fatalf("span %q lost its marker %q", ask.Span, ask.Marker)
	}

	// The reap direction quotes the completion word instead.
	marker, span, offset, ok := FindCompletionClaim(report)
	if !ok {
		t.Fatal("no completion claim found in a report that reaped as one")
	}
	if marker != "done" || !strings.Contains(span, "done") {
		t.Fatalf("completion marker=%q span=%q", marker, span)
	}
	if report[offset:offset+len(span)] != span {
		t.Fatalf("completion span is not at offset %d", offset)
	}
}

// TestT439ReapLogRecordsDecisionAndSpan is the lifecycle-log half of the
// acceptance, on the path the daemon actually takes. jv-t435's disappearance
// was diagnosable only from the report store, because the log line carried a
// reason and nothing else; both the skip and the reap must now name the words
// they fired on.
func TestT439ReapLogRecordsDecisionAndSpan(t *testing.T) {
	cap := &slogCapture{}
	prev := slog.Default()
	slog.SetDefault(slog.New(cap))
	t.Cleanup(func() { slog.SetDefault(prev) })

	report := loadBriefRequestReport(t)
	reg := t439Registry(t, "jv-t435-reap-event")
	s := &Server{registry: reg}
	s.maybeReapDoneWorkAgent("jv-t435-reap-event", report)

	got := findLifecycle(cap.records, compAgentLifecycle, "reap_done")
	if got == nil {
		t.Fatalf("no agent_lifecycle reap_done record; records=%d", len(cap.records))
	}
	if got["outcome"] != "skipped" {
		t.Fatalf("outcome = %v, want skipped", got["outcome"])
	}
	if got["reason"] != "awaits_overseer_request" || got["ask_class"] != "request" {
		t.Fatalf("reason=%v ask_class=%v", got["reason"], got["ask_class"])
	}
	if got["ask_marker"] != "did not receive" {
		t.Fatalf("ask_marker = %v", got["ask_marker"])
	}
	span, _ := got["report_span"].(string)
	if !strings.Contains(span, "did not receive your detailed brief") {
		t.Fatalf("report_span = %q", span)
	}
	if reg.Def("jv-t435-reap-event") == nil {
		t.Fatal("agent was reaped")
	}

	// A genuine finish reaps, and its log line quotes the completion word.
	cap.records = nil
	reg2 := t439Registry(t, "jv-worker")
	s2 := &Server{registry: reg2}
	s2.maybeReapDoneWorkAgent("jv-worker", "Done.")

	got = findLifecycle(cap.records, compAgentLifecycle, "reap_done")
	if got == nil {
		t.Fatal("no reap_done record for a genuine finish")
	}
	if got["outcome"] != "ok" || got["reason"] != "bare_done_completion" {
		t.Fatalf("outcome=%v reason=%v", got["outcome"], got["reason"])
	}
	if got["claim_marker"] != "done" || got["report_span"] != "Done." {
		t.Fatalf("claim_marker=%v report_span=%v", got["claim_marker"], got["report_span"])
	}
	if reg2.Def("jv-worker") != nil {
		t.Fatal("🎯T165 / 🎯T195: a genuine bare done must still leave the fleet")
	}
}

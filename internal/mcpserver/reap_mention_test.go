// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/gate"
)

// plantT446GateRecords makes the three GREEN gate ids the fixture cites
// resolvable from a hermetic store. On the machine that ran them the
// records sit in ~/.jevons/gates; anywhere else (a CI runner) the T470
// false-green check would find no record behind the id and veto the reap,
// which is that check working — but this test is about the ask classifier,
// not about where the evidence lives.
func plantT446GateRecords(t *testing.T) {
	t.Helper()
	root := t.TempDir()
	t.Setenv(gate.StoreDirEnv, root)
	st, err := gate.OpenStore(root)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 8, 14, 22, 50, 0, 0, time.UTC)
	for id, name := range map[string]string{
		"c1c07bdc": "t439-mcpserver",
		"0f1d4204": "t439-clean-checkout",
		"af143180": "t439-head-build",
	} {
		if err := st.Save(&gate.Record{
			ID: id, Name: name, Command: []string{"go", "test", "./..."},
			Started: at, Ended: at.Add(time.Minute),
			StatusKnown: true, ExitStatus: 0, Verdict: gate.VerdictGreen,
		}); err != nil {
			t.Fatal(err)
		}
	}
}

// 🎯T446 fixtures: jv-t439-reap-request's two actual stored terminal reports,
// copied verbatim out of the agent-report store —
// 20260814T225011Z-bd8dc08c and 20260814T225109Z-41c9d166 — the ones the daemon
// skipped the reap on at 08:50:14 and 08:51:11 with ask_marker=resend and
// ask_marker=re-send. Both declare the mission over, with a commit SHA and three
// GREEN gate ids; the phrases that saved them are the report describing the
// classifier it had just written, and the negation "I did not re-send anything".
func loadMentionReport(t *testing.T, file string, minBytes int) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", file))
	if err != nil {
		t.Fatalf("read mention report fixture %s: %v", file, err)
	}
	if len(b) < minBytes {
		t.Fatalf("fixture %s is too short to be the real report: %d bytes", file, len(b))
	}
	return string(b)
}

func t446MetaMentionReport(t *testing.T) string {
	t.Helper()
	return loadMentionReport(t, "t446_meta_mention_report.md", 2000)
}

func t446NegatedMentionReport(t *testing.T) string {
	t.Helper()
	return loadMentionReport(t, "t446_negated_mention_report.md", 1600)
}

// TestT446MentionReportsAreTheIncidentShape pins that the fixtures still
// reproduce the incident, so the green below means the guards are doing the
// work. Two properties: the report claims completion (otherwise there is no reap
// to skip), and the unguarded marker scan — the classifier exactly as 🎯T439 left
// it — still finds a request phrase in it. If either lapses, the regression test
// is testing nothing.
func TestT446MentionReportsAreTheIncidentShape(t *testing.T) {
	for _, tc := range []struct {
		name       string
		report     string
		wantMarker string
	}{
		{"meta mention", t446MetaMentionReport(t), "resend"},
		{"negated mention", t446NegatedMentionReport(t), "re-send"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lower := strings.ToLower(tc.report)
			if !hasAnyCompletionMarker(lower) {
				t.Fatal("fixture carries no completion word — there was no reap for the mention to block")
			}
			if hasCompletionClaim(lower) {
				t.Fatal("a completion sentence is a completion claim")
			}
			if !strings.Contains(asciiLower(tc.report), tc.wantMarker) {
				t.Fatalf("fixture lost %q", tc.wantMarker)
			}
		})
	}
}

// TestT446MentionsNoLongerReadardAsRequests is the acceptance oracle for the
// class this target narrows: neither real report asks for anything, so neither
// may be held as a request.
func TestT446MentionsNoLongerReadAsRequests(t *testing.T) {
	for _, tc := range []struct {
		name   string
		report string
	}{
		{"meta mention", t446MetaMentionReport(t)},
		{"negated mention", t446NegatedMentionReport(t)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := ClassifyReportAskDetail(tc.report)
			if d.Class == AskRequest {
				t.Fatalf("🎯T446: held as a request on %q — span %q", d.Marker, d.Span)
			}
		})
	}
}

// TestT446MetaMentionReportReaps runs the first report through the reap the
// daemon actually takes: a finished worker whose report described the ask
// vocabulary leaves the fleet.
func TestT446MetaMentionReportReaps(t *testing.T) {
	plantT446GateRecords(t)
	report := t446MetaMentionReport(t)

	if got := ClassifyReportAsk(report); got != AskNone {
		d := ClassifyReportAskDetail(report)
		t.Fatalf("ClassifyReportAsk = %s (marker %q, span %q), want %s", got, d.Marker, d.Span, AskNone)
	}
	if LooksLikeFinishedWorkReport(report) {
		t.Fatal("unenveloped prose is not a finished-work report")
	}
	done := declaredFinishReport("T446", report)
	if !LooksLikeFinishedWorkReport(done) {
		t.Fatal("🎯T446: a declared finish that names the ask vocabulary is a finished-work report")
	}

	reg := t439Registry(t, "jv-t439-reap-request")
	s := &Server{registry: reg}
	s.maybeReapDoneWorkAgent("jv-t439-reap-request", done)
	if reg.Def("jv-t439-reap-request") != nil {
		t.Fatal("🎯T165 / 🎯T195: the finished worker stayed in the fleet")
	}
}

// TestT446QuotedCommitSubjectIsTheDeclaredResidual pins the negated-mention
// fixture. A quoted commit subject is not an ask class (🎯T766.4). The
// words stay in the fixture. Unenveloped prose does not reap.
func TestT446QuotedCommitSubjectIsTheDeclaredResidual(t *testing.T) {
	report := t446NegatedMentionReport(t)

	d := ClassifyReportAskDetail(report)
	if d.Class != AskNone {
		t.Fatalf("a quoted commit subject is an ask: class = %s", d.Class)
	}
	if !strings.Contains(strings.ToLower(report), "not finished") || !strings.Contains(report, "a worker asking for its brief has not finished") {
		t.Fatal("fixture lost the quoted commit subject")
	}
	reg := t439Registry(t, "jv-t439-reap-request")
	if ok, reason := ShouldAutoReapDoneWorkAgent(reg, "jv-t439-reap-request", report, nil); ok {
		t.Fatalf("unenveloped prose reaps (%s)", reason)
	}
}

// TestT446MentionsAndRequests is the both-directions table. Every mention row is
// paired with the request it must not swallow: the guards are a narrowing of the
// ask, and a narrowing that also rejects "please resend the brief" has simply
// reverted 🎯T439.
func TestT446MentionsAndRequests(t *testing.T) {
	cases := []struct {
		name     string
		report   string
		wantAsk  ReportAskClass
		wantReap bool
	}{
		{
			name:     "negated own action is not a request",
			report:   "🎯T446 done, commit 4f2b8c1. The report was queued, and I did not re-send anything.",
			wantAsk:  AskNone,
			wantReap: true,
		},
		{
			name:     "negation belongs to its own clause",
			report:   "I did not get anything, please resend the brief.",
			wantAsk:  AskNone,
			wantReap: false,
		},
		{
			name:     "gerund is not the verb",
			report:   "Done — commit 4f2b8c1. Re-sending would stack a second copy, so I left it queued.",
			wantAsk:  AskNone,
			wantReap: true,
		},
		{
			name:     "imperative is the verb",
			report:   "Analysis complete. Re-send the acceptance clauses and I'll continue.",
			wantAsk:  AskNone,
			wantReap: false,
		},
		{
			name:     "naming the vocabulary is not using it",
			report:   "Done. AskRequest fires on a report that names an input it is waiting on (brief resend, an answer, an unblock).",
			wantAsk:  AskNone,
			wantReap: true,
		},
		{
			name:     "a bare verb after a request cue is a request",
			report:   "Finished the recon. Could you resend the acceptance clauses?",
			wantAsk:  AskNone,
			wantReap: false,
		},
		{
			name:     "past tense of the parent's send is not a request",
			report:   "Done. The brief you sent through the composer covered every clause.",
			wantAsk:  AskNone,
			wantReap: true,
		},
		{
			name:     "🎯T439 shape survives untouched",
			report:   "I did not receive your detailed brief — only the target id. Please resend it. Recon done meanwhile.",
			wantAsk:  AskNone,
			wantReap: false,
		},
		{
			name:     "imperfect bare done still reaps (🎯T165 / 🎯T195)",
			report:   "Done.",
			wantAsk:  AskNone,
			wantReap: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyReportAsk(tc.report); got != tc.wantAsk {
				d := ClassifyReportAskDetail(tc.report)
				t.Fatalf("ClassifyReportAsk = %s (marker %q), want %s", got, d.Marker, tc.wantAsk)
			}
			reg := t439Registry(t, "jv-worker")
			reapReport := tc.report
			if tc.wantReap {
				reapReport = declaredFinishReport("T446", tc.report)
			}
			ok, reason := ShouldAutoReapDoneWorkAgent(reg, "jv-worker", reapReport, nil)
			if ok != tc.wantReap {
				t.Fatalf("ShouldAutoReapDoneWorkAgent = %v (%s), want %v", ok, reason, tc.wantReap)
			}
		})
	}
}

// TestT446RejectedMentionDoesNotVetoALaterRequest is the property that keeps the
// guards from being a new way to lose an agent: a mention earlier in the report
// must not consume the scan and hide the real ask behind it. The bias of 🎯T395
// and 🎯T439 still holds — ambiguity resolves toward keeping the agent.
func TestT446RejectedMentionDoesNotVetoALaterRequest(t *testing.T) {
	mentions := []string{
		"I did not re-send anything.",
		"Re-sending would stack a second copy.",
		"AskRequest names an input it is waiting on (brief resend, an answer).",
	}
	requests := []string{
		"Please resend the acceptance clauses.",
		"I did not receive your detailed brief.",
		"Please provide the credentials.",
		"I need you to send the fixture path.",
	}
	for _, m := range mentions {
		if ClassifyReportAsk(m) != AskNone {
			t.Fatalf("mention fixture %q is still classified as an ask — the property is vacuous", m)
		}
		for _, q := range requests {
			report := "Done. " + m + "\n\n" + q
			if got := ClassifyReportAsk(report); got != AskNone {
				t.Errorf("a sentence became an ask (%s): %q + %q", got, m, q)
			}
			reg := t439Registry(t, "jv-worker")
			if ok, _ := ShouldAutoReapDoneWorkAgent(reg, "jv-worker", report, nil); ok {
				t.Errorf("🎯T439: %q + %q reaps", m, q)
			}
		}
	}
}

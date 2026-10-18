// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/marcelocantos/jevons/internal/envelope"
)

// t7664ReapEnglishAllowlist is the English classifiers the reap/done control
// path may call. Pinned at zero (🎯T766.4). A hit here is a prose fallback.
var t7664ReapEnglishAllowlist = []string{}

func TestT7664DeclaredKindIsTheDoneAnswer(t *testing.T) {
	reg := regWithTree(t)
	isWork := func(string) bool { return false }

	prose := "Done. SHA abcdef0123456. go test ./internal/mcpserver -run T165 PASS"
	ok, reason := ShouldAutoReapDoneWorkAgent(reg, "worker", prose, isWork)
	if ok || reason != "unknown_report" {
		t.Fatalf("prose reap=%v reason=%s, want unknown", ok, reason)
	}
	if LooksLikeFinishedWorkReport(prose) {
		t.Fatal("supervision treated unenveloped prose as done")
	}
	if ClassifyWorkerIdleDisposition(prose) != IdleActionKeep {
		t.Fatal("idle disposition treated unenveloped prose as a reap or park")
	}

	finish := declaredFinishReport("T766.4", "Landed.")
	if !LooksLikeFinishedWorkReport(finish) || !envelope.IsDoneReport(finish) {
		t.Fatal("declared finish-report must be the one done answer")
	}
	ok, reason = ShouldAutoReapDoneWorkAgent(reg, "worker", finish, isWork)
	if !ok || reason != "finished_work" {
		t.Fatalf("declared finish reap=%v reason=%s", ok, reason)
	}
	if ClassifyWorkerIdleDisposition(finish) != IdleActionReap {
		t.Fatal("idle disposition did not reap a declared finish-report")
	}

	scout := envelope.Format(&envelope.Message{
		Kind:         envelope.KindScoutReport,
		Target:       "T766.4",
		SilentLedger: envelope.SilentLedgerEmpty,
		Payload:      "Scout done.",
	})
	ok, reason = ShouldAutoReapDoneWorkAgent(reg, "worker", scout, isWork)
	if ok || reason != "not_finished_work_report" {
		t.Fatalf("scout reap=%v reason=%s", ok, reason)
	}

	blocked := envelope.Format(&envelope.Message{
		Kind:         envelope.KindFinishReport,
		Target:       "T766.4",
		Status:       envelope.ProgressBlocked,
		Blocker:      "owner-go-ahead",
		SilentLedger: envelope.SilentLedgerEmpty,
	})
	ok, reason = ShouldAutoReapDoneWorkAgent(reg, "worker", blocked, isWork)
	if ok || reason != IdleSkipBlockedOnOwner {
		t.Fatalf("blocked reap=%v reason=%s", ok, reason)
	}
	if ClassifyWorkerIdleDisposition(blocked) != IdleActionPark {
		t.Fatal("blocked finish-report must park")
	}
}

func TestT7664ReapDoneEnglishAllowlistZero(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	hits := append(englishSelectorsIn(t, filepath.Join(dir, "reap_done.go"),
		"LooksLikeFinishedWorkReport", "reportDoneForReap", "ShouldAutoReapDoneWorkAgent", "finishedWorkReapReason"),
		englishSelectorsIn(t, filepath.Join(dir, "t985_reap_vs_park.go"), "ClassifyWorkerIdleDisposition")...)
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "stored_terminal_report.go"), "storedTerminalFromReport")...)
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "t938_seat_blocker.go"), "classifyStoredReport")...)
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "reap_scope.go"), "OutstandingScopeReasons", "midWorkReportScope")...)
	if len(hits) != len(t7664ReapEnglishAllowlist) {
		t.Fatalf("control-path English classifiers = %v, allowlist %v", hits, t7664ReapEnglishAllowlist)
	}
}

// t7664ErrorEnglishAllowlist is the English classifiers the send/error
// control path may call. Pinned at zero (🎯T766.4).
var t7664ErrorEnglishAllowlist = []string{}

func TestT7664ErrorCauseEnglishAllowlistZero(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	hits := englishSelectorsIn(t, filepath.Join(dir, "agent_send.go"), "queueSendDefinitelyNotSent")
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "../agenterr/busy.go"), "IsPromptBusy")...)
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "../agenterr/cause.go"), "ProviderCause")...)
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "../escalate/escalate.go"), "SteerUnsupported", "NotStarted")...)
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "../agenterr/failure.go"), "Classify", "ClassifyText", "ClassFromCause")...)
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "../agenterr/reply.go"), "ClassifyReply")...)
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "../agenterr/retry_exhausted.go"), "IsRetryExhausted")...)
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "../agenterr/context_overflow.go"), "IsContextOverflow")...)
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "../agenterr/source.go"), "ClassifyFrom")...)
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "../agenterr/hardblock.go"), "HardBlock")...)
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "../agenterr/grant.go"), "IsGrantRefusal", "NotifyClass", "ErrOverseerNotRunning", "NoteBrokerGrant", "GrantRefusal", "protocolCode", "exportedProtocolCode")...)
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "provider_failure.go"), "jworkFailure")...)
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "../server/chat.go"), "notifyErrClass")...)
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "../upgrade/readopt.go"), "IsNotOwner")...)
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "idle_nudge.go"), "DeclaresBlockingGateWait")...)
	if len(hits) != len(t7664ErrorEnglishAllowlist) {
		t.Fatalf("error-control English classifiers = %v, allowlist %v", hits, t7664ErrorEnglishAllowlist)
	}
}

func englishSelectorsIn(t *testing.T, path string, names ...string) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{}
	for _, n := range names {
		want[n] = true
	}
	banned := map[string]bool{
		"strings.Contains": true, "strings.HasPrefix": true, "strings.HasSuffix": true,
		"strings.EqualFold": true,
	}
	bannedCall := map[string]bool{
		"ClassifyReportAsk": true, "ClassifyReportAskDetail": true,
		"hasFinishShape": true, "hasCompletionClaim": true, "hasForwardLookingPlan": true,
		"LooksLikeOwnWorkCompleteReport": true, "containsAny": true,
		"FindCompletionClaim": true, "ReportAwaitsOverseer": true,
		"FindActiveExternalWait": true, "declaresBlockingGateWaitLocalized": true,
		"DeclaresBlockingGateWait": true, "declaresActiveExternalWait": true,
		"declaresUnresolvedExternalWait": true,
	}
	var hits []string
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil || !want[fn.Name.Name] {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			switch fun := call.Fun.(type) {
			case *ast.SelectorExpr:
				pkg, ok := fun.X.(*ast.Ident)
				if !ok {
					return true
				}
				name := pkg.Name + "." + fun.Sel.Name
				if banned[name] {
					hits = append(hits, fn.Name.Name+": "+name)
				}
			case *ast.Ident:
				if bannedCall[fun.Name] {
					hits = append(hits, fn.Name.Name+": "+fun.Name)
				}
			}
			return true
		})
	}
	return hits
}

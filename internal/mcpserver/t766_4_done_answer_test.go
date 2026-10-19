// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/envelope"
	"github.com/marcelocantos/jevons/internal/gate"
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
		"LooksLikeFinishedWorkReport", "reportDoneForReap", "ShouldAutoReapDoneWorkAgent", "finishedWorkReapReason",
		"maybeReapDoneWorkAgent", "reapDecisionFields"),
		englishSelectorsIn(t, filepath.Join(dir, "t985_reap_vs_park.go"), "ClassifyWorkerIdleDisposition", "LooksLikeOwnWorkCompleteReport")...)
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "stored_terminal_report.go"), "storedTerminalFromReport")...)
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "t938_seat_blocker.go"), "classifyStoredReport")...)
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "reap_scope.go"), "OutstandingScopeReasons", "midWorkReportScope")...)
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "finish_shape.go"), "bareAckTurnReport", "hasFinishShape")...)
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "oracle_evidence.go"), "ClassifyCompletionReport", "LooksLikeBareDone", "HasOracleOrRisk", "HasOracleEvidence", "HasAcceptedRisk", "UncitedClaimAgainstDirtyWorktree", "hasOracleEvidence", "hasAcceptedRisk", "hasCompletionClaim", "FindCompletionClaim", "reportCitesOracleID", "HasActivationEvidence", "LooksLikeHermeticOnlyDaemonClaim", "LiveRenderOracleDidNotRun", "LooksLikeSilentBrowserOracleFallback")...)
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "open_intent.go"), "hasOpenIntentProductEvidence", "openIntentEvidenceCandidate", "openIntentAnswerShaped", "openIntentSubstantiveReply", "ownerIntentClosedByLaterSubstantiveReply", "OwnerQuestionAnsweredWithExplanation", "openIntentFleetOpsEvidence", "openIntentTopicalLink", "isOpenIntentAckOnly", "parseFleetOpExcerpt", "fleetOpsToolIs", "isOpenIntentFleetOpsTool", "synthesizeOpenIntentFleetOpsTool", "OwnerFleetDirectiveCompleted", "keepOpenIntentAssistant", "ownerFleetDirectiveTargets", "isOpenIntentRestartNudge", "isOpenIntentSendProbe", "isOpenIntentHarness", "isOpenIntentProtocolJSON")...)
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "false_green.go"), "FalseGreenFlagsForWorker")...)
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "t750_quoted_claim.go"), "wouldHaveFinishedBeforeT750", "CitedCompletionClaim")...)
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "local_done.go"), "allCompletionClaimsLocal")...)
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "report_ask.go"), "ClassifyReportAskDetail", "hasForwardLookingPlan")...)
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "../userturn/userturn.go"), "LegacyInjectionText", "unwrapUserQuery")...)
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "../rsi/deeper.go"), "isHarnessInject", "frictionSignal")...)
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "../rsi/retro.go"), "isWeakRetroPhrase")...)
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "../envelope/parse.go"), "stripStandingBrief", "stripOnePrefix", "stripDeclaredPrefix", "splitFence", "parseSlotLine", "Format")...)
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "../envelope/fences.go"), "FenceStarts")...)
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "t731_stale_report.go"), "parseAgentRespondedLine", "hasReapedSeatBanner")...)
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "../briefaddr/briefaddr.go"), "IdentityHeaderName", "Check")...)
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "identity_header.go"), "FormatIdentityHeader", "withIdentity")...)
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "t597_briefless.go"), "IsFullRebrief")...)
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "../sendq/digest.go"), "classify", "reportID")...)
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "deliver_by_name.go"), "stripLeadingIdentityEnvelope")...)
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "../server/notify_queue.go"), "notifyCoalesceKey", "notifyAgentRespondedName", "openingSeatFact", "isOwnerNotifyText")...)
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "fleet_brief.go"), "EnsureFleetBriefWithRole", "textHasHarnessFact")...)
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "idle_nudge.go"), "SessionLooksBriefed", "formatIdleNudgeWire", "IsIdleNudgeText")...)
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "idle_events.go"), "FormatWorkerIdleAmbiguousText")...)
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
	hits := englishSelectorsIn(t, filepath.Join(dir, "agent_send.go"), "queueSendDefinitelyNotSent", "isPromptInFlight")
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "../agenterr/busy.go"), "IsPromptBusy")...)
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "../agenterr/cause.go"), "ProviderCause")...)
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "../escalate/escalate.go"), "SteerUnsupported", "NotStarted")...)
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "../agenterr/failure.go"), "Classify", "ClassifyText", "ClassFromCause")...)
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "../agenterr/reply.go"), "ClassifyReply")...)
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "../agenterr/retry_exhausted.go"), "IsRetryExhausted")...)
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "../agenterr/context_overflow.go"), "IsContextOverflow")...)
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "../agenterr/source.go"), "ClassifyFrom")...)
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "../agenterr/hardblock.go"), "HardBlock")...)
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "../agenterr/grant.go"), "IsGrantRefusal", "NotifyClass", "ErrOverseerNotRunning", "NoteBrokerGrant", "GrantRefusal", "protocolCode", "exportedProtocolCode", "IsFrameTooLarge")...)
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "provider_failure.go"), "jworkFailure")...)
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "../server/chat.go"), "notifyErrClass")...)
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "../upgrade/readopt.go"), "IsNotOwner")...)
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "idle_nudge.go"), "DeclaresBlockingGateWait")...)
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "send_claim.go"), "ClassifySendError")...)
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "t661_oversized.go"), "IsBrokerLineLimitError")...)
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "../server/chat_wire.go"), "assistantProviderCause", "providerCodeCause")...)
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "../fleet/plan_auth_revive.go"), "PlanAuthFailure", "recordedPlanAuthFailure", "planAuthCandidates", "ownerReauthCandidates", "noteAdoptFailure")...)
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "../fleet/provider_caps.go"), "noteRehydrate", "logPlanAuthLoss", "RehydrateBroken")...)
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "../fleet/exit_cause.go"), "BrokerPlanned", "BrokerCaused")...)
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "../server/agent_auth_recover.go"), "destinationAuthRecoverable", "reauthablePlanFailure")...)
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "plan_policy.go"), "planSwitch", "turnInFlight")...)
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "../relayroute/classify.go"), "Classify", "Reason", "envelopeRoute")...)
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "t869_plan_only.go"), "shouldForwardAgentReply", "barePlanSentence")...)
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "report_ask.go"), "ReportAwaitsOverseer")...)
	hits = append(hits, englishSelectorsIn(t, filepath.Join(dir, "idle_residue_evidence.go"), "fillIdleResidueEvidence")...)
	if len(hits) != len(t7664ErrorEnglishAllowlist) {
		t.Fatalf("error-control English classifiers = %v, allowlist %v", hits, t7664ErrorEnglishAllowlist)
	}
}

// TestT7664BareAckSuppressesVocabularyNotAFinishSentence drives the
// production notify seam. A declared finish-report is delivered. The exact
// ack vocabulary is not. A completion sentence is not what decides either.
func TestT7664BareAckSuppressesVocabularyNotAFinishSentence(t *testing.T) {
	prose := "Done. SHA abcdef0123456. go test ./internal/mcpserver -run T165 PASS"
	if envelope.IsDoneReport(prose) {
		t.Fatal("unenveloped prose is not a declared done report")
	}
	if bareAckTurnReport(prose) {
		t.Fatal("prose completion was suppressed as a bare ack")
	}

	finish := declaredFinishReport("T766.4", "Landed.")
	if !envelope.IsDoneReport(finish) {
		t.Fatal("fixture is not a declared done report")
	}
	if bareAckTurnReport(finish) {
		t.Fatal("declared finish-report was suppressed as a bare ack")
	}

	deliver := func(text string) []string {
		s := &Server{}
		var got []string
		s.SetNotify(func(msg string) { got = append(got, msg) })
		s.agentEventSink("jv-t7664-ack")(claudia.Event{
			Type: "assistant", Text: text, StopReason: "end_turn",
		})
		return got
	}
	if got := deliver(finish); len(got) != 1 {
		t.Fatalf("declared finish deliveries=%d", len(got))
	}
	if got := deliver(liveMisrouteAck); len(got) != 0 {
		t.Fatalf("bare ack reached the overseer: %v", got)
	}
	if got := deliver(prose); len(got) != 1 {
		t.Fatalf("prose completion deliveries=%d, want the notify path to forward it", len(got))
	}
	if !bareAckTurnReport("Noted.") {
		t.Fatal("exact ack vocabulary was not suppressed")
	}
}

// TestT7664ProseOracleDoesNotReapThroughTheSink drives the turn sink.
// An oracle sentence is not a finish shape and not an evidence class, so
// the seat stays. A declared finish-report of that same sentence is oracle
// evidence and the sink removes the seat.
func TestT7664ProseOracleDoesNotReapThroughTheSink(t *testing.T) {
	prose := "Done. SHA abcdef0123456. go test ./internal/mcpserver -run T31 PASS"
	if hasFinishShape(strings.ToLower(prose)) {
		t.Fatal("a completion sentence is a finish shape")
	}
	if hasOracleEvidence(strings.ToLower(prose)) {
		t.Fatal("an oracle sentence is oracle evidence")
	}
	if got := ClassifyCompletionReport(prose); got != CompletionNoClaim || LooksLikeBareDone(prose) || HasOracleOrRisk(prose) {
		t.Fatalf("class=%s bare=%v oracle=%v", got, LooksLikeBareDone(prose), HasOracleOrRisk(prose))
	}
	const agent = "jv-t7664-prose-oracle"
	s, reg := reapSinkServer(t, agent)
	s.agentEventSink(agent)(claudia.Event{Type: "assistant", Text: prose, StopReason: "end_turn"})
	if reg.Def(agent) == nil {
		t.Fatal("oracle sentence reaped the seat")
	}

	finish := declaredFinishReport("T766.4", prose)
	if got := ClassifyCompletionReport(finish); got != CompletionOracleEvidence || !HasOracleOrRisk(finish) || !HasOracleEvidence(finish) {
		t.Fatalf("declared class=%s oracle=%v slot=%v", got, HasOracleOrRisk(finish), HasOracleEvidence(finish))
	}
	const doneAgent = "jv-t7664-declared-oracle"
	s2, reg2 := reapSinkServer(t, doneAgent)
	s2.agentEventSink(doneAgent)(claudia.Event{Type: "assistant", Text: finish, StopReason: "end_turn"})
	if reg2.Def(doneAgent) != nil {
		t.Fatal("declared finish stayed registered")
	}
}

// TestT7664ProseOracleDoesNotCloseOwnerIntent drives the owner-intent
// answer path. An oracle sentence is not oracle evidence and does not
// close the complaint. A declared finish-report with an oracle slot does.
// An accepted-risk sentence is not a risk slot. The sink keeps that seat
// and removes a declared risk finish.
func TestT7664ProseOracleDoesNotCloseOwnerIntent(t *testing.T) {
	owner := "Why is the jiggle layout thrash still happening? (T766.4)"
	prose := "Done. The jiggle layout thrash is fixed. go test ./internal/mcpserver -run Layout PASS"
	if HasOracleEvidence(prose) || HasAcceptedRisk(prose) {
		t.Fatal("a sentence is oracle evidence or accepted risk")
	}
	if OwnerIntentAnsweredWithProductEvidence(owner, []string{prose}) {
		t.Fatal("oracle sentence closed the owner intent")
	}
	if wouldHaveFinishedBeforeT750(strings.ToLower(prose)) {
		t.Fatal("a completion sentence is a historical finish")
	}
	riskProse := "Done. The jiggle layout thrash fix carries accepted risk for the owner."
	if HasAcceptedRisk(riskProse) || hasAcceptedRisk(strings.ToLower(riskProse)) {
		t.Fatal("an accepted-risk sentence is a risk slot")
	}

	finish := declaredFinishReport("T766.4", "The jiggle layout thrash is fixed.")
	if !HasOracleEvidence(finish) {
		t.Fatal("declared sha slot is not oracle evidence")
	}
	if !OwnerIntentAnsweredWithProductEvidence(owner, []string{finish}) {
		t.Fatal("declared oracle did not close the owner intent")
	}
	risk := declaredRiskFinishReport("T766.4", "The jiggle layout thrash fix is residual.")
	if !HasAcceptedRisk(risk) || HasOracleEvidence(risk) {
		t.Fatalf("risk slot accepted=%v oracle=%v", HasAcceptedRisk(risk), HasOracleEvidence(risk))
	}

	const agent = "jv-t7664-prose-risk"
	s, reg := reapSinkServer(t, agent)
	s.agentEventSink(agent)(claudia.Event{Type: "assistant", Text: riskProse, StopReason: "end_turn"})
	if reg.Def(agent) == nil {
		t.Fatal("accepted-risk sentence reaped the seat")
	}
	const doneAgent = "jv-t7664-declared-risk"
	s2, reg2 := reapSinkServer(t, doneAgent)
	s2.agentEventSink(doneAgent)(claudia.Event{Type: "assistant", Text: risk, StopReason: "end_turn"})
	if reg2.Def(doneAgent) != nil {
		t.Fatal("declared risk finish stayed registered")
	}
}

// TestT7664MarkerWordsDoNotVetoReapOnADirtyTree drives the reap decision
// on a dirty git worktree. "All tests pass" is not an uncited-claim veto.
// The prose seat stays. A declared risk finish of that sentence is removed.
func TestT7664MarkerWordsDoNotVetoReapOnADirtyTree(t *testing.T) {
	prose := "Implemented the fix. All tests pass. Done."
	dir := seededGitWorktree(t, dirtyTrackedEdit)
	tree := gate.ProbeTree(dir)
	if tree == nil || tree.Clean || tree.DirtyFiles == 0 {
		t.Fatal("fixture is not a dirty worktree")
	}
	if UncitedClaimAgainstDirtyWorktree(prose, tree) {
		t.Fatal("marker words vetoed a dirty worktree")
	}
	reg := regWithWorkerDir(t, dir)
	ok, reason := ShouldAutoReapDoneWorkAgent(reg, "worker", prose, func(string) bool { return false })
	if ok || reason != "unknown_report" {
		t.Fatalf("prose reap=%v reason=%s", ok, reason)
	}

	finish := declaredRiskFinishReport("T766.4", prose)
	const agent = "jv-t7664-dirty-marker"
	sinkReg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := sinkReg.Register(claudia.AgentDef{
		Name: agent, WorkDir: dir, SessionID: "s-dirty",
		Purpose: claudia.PurposeWork, Parent: "jevons-po",
		Materialized: true, Provider: "grok", TargetID: "T766.4",
	}); err != nil {
		t.Fatal(err)
	}
	s := &Server{registry: sinkReg, notifyJevon: func(string) {}}
	s.agentEventSink(agent)(claudia.Event{Type: "assistant", Text: prose, StopReason: "end_turn"})
	if sinkReg.Def(agent) == nil {
		t.Fatal("marker sentence reaped the seat")
	}
	s.agentEventSink(agent)(claudia.Event{Type: "assistant", Text: finish, StopReason: "end_turn"})
	if sinkReg.Def(agent) != nil {
		t.Fatal("declared risk finish stayed registered")
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
		"FindCompletionClaim":    true,
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

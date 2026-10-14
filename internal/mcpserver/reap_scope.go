// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"log/slog"
	"os/exec"
	"strings"

	"github.com/marcelocantos/jevons/internal/envelope"
	"github.com/marcelocantos/jevons/internal/gate"
)

// 🎯T972 (split from 🎯T784, whose classifier-fix half landed separately): a
// pre-reap scope-scan. A finished-work report that reads as done is not
// always done — the seat may be mid-background-gate, or its own worktree may
// carry uncommitted work the report never mentioned. Reaping such a seat
// loses that scope: the process is gone, nothing can finish the gate wait or
// commit the pending diff, and the parent has no notice that anything was
// left behind.
//
// This scan runs before the reap decision and, when it finds outstanding
// scope, keeps the seat registered instead — retention is cheap (the seat
// sits idle until the next classification, or a human/parent acts), and a
// reap is irreversible (🎯T435's own framing: "reaping is destructive and
// irreversible, keeping is cheap").

// OutstandingScope is one thing a finished-work report leaves unresolved.
type OutstandingScope struct {
	// Kind is a short machine-readable tag: "pending_gate" or
	// "owned_uncommitted". Used as a lifecycle-log/reason suffix.
	Kind string
	// Detail is the operator-facing sentence naming the specific scope —
	// what a parent notice quotes.
	Detail string
}

// OutstandingScopeReasons is the pure decision: given the report text and a
// probe of the seat's own worktree, name the scope still outstanding. tree
// may be nil (not a git worktree, or the probe failed) and ownWorktree false
// when the workdir could not be distinguished from a shared clone — both
// degrade to skipping the uncommitted-paths check rather than guessing.
//
// ownWorktree matters on purpose: a dirty shared clone is not this seat's
// scope to answer for (other agents may share it), so only a seat's own
// isolated worktree (🎯T254.2) counts uncommitted paths as owned. A report
// that declares a blocking wait on a tracked background gate (🎯T565's own
// classifier, DeclaresBlockingGateWait) is always this seat's scope
// regardless of where it runs.
func OutstandingScopeReasons(report string, tree *gate.TreeProvenance, ownWorktree bool) []OutstandingScope {
	var out []OutstandingScope
	if scope := midWorkReportScope(report); scope != nil {
		out = append(out, *scope)
	}
	if declaresBlockingGateWaitLocalized(report) {
		out = append(out, OutstandingScope{
			Kind:   "pending_gate",
			Detail: "the report declares a blocking wait on a tracked background gate that has not resolved",
		})
	} else if span, ok := FindActiveExternalWait(report); ok {
		// 🎯T1024: the same scope narrated in monitoring vocabulary ("alive
		// and still queued", "monitor armed ... still watching") that the
		// T565 verb list does not hear. Only when T565 was silent, so a
		// report both recognise logs once, under the classifier that
		// existed first.
		out = append(out, OutstandingScope{
			Kind:   activeWaitScopeKind,
			Detail: "the report narrates an active wait on an alive external process it does not control: " + strings.TrimSpace(span),
		})
	}
	if ownWorktree && tree != nil && !tree.Clean && tree.DirtyFiles > 0 {
		sample := strings.Join(tree.DirtySample, ", ")
		if sample == "" {
			sample = "(no sample)"
		}
		out = append(out, OutstandingScope{
			Kind: "owned_uncommitted",
			Detail: fmt.Sprintf(
				"%d uncommitted change(s) owned in %s were never committed: %s",
				tree.DirtyFiles, tree.Repo, sample,
			),
		})
	}
	return out
}

// midWorkReportScope gives declared remaining work precedence over the
// envelope kind (T784). Evidence for a slice does not close its mission.
func midWorkReportScope(report string) *OutstandingScope {
	m, err := envelope.Parse(report)
	if m == nil || err != nil || m.Kind != envelope.KindFinishReport {
		return nil
	}
	if m.Status == envelope.ProgressInProgress {
		return &OutstandingScope{Kind: "in_progress", Detail: "the worker explicitly reports status in-progress"}
	}
	if hasForwardLookingPlan(m.Payload) || ClassifyReportAsk(m.Payload) == AskExplicitIncomplete || ClassifyReportAsk(m.Payload) == AskCheckpoint {
		return &OutstandingScope{Kind: "remaining_work", Detail: "the worker names remaining work: " + m.Payload}
	}
	return nil
}

// probeOwnWorktree reports whether dir is its own linked git worktree
// (🎯T254.2 per-seat isolation) as opposed to the shared clone every seat's
// workdir could otherwise resolve to. A linked worktree's --git-dir sits
// under the shared repo's --git-common-dir/worktrees/<name>; the shared
// clone's own working tree answers the same path for both. Best-effort:
// any git failure (not a repo, git missing) answers false, which is the
// same "cannot establish ownership, do not guess" default OutstandingScopeReasons
// already applies to a nil tree.
// declaresBlockingGateWaitLocalized is DeclaresBlockingGateWait (🎯T565)
// scoped to one sentence/line at a time rather than the whole report.
//
// 🎯T972 regression: a report that discusses the ask-vocabulary itself —
// "a report that names an input it is waiting on" in one bullet, "GATE
// t439-... exit=0 GREEN" naming a resolved gate three bullets later —
// satisfied DeclaresBlockingGateWait's whole-text scan (verb anywhere,
// object anywhere) though the report was a genuine finish describing
// neither an active wait nor an unresolved gate (TestT446MetaMentionReportReaps).
// A wait verb and a gate object only mean "still waiting" when they land in
// the same clause the worker is actually reporting from.
func declaresBlockingGateWaitLocalized(report string) bool {
	for _, sentence := range splitReportSentences(report) {
		if DeclaresBlockingGateWait(sentence) {
			return true
		}
	}
	return false
}

// splitReportSentences breaks report text on sentence/line boundaries
// (period, newline, exclamation, question mark) — coarse but sufficient:
// it only needs to keep phrases from different clauses out of the same
// chunk, not produce linguistically exact sentences.
func splitReportSentences(report string) []string {
	var out []string
	start := 0
	for i, r := range report {
		switch r {
		case '.', '\n', '!', '?':
			if s := strings.TrimSpace(report[start:i]); s != "" {
				out = append(out, s)
			}
			start = i + 1
		}
	}
	if s := strings.TrimSpace(report[start:]); s != "" {
		out = append(out, s)
	}
	return out
}

func probeOwnWorktree(dir string) bool {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return false
	}
	gitDir, err := scopeGitOut(dir, "rev-parse", "--git-dir")
	if err != nil || gitDir == "" {
		return false
	}
	commonDir, err := scopeGitOut(dir, "rev-parse", "--git-common-dir")
	if err != nil || commonDir == "" {
		return false
	}
	return gitDir != commonDir
}

func scopeGitOut(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// outstandingScopeReasonsForWorker is the I/O-performing wrapper
// ShouldAutoReapDoneWorkAgent and the notify path share: probe the worker's
// own worktree and run the pure decision over it.
func outstandingScopeReasonsForWorker(report, workDir string) []OutstandingScope {
	workDir = strings.TrimSpace(workDir)
	if workDir == "" {
		return OutstandingScopeReasons(report, nil, false)
	}
	return OutstandingScopeReasons(report, gate.ProbeTree(workDir), probeOwnWorktree(workDir))
}

// outstandingScopeKind is the first (and lifecycle-log-worthy) scope kind,
// or "" when none. Callers that need the full list call
// outstandingScopeReasonsForWorker directly.
func outstandingScopeKind(scope []OutstandingScope) string {
	if len(scope) == 0 {
		return ""
	}
	return scope[0].Kind
}

// outstandingScopeReapReason is the ShouldAutoReapDoneWorkAgent reason
// string for a retained seat — "outstanding_scope_pending_gate" or
// "outstanding_scope_owned_uncommitted".
const outstandingScopeReapReasonPrefix = "outstanding_scope_"

func outstandingScopeReapReason(scope []OutstandingScope) string {
	return outstandingScopeReapReasonPrefix + outstandingScopeKind(scope)
}

// FormatOutstandingScopeNotice is the 🎯T972 parent-facing notice: a
// finished-work report was kept, not reaped, and why. Named after the
// existing 🎯T577 checkpoint-reap notice shape (FormatCheckpointReapRespawnNotice)
// but for the opposite decision — here the seat stays registered rather
// than being replaced.
func FormatOutstandingScopeNotice(targetID, worker string, scope []OutstandingScope) string {
	tid := FormatTargetID(targetID)
	if tid == "" {
		tid = "its target"
	}
	w := strings.TrimSpace(worker)
	if w == "" {
		w = "worker"
	}
	var details []string
	for _, sc := range scope {
		details = append(details, sc.Detail)
	}
	return fmt.Sprintf(
		"[reap-scope 🎯T972] %s worker %s reported finished work but was kept, not reaped — outstanding scope: %s. It stays registered until that scope clears (commit, gate resolves) or you act on it directly.",
		tid, w, strings.Join(details, "; "),
	)
}

// notifyParentOutstandingScope tells the seat's parent its finished-work
// report was kept rather than reaped, naming the scope. Mirrors
// notifyPORespawnAfterCheckpointReap's escalate-to-fleet-health-on-failure
// shape (🎯T577).
func (s *Server) notifyParentOutstandingScope(parent, worker, targetID string, scope []OutstandingScope) {
	if s == nil || strings.TrimSpace(worker) == "" || len(scope) == 0 {
		return
	}
	p := strings.TrimSpace(parent)
	if p == "" {
		p = "jevons-po"
	}
	msg := FormatOutstandingScopeNotice(targetID, worker, scope)
	if _, err := s.deliverByName(p, msg, OriginAgent, false); err != nil {
		slog.Warn("T972 outstanding-scope notice undelivered to parent; escalating to overseer",
			"parent", p, "worker", worker, "target", targetID, "err", err)
		s.notifyFleetHealth(worker, fmt.Sprintf("parent %s unreachable (%v) for: %s", p, err, msg))
	}
}

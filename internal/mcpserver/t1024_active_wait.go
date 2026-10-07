// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import "strings"

// 🎯T1024: a worker actively waiting on an alive external process it does
// not control is not finished work, whatever completion word a sub-step
// earned along the way.
//
// Two arrai seats were reaped as finished_work on 2026-10-07 while queued
// on the shared heavy gate lease. The lifecycle log names the phrases:
//
//	arrai-t41-nest-quadratic  claim_marker=finished
//	  report_span="50365 finished (another worker's gate)."
//	arrai-t41-verify-land     claim_marker=done
//	  report_span="The landing is done and verified;"
//
// Neither sentence claims the mission is over — one says a sibling's PID
// exited, the other closes one step and opens the next in the same breath
// ("the only remaining step is the gate verdict") — but each report also
// carries oracle evidence (a GREEN gate id, a merge SHA proven an
// ancestor), and hasFinishShape reads completion word + oracle evidence as
// the 🎯T31 finish report. That is a deliberate shape and this file does
// not touch it. What should have caught both reports is the 🎯T972
// pre-reap scope scan, whose pending-gate half is 🎯T565's
// DeclaresBlockingGateWait: a wait VERB ("waiting on", "still running")
// near a gate OBJECT. The real reports narrate the wait in the vocabulary
// of monitoring, not of waiting — "alive and still queued", "queued fifth
// on the shared heavy lease", "the monitor armed last turn is still
// watching the result file", "Still ahead in queue" — and none of those
// is a T565 verb, so the veto never fired and the finish shape reaped.
//
// This classifier recognises that vocabulary: a sentence that (a) states a
// present, continuing wait state, (b) names the external thing being
// waited on, and (c) does not also say the wait resolved. All three in one
// sentence, so a finish that recounts a wait it already saw through
// ("queued behind two siblings and came back GREEN") is not retained, and
// a report that merely mentions `bin/gate` in passing is not either. The
// product consumers are the 🎯T972 scope scan (keeps the seat, notifies
// the parent, logs the sentence) and 🎯T985's disposition (park, not
// reap). The sentinel's finished_awaiting_gate reading
// (fillIdleResidueEvidence) is a separate path that consumed the same
// LooksLikeFinishedWorkReport verdict; it now reads outstanding scope too.
//
// The bias is the reap chain's own: a wait read as a finish is
// irreversible, a finish read as a wait costs an idle seat the parent is
// told about. So the vocabulary below leans toward recognising a wait, and
// the resolution markers are what keep a genuine finish reaping.

// activeWaitStates are present-tense, continuing descriptions of a wait on
// something that has not come back yet. Substring match on the lowered,
// claim-masked sentence (🎯T750: quoted text is not the worker's narration).
var activeWaitStates = []string{
	"queued", "in queue", "in the queue", "ahead in queue", "ahead of mine",
	"still running", "is running", "now running", "currently running",
	"still alive", "is alive", "alive and", "and alive",
	"in progress", "still pending", "is pending",
	"not yet resolved", "has not resolved", "hasn't resolved", "unresolved",
	"waiting on", "waiting for", "wait on", "wait for", "still waiting",
	"keep waiting", "continue waiting", "wait patiently", "awaiting",
	"watching", "monitor armed", "monitor is armed", "armed to",
	"will notify", "notify me", "notifies me", "when the monitor fires",
	"when it exits", "when it finishes", "when it completes",
	"until it exits", "until it finishes", "until it completes", "until it returns",
	"polling", "held by", "lease holder", "contention",
}

// externalProcessObjects name the thing waited on: a gate, the lease or
// lock it contends for, the queue it sits in, the monitor watching it, or
// a background process by kind. Whole-word match (indexWordish) so "gates"
// is listed separately and "specific" is not "ci". "daemon" is deliberately
// absent: "the development daemon is running abc123" is activation
// evidence in a finish report (🎯T632), not a wait.
var externalProcessObjects = []string{
	"gate", "gates", "lease", "lock", "queue", "monitor", "background",
	"process", "pid", "job", "run", "task", "ci", "build",
	"go test", "make test", "test-go", "test-journey",
}

// waitResolvedMarkers say, in the same sentence, that the wait is over or
// never was: a verdict, an exit, a past tense. A sentence carrying one is a
// recount, not an active wait. Whole-word match.
var waitResolvedMarkers = []string{
	"green", "red", "exit=", "exited", "passed", "failed", "finished",
	"completed", "resolved", "returned", "came back", "gone", "died", "dead",
	"stopped", "killed", "no longer", "not queued", "not running", "not alive",
	"isn't running", "is not running", "was queued", "were queued",
	"was running", "was waiting", "waited", "had been", "has finished",
	"have finished", "did finish",
}

// FindActiveExternalWait reports whether some sentence of report narrates
// an active wait on a named, alive external process the worker does not
// control (🎯T1024), and returns the first such sentence as written so the
// lifecycle log and the parent notice can quote it.
func FindActiveExternalWait(report string) (span string, ok bool) {
	if strings.TrimSpace(report) == "" {
		return "", false
	}
	lower := claimScanText(asciiLower(report))
	start := 0
	for i := 0; i <= len(lower); i++ {
		if i < len(lower) && !sentenceDelimiter(rune(lower[i])) {
			continue
		}
		if activeWaitSentence(lower[start:i]) {
			s := strings.TrimSpace(report[start:i])
			return s, s != ""
		}
		start = i + 1
	}
	return "", false
}

// declaresActiveExternalWait is FindActiveExternalWait as a predicate.
func declaresActiveExternalWait(report string) bool {
	_, ok := FindActiveExternalWait(report)
	return ok
}

// sentenceDelimiter is splitReportSentences' boundary set (🎯T972): the
// unit a wait state and its object must share.
func sentenceDelimiter(r rune) bool {
	return r == '.' || r == '\n' || r == '!' || r == '?'
}

// activeWaitSentence is the per-sentence decision: a wait state, an
// external object, and no resolution marker.
func activeWaitSentence(lower string) bool {
	if strings.TrimSpace(lower) == "" {
		return false
	}
	if !containsAny(lower, activeWaitStates) {
		return false
	}
	object := false
	for _, o := range externalProcessObjects {
		if indexWordish(lower, o) >= 0 {
			object = true
			break
		}
	}
	if !object {
		return false
	}
	for _, m := range waitResolvedMarkers {
		if indexWordish(lower, m) >= 0 {
			return false
		}
	}
	return true
}

// declaresUnresolvedExternalWait is the union the reap consumers read: the
// 🎯T565 blocking-gate-wait verb shape, sentence-localized (🎯T972), or the
// 🎯T1024 monitoring shape.
func declaresUnresolvedExternalWait(report string) bool {
	return declaresBlockingGateWaitLocalized(report) || declaresActiveExternalWait(report)
}

// activeWaitScopeKind is the OutstandingScope kind a 🎯T1024 retention logs
// as: outstanding_scope_active_wait. Distinct from 🎯T972's pending_gate so
// the lifecycle record names the classifier that read the report (🎯T439).
const activeWaitScopeKind = "active_wait"

// storedReportLooksFinished is the sentinel's reading of a seat's latest
// stored report (🎯T410 finished_awaiting_gate): a finish shape that leaves
// no scope outstanding. A report that is mid-gate, mid-wait, or typed
// in-progress is not finished however it reads, so the sentinel does not
// file a close-target against a worker that is still waiting (🎯T1024).
func storedReportLooksFinished(text string) bool {
	return LooksLikeFinishedWorkReport(text) && len(OutstandingScopeReasons(text, nil, false)) == 0
}

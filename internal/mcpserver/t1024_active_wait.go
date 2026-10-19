// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

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
// pre-reap scope scan, whose pending-gate half was a wait verb
// ("waiting on", "still running") near a gate object. The real reports narrate the wait in the vocabulary
// of monitoring, not of waiting — "alive and still queued", "queued fifth
// on the shared heavy lease", "the monitor armed last turn is still
// watching the result file", "Still ahead in queue" — and none of those
// is a T565 verb, so the veto never fired and the finish shape reaped.
//
// FindActiveExternalWait does not read that vocabulary (🎯T766.4). A
// sentence is not a wait slot. The typed wait is DeclaresBlockingGateWait
// (status in-progress or blocked). These reports declare no envelope, so
// they are unknown: not an active wait and not a finish. The scope scan
// and the stop disposition do not keep a seat because of the narration.

// FindActiveExternalWait reports no span. Monitoring prose is not a wait.
func FindActiveExternalWait(string) (span string, ok bool) {
	return "", false
}

// declaresActiveExternalWait is FindActiveExternalWait as a predicate.
func declaresActiveExternalWait(report string) bool {
	_, ok := FindActiveExternalWait(report)
	return ok
}

// declaresUnresolvedExternalWait is a typed status wait. Monitoring prose
// is not a member. The reap path does not call this function.
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

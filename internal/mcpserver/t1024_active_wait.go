// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"github.com/marcelocantos/jevons/internal/envelope"
)

// 🎯T1024: a worker actively waiting on an alive external process it does
// not control is not finished work, whatever completion word a sub-step
// earned along the way.
//
// Two arrai seats were reaped as finished_work on 2026-10-07 while queued
// on the shared heavy gate lease. Their reports narrated the wait in the
// vocabulary of monitoring — "alive and still queued", "queued fifth on the
// shared heavy lease", "the monitor armed last turn is still watching the
// result file" — beside a completion word for one sub-step ("The landing is
// done and verified") and real oracle evidence. The 🎯T1024 fix was a
// sentence detector; 🎯T766.4 removed it, because a control does not
// decide by English, and with it went the protection.
//
// 🎯T766.4.1 makes the protection a typed fact. A seat that is waiting
// says so in its envelope:
//
//	jevons: active-wait gate t41-slowpath on 9890903 (queued fifth on the shared heavy lease)
//
// (schema: internal/envelope, ActiveWait / WaitClass). The slot decides;
// the payload never does:
//
//   - A finish-report that carries it is contradictory. ClassifyDone answers
//     Unknown, never Done, and the envelope check tells the author why.
//   - The reap path keeps the seat (outstanding_scope_active_wait) and,
//     for that contradictory finish-report, tells the parent what it waits
//     on (🎯T972 notice). A status-ping that carries it keeps the seat
//     quietly (active_wait): that is the instructed shape of a mid-wait turn.
//   - The stop and idle dispositions park it: an external wait that will
//     resume is the 🎯T985 park, never a reap_stop.
//   - Idle-nudge and impatience hold off (DeclaresBlockingGateWait).
//
// A report that declares nothing is unknown: it neither reaps nor parks on
// the strength of its narration.

// activeWaitScopeKind is the OutstandingScope kind a 🎯T1024 retention logs
// as: outstanding_scope_active_wait. Distinct from 🎯T972's in_progress so
// the lifecycle record names the slot that kept the seat (🎯T439).
const activeWaitScopeKind = "active_wait"

// activeWaitKeepReason is the reap reason for a non-finish report that
// declares an active wait (a status-ping mid-wait). It is not an
// outstanding-scope reason: the parent already knows the seat is working,
// and a notice per polling turn would be chatter.
const activeWaitKeepReason = "active_wait"

// declaredActiveWait is the typed read of the 🎯T1024 slot.
func declaredActiveWait(report string) (envelope.ActiveWait, bool) {
	return envelope.ActiveWaitIn(report)
}

// activeWaitScope is the outstanding scope a declared wait names, for the
// lifecycle log and the parent notice.
func activeWaitScope(w envelope.ActiveWait) OutstandingScope {
	return OutstandingScope{
		Kind:   activeWaitScopeKind,
		Detail: "the worker declares an active wait on " + string(w.Class) + " " + w.On,
	}
}

// storedReportLooksFinished is the sentinel's reading of a seat's latest
// stored report (🎯T410 finished_awaiting_gate): a declared finish that
// leaves no scope outstanding. A report that declares an active wait or
// status in-progress is not finished, so the sentinel does not file a
// close-target against a worker that is still waiting (🎯T1024).
func storedReportLooksFinished(text string) bool {
	return LooksLikeFinishedWorkReport(text) && len(OutstandingScopeReasons(text, nil, false)) == 0
}

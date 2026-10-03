// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"log/slog"
	"time"
)

// 🎯T915: an owner cancel leaves nothing queued able to run ahead of the
// owner's next send.
//
// settleCancel used to drain the notify queue the moment it cleared the
// turn, so whatever had queued while the cancelled turn ran became the
// overseer's next turn: on 2026-09-29 the post-boot daemon-restarted brief
// (J3, sessions 5b6758ac / e11042f2), and an owner-health re-injection of the
// very prompt the owner had just cancelled (e9620c51), which re-ran it until
// shutdown. The owner's replacement then waited behind that turn.
//
// A cancel now holds the queue. The owner's next send releases the hold and
// is put first; if the owner sends nothing, the window lapses and the queue
// drains as before. Copies of the cancelled prompt that the daemon
// re-injected (they carry no owner message id) are dropped outright: the
// owner stopped that prompt, and running it again is the opposite of what
// they asked for. Copies the owner sent themselves keep their ids and stay.

// ownerCancelHoldDefault bounds how long an owner cancel holds the queue
// when no owner send follows it. Fleet notes are informational; a minute's
// delay costs little, and it covers the owner composing a replacement.
const ownerCancelHoldDefault = time.Minute

// settleOwnerCancel is settleCancel for the owner's own cancel: drop the
// daemon's copies of the cancelled prompt and hold the queue, then settle.
// The hold is armed first so the settle's own drain already sees it.
func (s *Server) settleOwnerCancel() {
	s.mu.Lock()
	dropped := s.purgeCancelledOwnerCopiesLocked()
	s.armOwnerCancelHoldLocked()
	depth := len(s.notifyQueue)
	s.mu.Unlock()
	if dropped > 0 {
		s.persistOwnerQueue()
	}
	slog.Info("notify_queue",
		"component", "notify_queue",
		"decision", "owner_cancel_hold",
		"depth", depth,
		"dropped_reinjected", dropped,
	)
	s.settleCancel()
}

// purgeCancelledOwnerCopiesLocked removes queued copies of the in-flight
// owner prompt beyond those the owner sent (one per registered message id)
// and returns how many it removed. Caller holds mu.
func (s *Server) purgeCancelledOwnerCopiesLocked() int {
	text := s.overseerOwnerTurnText
	if text == "" || !s.waiting || !s.overseerOwnerTurn {
		return 0
	}
	keep := len(s.notifyOwnerIDs[text])
	out := s.notifyQueue[:0:0]
	dropped := 0
	for _, n := range s.notifyQueue {
		if n == text {
			if keep == 0 {
				dropped++
				continue
			}
			keep--
		}
		out = append(out, n)
	}
	s.notifyQueue = out
	s.observeOwnerQueueLocked()
	return dropped
}

// armOwnerCancelHoldLocked starts (or restarts) the hold. Caller holds mu.
func (s *Server) armOwnerCancelHoldLocked() {
	if s.ownerCancelHoldTimer != nil {
		s.ownerCancelHoldTimer.Stop()
	}
	window := s.ownerCancelHoldWindow
	if window == 0 {
		window = ownerCancelHoldDefault
	}
	s.ownerCancelHold = true
	s.ownerCancelHoldGen++
	gen := s.ownerCancelHoldGen
	s.ownerCancelHoldTimer = time.AfterFunc(window, func() {
		s.mu.Lock()
		// A later cancel re-armed, or an owner send released: not ours.
		if !s.ownerCancelHold || s.ownerCancelHoldGen != gen {
			s.mu.Unlock()
			return
		}
		s.releaseOwnerCancelHoldLocked()
		s.mu.Unlock()
		slog.Info("notify_queue",
			"component", "notify_queue",
			"decision", "owner_cancel_hold_lapsed",
			"window", window,
		)
		s.drainOverseerNotes()
	})
}

// releaseOwnerCancelHoldLocked ends the hold. Caller holds mu and drains
// after unlocking.
func (s *Server) releaseOwnerCancelHoldLocked() {
	s.ownerCancelHold = false
	if s.ownerCancelHoldTimer != nil {
		s.ownerCancelHoldTimer.Stop()
		s.ownerCancelHoldTimer = nil
	}
}

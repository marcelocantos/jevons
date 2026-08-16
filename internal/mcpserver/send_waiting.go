// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/marcelocantos/jevons/internal/sendq"
	"github.com/marcelocantos/jevons/internal/turnev"
)

// 🎯T447 — WHAT THE DAEMON SAYS ABOUT A MESSAGE THAT IS MERELY WAITING.
//
// The drain hands a queued message to an idle agent and then reads the
// receiver for it. When the payload does not show up as a begun turn, one
// notice was written for every reason it might not have:
//
//	"a queued message (N bytes) was pasted after its turn ended and never
//	 became a turn … It is in that agent's composer, not lost"
//
// That sentence is true of exactly one of the three things that reach it. On
// 2026-08-14 it was published about a 17118-byte send for which a
// `queued_command` attachment already existed in the receiver's own file: the
// message had been accepted and was waiting, and the notice described it as a
// paste that never submitted. The reader is left to infer waiting from "not
// lost" — and the reader that night inferred the opposite, re-sent 17KB as a
// "first delivery", and reported correct behaviour to a product owner as a
// defect specimen.
//
// So the notice is split by what was actually READ of the receiver, and the
// waiting case says waiting, with the two numbers that make it actionable: how
// long the message has been in the queue, and where it sits in it. A count
// only the daemon can see is where "queued" goes to die (🎯T418); an age and a
// position are what let an operator tell a queue that is moving from one that
// is not, without opening a transcript.
//
// AND NOTHING ON THIS PATH MAY SUGGEST A RETRY. Clause 4 is a rule about the
// waiting case specifically, because the two recoveries an operator reaches
// for are both destructive there: a re-send stacks a duplicate behind the
// original, and a flush submits the entire accumulated composer backlog at
// once. Both happened. The advice is to wait, and the notice says so in words
// rather than by omitting the alternative.

// Fate is what the daemon's own observation of the receiver amounts to, in the
// vocabulary of the decoder that reads the receiver's file (🎯T422 clause 1).
//
// Derived, never measured a second time: TurnEvidence's payload fields are
// already populated by scanning the receiver's records, so this is a rename of
// findings that exist rather than a second instrument that could disagree with
// the first.
func (e TurnEvidence) Fate() turnev.Fate {
	switch {
	case e.PayloadSeen:
		return turnev.FateUserMessage
	case e.PayloadEnteredTurn:
		return turnev.FateEnteredTurn
	case e.PayloadQueued:
		return turnev.FateQueued
	case !e.Observed:
		// No instrument ran. Not an absence — a non-observation, and the
		// difference is the whole of this family of false reports.
		return turnev.FateUnknown
	default:
		return turnev.FateUnseen
	}
}

// Reading is the three-outcome answer this evidence supports: delivered,
// in-flight, lost — or undecided when nothing was read at all.
func (e TurnEvidence) Reading() turnev.Reading { return e.Fate().Reading() }

// reportDrainedSendNotBegun is what the drain says when the message it handed
// over did not become a turn. It reports, and deliberately does not re-queue:
// a message sitting in a composer that is re-queued is delivered twice the
// moment anything submits the first copy (🎯T416 clause 4).
func (s *Server) reportDrainedSendNotBegun(name string, entry sendq.Entry, ev TurnEvidence, now time.Time) {
	behind := s.pendingAgentSends(name)
	reading := ev.Reading()
	if reading == turnev.ReadingInFlight {
		slog.Info("🎯T447 drained message is waiting in the receiver's own queue",
			"component", "agent_send",
			"name", name,
			"reading", reading.String(),
			"fate", ev.Fate().String(),
			"age", entry.Age(now).Round(time.Second).String(),
			"behind", behind,
			"bytes", len(entry.Text),
		)
	} else {
		slog.Error("agent send queue: drained message not submitted",
			"component", "agent_send",
			"name", name,
			"status", "not_submitted",
			"reading", reading.String(),
			"evidence", ev.Detail,
			"remaining", behind,
			"bytes", len(entry.Text),
		)
	}
	s.notifyFleetHealth(DrainedSendNotice(name, len(entry.Text), reading, evidenceDetail(ev), entry.Age(now), behind))
}

// DrainedSendNotice is the operator-facing account of a drained message that
// did not become a turn. Pure, so the three readings can be exercised without a
// process, a queue directory or a clock — the notice IS the product here, and a
// notice only reachable through a live daemon is one nobody checks the wording
// of until it is wrong in front of an owner.
//
// age is how long the message has been waiting since the daemon accepted it,
// and behind is how many of that agent's messages are still queued after this
// one, so "position 1 of behind+1" is the head of a queue whose depth is named.
func DrainedSendNotice(name string, size int, reading turnev.Reading, detail string, age time.Duration, behind int) string {
	switch reading {
	case turnev.ReadingInFlight:
		return fmt.Sprintf(
			"WAITING, not lost: a queued message (%d bytes) for %q is in that agent's own queue — %s. "+
				"It is position 1 of %d in the daemon's queue for %[2]q and has been waiting %s since the daemon accepted it. "+
				"It becomes a turn when the agent's current one ends, and nothing is required of anyone until then. "+
				"DO NOT re-send it and DO NOT flush that composer: a re-send stacks a duplicate behind the original, "+
				"and a flush submits the whole accumulated backlog at once.",
			size, name, detail, behind+1, age.Round(time.Second))
	case turnev.ReadingUndecided:
		return fmt.Sprintf(
			"UNDECIDED: a queued message (%d bytes) for %q was handed over and nothing was observed of that agent afterwards — %s. "+
				"This is a failure to look, not a finding: it is NOT evidence the message was lost, and it is not evidence it landed. "+
				"%d more are queued behind it. Read %[2]q's own session records before concluding anything, "+
				"and do not re-send on this reading — a duplicate on a false negative is the more expensive mistake.",
			size, name, detail, behind)
	default:
		return fmt.Sprintf(
			"Undelivered backlog on %q: a queued message (%d bytes) was pasted after its turn ended and never became a turn — %s. "+
				"It is in that agent's composer, not lost, and %d more are still queued behind it. "+
				"It has NOT been re-queued: that would deliver a duplicate once anything submits the first copy.",
			name, size, detail, behind)
	}
}

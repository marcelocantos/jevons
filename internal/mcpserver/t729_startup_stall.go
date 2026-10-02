// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/marcelocantos/jevons/internal/agenterr"
	"github.com/marcelocantos/jevons/internal/fleetlog"
	"github.com/marcelocantos/jevons/internal/seatstop"
)

// 🎯T729 — a seat whose CLI stalled on startup is retried, not retired two
// seconds later.
//
// THE SPECIMEN. Three eventlog lines on ge-t190-desktop-cook, 2026-09-20:
//
//	15:19:48Z seat_stop      opening brief proven undelivered; seat released (🎯T387)
//	15:19:50Z unbriefed_seat retired a seat whose opening brief never landed (🎯T433)
//	15:19:50Z start error    Agent CLI stalled on startup (startup_stall / splash)
//
// Read in order they are a contradiction. The third line says the CLI was
// still drawing its splash screen when the ready timeout expired — the
// composer did not exist yet — and the first two say the brief was PROVEN
// undelivered and the seat NEVER BRIEFED. Both of those are claims about an
// agent that was working and did not answer. This agent was not working: it
// had not finished starting. 🎯T190 had landed code at 37515a9 and filed
// zero oracle evidence, because the only seat that could file it was gone.
//
// THE COMPOSITION, NOT THE BUG. Each guard is individually right:
//
//   - the ready timeout (claudia) must not wait forever for a composer;
//   - 🎯T387 must release a seat whose brief provably went nowhere, or
//     agent_list reports a phantom running worker;
//   - 🎯T433 must retire the ROW too, because engagement is read from the
//     registry, so a stopped-but-registered seat keeps 🎯T222 refusing a
//     second implementer and keeps the leaf looking consumed to 🎯T155.
//
// What composes them wrongly is the EVIDENCE, not any of the three actions.
// The ready timeout produces "the process never became ready"; 🎯T387/🎯T433
// consume it as "a ready pane was handed a brief and ignored it". They are
// different findings with different answers, and the daemon had one channel
// for both — a string, through agent_send's `send failed: <owner copy>`.
//
// So this file does not delete a check. It adds the missing distinction and
// gives the new branch the answer agenterr already promised for this class:
// agenterr.TransientBackend(ClassStartupStall) is true and the owner copy
// says in so many words "the seat is retried". On the spawn path it was not.
//
//  1. deliverStartPrompt retries the brief after a stall, once the CLI has
//     had a grace period to finish drawing. That is the whole fix for the
//     ordinary case: a splash screen that needed a few more seconds.
//  2. A stall that survives the retries still releases the seat — leaving a
//     never-ready seat registered would re-create exactly the leaf-consumed
//     failure 🎯T433 exists to prevent — but it releases it as
//     fleetlog.ReasonStartupStall, never as unbriefed_seat, and the parent
//     is told the class by name so it can act (respawn) rather than
//     discovering later that a target has no worker.

const (
	// defaultStartupStallRetries is how many extra attempts a stalled
	// opening brief gets. One is deliberate: a CLI still on its splash
	// screen usually needs seconds, and a CLI that is genuinely wedged
	// must reach its parent quickly rather than be nursed for minutes.
	defaultStartupStallRetries = 1
	// defaultStartupStallGrace is the wait before a retry. The stall means
	// the ready timeout already elapsed, so this is time for the pane to
	// finish what it was doing, not a poll interval.
	defaultStartupStallGrace = 20 * time.Second
)

// startBriefVerdict is the fork a failed opening brief takes.
type startBriefVerdict string

const (
	// startBriefInFlight: queued / delivered_unconfirmed (🎯T518) — the
	// brief is held or undecided. Keep the seat.
	startBriefInFlight startBriefVerdict = "in_flight"
	// startBriefNeverReady: the CLI never drew a composer (🎯T729). The
	// brief had nowhere to land; this is not evidence about the agent.
	startBriefNeverReady startBriefVerdict = "never_ready"
	// startBriefUndelivered: positive evidence of no brief on a ready pane
	// (🎯T387) — a clean "sent" whose watch saw nothing, or a send error
	// that disproves delivery.
	startBriefUndelivered startBriefVerdict = "undelivered"
)

// classifyStartBriefFailure is the pure fork. In-flight is checked first
// because it is a structural fact about this daemon's own verdict, and the
// stall check second because it is a fact about the provider's process;
// everything else is the 🎯T387 reap that was the only answer before.
func classifyStartBriefFailure(err error) startBriefVerdict {
	if err == nil {
		return startBriefUndelivered
	}
	if BriefInFlight(err) {
		return startBriefInFlight
	}
	if agenterr.ClassifyText(err.Error()) == agenterr.ClassStartupStall {
		return startBriefNeverReady
	}
	return startBriefUndelivered
}

// startBriefNeverReached reports the 🎯T729 branch: the opening brief failed
// because the agent CLI never became ready, so nothing can be concluded
// about whether the agent would have answered it.
func startBriefNeverReached(err error) bool {
	return classifyStartBriefFailure(err) == startBriefNeverReady
}

// releaseStalledSeat is 🎯T387's teardown under 🎯T729's vocabulary: the
// process is stopped and a row this call minted is retired (so the leaf is
// free for a fresh spawn rather than consumed by a seat that never ran),
// but the journal records a launch that never completed — not a worker that
// ignored its brief.
func (s *Server) releaseStalledSeat(name string, existed bool, cause error) bool {
	return s.releaseSeatAfterFailedBrief(name, existed, seatRelease{
		Cause:  cause,
		Source: seatstop.SourceStartupStall,
		StopReason: "agent CLI never became ready; the opening brief had no composer to land in " +
			"(startup_stall, 🎯T729)",
		RemovalReason: fleetlog.ReasonStartupStall,
		RemovalDetail: "released a seat whose CLI stalled on startup and did not recover on retry; " +
			"the brief never reached a ready pane (🎯T729)",
	})
}

// startupStallGrace is the wait before retrying a stalled brief. Tests set
// startStallGrace to zero so the retry is exercised without the wall clock.
func (s *Server) startupStallGrace() time.Duration {
	if s == nil {
		return defaultStartupStallGrace
	}
	if s.startStallGrace != nil {
		return *s.startStallGrace
	}
	return defaultStartupStallGrace
}

func (s *Server) startupStallRetries() int {
	if s == nil || s.startStallRetries == nil {
		return defaultStartupStallRetries
	}
	return *s.startStallRetries
}

// waitStartupStallGrace sleeps the grace period.
func (s *Server) waitStartupStallGrace() {
	if d := s.startupStallGrace(); d > 0 {
		time.Sleep(d)
	}
}

// noteStartBriefRetried records that a stall was retried, and how many
// times, on the error the caller will surface. A parent that is eventually
// told the spawn failed has to be able to see that waiting was already
// tried — otherwise "the seat is retried" reads as advice it should follow.
func noteStartBriefRetried(err error, attempts int) error {
	if err == nil || attempts < 1 {
		return err
	}
	return fmt.Errorf("%w (retried %s after startup_stall; the CLI never became ready)",
		err, pluralAttempts(attempts))
}

func pluralAttempts(n int) string {
	if n == 1 {
		return "once"
	}
	return fmt.Sprintf("%d times", n)
}

// logStartBriefRetry journals one retry so a stall that recovers is still
// visible — a spawn that silently needed two goes is a latency regression
// nobody would otherwise see.
func (s *Server) logStartBriefRetry(name string, attempt int, err error) {
	slog.Warn("opening brief stalled on CLI startup; retrying",
		"component", compAgentLifecycle, "name", name,
		"attempt", attempt, "grace", s.startupStallGrace().String(),
		"failure_class", agenterr.ClassStartupStall.String(),
		"err", strings.TrimSpace(err.Error()))
}

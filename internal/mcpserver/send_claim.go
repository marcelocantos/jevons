// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"errors"
	"strings"
)

// 🎯T429 — a failed send call answers "why did the CALL fail". The daemon was
// reading it as "what happened to the PAYLOAD", and on the tmux path the second
// question is not answerable from the first at all.
//
// THE OVER-CLAIM IS THE DEFECT, NOT THE UNCERTAINTY. 🎯T416 built the observer:
// five callers now ask the RECEIVER what became of a message instead of trusting
// the sender's own return value. What was left unfixed is the arm where the send
// call itself returns an error — because there the daemon never reached the
// observer at all. It took the transport's error, wrapped it as "send failed",
// and returned it, so a harness that could not SEE a submission was reported to
// the caller as a submission that did not HAPPEN.
//
// The two are not close. Nine confirmed instances on 2026-08-10, and two more on
// 2026-08-15 after the daily daemon was rebuilt onto a candidate root's fix, were
// all in the same dangerous direction — a negative verdict on a payload the
// receiver was already working from. jv-t434's brief was reported
// `turn not submitted: composer state=paste_chip after 8 Enter presses`; its own
// transcript shows two turns spent on exactly that brief. The frame tail on every
// instance reads `Press up to edit queued messages`, which is the CLI saying it
// ACCEPTED the message — the harness was staring at a successful delivery and
// calling it a stuck composer.
//
// WHY THE HARNESS CANNOT BE THE JUDGE HERE. claudia's submit loop presses Enter
// and re-reads the pane; its verdict is a classification of 24 lines of terminal
// chrome, taken from outside the process that owns the composer. That instrument
// belongs to the claudia harness and is being repaired there (clause 3,
// cl-t30-paste-chip-submit). It is not this daemon's to second-guess and not this
// daemon's to trust: what jevons owns is the verdict it hands its caller, and the
// rule for that verdict is the one 🎯T416 already established — the RECEIVER's own
// records decide, and where they cannot, the answer is unknown.
//
// So a send error is first asked a much narrower question than "did it fail":
// does it DISPROVE delivery? Only one of the three answers does.
type SendClaim int

const (
	// ClaimNotSent: the error is positive evidence the payload never reached
	// the receiver. The process was gone, the registry refused, the paste
	// itself errored — nothing was handed over, so there is nothing for the
	// receiver's records to hold and no reason to go looking.
	ClaimNotSent SendClaim = iota
	// ClaimSubmitUnverified: the payload was handed over and the transport
	// could not verify that it was submitted. A NON-observation — the pane
	// capture did not show the chrome the submit loop wanted, which is
	// consistent with a stuck composer, with a message the CLI queued behind a
	// live turn, and with a frame that was merely half-drawn. This is the claim
	// that must never be reported as a defect on its own.
	ClaimSubmitUnverified
	// ClaimReplyUnverified: the payload was handed over and the wait for a
	// REPLY ran out. It says the receiver had not finished answering inside a
	// window this daemon chose; it says nothing whatever about delivery, and
	// clause 5 is the instance — `The operation timed out` returned for a
	// payload sitting in the receiver's transcript at 10:22:51Z, 3873 bytes of
	// it, while the caller was told the operation had failed.
	ClaimReplyUnverified
)

func (c SendClaim) String() string {
	switch c {
	case ClaimSubmitUnverified:
		return "submit_unverified"
	case ClaimReplyUnverified:
		return "reply_unverified"
	default:
		return "not_sent"
	}
}

// DisprovesDelivery reports whether this error is itself evidence that the
// receiver never got the payload. Only ClaimNotSent is: the other two are
// failures to observe, and a failure to observe cannot distinguish "not there"
// from "not there yet".
func (c SendClaim) DisprovesDelivery() bool { return c == ClaimNotSent }

// submitUnverifiedMarkers are the shapes claudia's tmux submit loop returns when
// it gives up on recognising a submitted turn. Every one of them is a statement
// about a captured frame — composerStateName over the last capture, or an Enter
// keystroke it could not confirm landed — and the live specimens prove all of
// them fire over payloads the receiver holds.
//
// `composer empty after paste` is deliberately in this list rather than treated
// as a positive "never reached the pane". It is the same instrument reporting on
// the same capture, and nothing is lost by demoting it: an agent that genuinely
// holds nothing produces no receiver-side records either, so the evidence path
// reaches not_submitted anyway — by observation instead of by assertion.
var submitUnverifiedMarkers = []string{
	"turn not submitted",
	"composer state=",
	"enter presses",
	"send-keys enter",
}

// replyUnverifiedMarkers are the shapes a REPLY wait leaves behind. Kept narrow
// on purpose: the generic network timeouts agenterr treats as backend-unavailable
// ("connection timed out", "i/o timeout") are failures of the transport to reach
// the provider at all, and reading those as possible-deliveries would invent an
// uncertainty where there is none.
var replyUnverifiedMarkers = []string{
	"context deadline exceeded",
	"operation timed out",
	"timed out waiting",
	"timeout waiting",
}

// ClassifySendError decides what a send error says about the payload. Pure, and
// deliberately ordered: the submit markers are checked before the reply markers
// because claudia's failure text carries the last 400 bytes of the terminal
// frame, and a frame can quote anything at all — including the word "timeout"
// from whatever the agent had on screen.
func ClassifySendError(err error) SendClaim {
	if err == nil {
		return ClaimNotSent
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return ClaimReplyUnverified
	}
	msg := strings.ToLower(err.Error())
	for _, m := range submitUnverifiedMarkers {
		if strings.Contains(msg, m) {
			return ClaimSubmitUnverified
		}
	}
	for _, m := range replyUnverifiedMarkers {
		if strings.Contains(msg, m) {
			return ClaimReplyUnverified
		}
	}
	return ClaimNotSent
}

// describeTransportClaim renders the transport's own account for an operator,
// beside — never instead of — what the daemon observed of the receiver. The two
// disagreeing is the ordinary case this target exists to handle, so the message
// has to show both or the reader cannot tell which one they are being told.
func describeTransportClaim(err error) string {
	if err == nil {
		return ""
	}
	text := strings.TrimSpace(err.Error())
	const max = 300
	if len(text) > max {
		text = text[:max] + "…"
	}
	return text
}

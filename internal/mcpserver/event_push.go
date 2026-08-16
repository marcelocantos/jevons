// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/agenterr"
	"github.com/marcelocantos/jevons/internal/butler"
)

// registerEventPushTools exposes 🎯T34 event-triggered push via MCP so
// generic event sources (scripts, CI hooks, other agents) do not
// hard-code delivery paths.
func (s *Server) registerEventPushTools() {
	s.mcpSrv.AddTool(
		mcp.NewTool("jevons_event_push",
			mcp.WithDescription("Push an event-driven message into a target participant (butler thread or fleet agent) so it acts next (🎯T34/T114/T111.2). One deliver path by name/id: rehydrates a stopped process; returns a typed error if undeliverable. Never fails with 'no thread' when a registered agent exists. Use when a dependency landed, CI went green, a worker finished, a timer elapsed, etc. — not for owner chat (use jevons_thread_direct)."),
			mcp.WithString("target", mcp.Required(), mcp.Description("Thread ID or fleet agent name to push into")),
			mcp.WithString("event", mcp.Required(), mcp.Description("Event source/kind, e.g. ci, worker-finished, timer, dependency")),
			mcp.WithString("text", mcp.Required(), mcp.Description("What the agent should do next / what happened")),
		),
		s.handleEventPush,
	)
}

func (s *Server) handleEventPush(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.butler == nil {
		s.logLifecycle(compEventPush, "push", "error", map[string]any{
			"err": "butler not configured",
		})
		return mcp.NewToolResultError("event push: butler not configured"), nil
	}
	args := req.GetArguments()
	target := strings.TrimSpace(str(args["target"]))
	event := strings.TrimSpace(str(args["event"]))
	text := strings.TrimSpace(str(args["text"]))
	life := map[string]any{"target": target, "event": event}
	if target == "" || text == "" {
		s.logLifecycle(compEventPush, "push", "error", map[string]any{
			"target": target, "err": "target and text are required",
		})
		return mcp.NewToolResultError("target and text are required"), nil
	}
	if event == "" {
		event = "unknown"
		life["event"] = event
	}
	wire := butler.FormatEventPush(event, text)

	// 🎯T428. This tool is the one notification door that does NOT pass through
	// deliverToOverseer — butler.PushEvent reaches the agent process directly —
	// so the guard is applied here against the same ledger and the same wire
	// bytes, and a coach judgment that arrives once by send and once by push
	// still counts as one batch. Non-overseer targets are unaffected: this
	// target is about the channel the whole fleet reports into.
	var ticket notifyReplayTicket
	if s.isOverseerAgent(target) {
		var dec notifyReplayDecision
		ticket, dec = s.notifyReplays().Offer(wire)
		if !dec.Admit {
			life["suppressed_replay"] = dec.Reason
			life["offers"] = dec.Offers
			s.logLifecycle(compEventPush, "push", "ok", life)
			return mcp.NewToolResultText(
				describeReplaySuppression(target, dec, s.notifyReplays().Now())), nil
		}
	}

	// 🎯T429 clause 5 — TIMEOUT IS NOT ABSENCE. The observation is opened here,
	// before the push, so the receiver's records are read against a pre-push
	// baseline. Window zero: by the time this is awaited the push has already
	// waited out its own window, and waiting a second time would only be this
	// tool spending the caller's patience twice over.
	watch := s.watchAgentTurnForWindow(target, wire, 0)

	reply, err, timedOut := s.pushEventBounded(target, event, text)
	switch {
	case timedOut, err != nil && !ClassifySendError(err).DisprovesDelivery():
		// The push did not come back with an answer inside the window, or came
		// back with an error that is a failure to OBSERVE rather than evidence
		// of a failure to deliver. Either way the transport has not seen the
		// payload's fate, so it does not get to pronounce on it: the receiver's
		// own records do, and where they are silent the answer is unknown.
		life["reply_pending"] = true
		if err != nil {
			life["err"] = err.Error()
		}
		ev := watch()
		// 🎯T428 seals on the receiver's records, not on this tool's silence.
		ticket.Settle(ev.PayloadSeen || ev.PayloadEnteredTurn || ev.PayloadQueued)
		res := s.reportPushDeliveryUnjudged(target, event, ev, err, timedOut)
		s.logLifecycle(compEventPush, "push", "ok", life)
		return res, nil
	case err != nil:
		// Positive evidence the payload did not land, so remembering it would
		// refuse the retry that is the correct response.
		ticket.Abandon()
		life["err"] = err.Error()
		life["failure_class"] = agenterr.Classify(err).String()
		s.logLifecycle(compEventPush, "push", "error", life)
		// 🎯T283: same classification the owner chat path gets.
		return toolFailure("event_push", target, err), nil
	}

	// The receiver answered this push, which is as direct as arrival evidence
	// gets: it read the batch and replied to it.
	ticket.Settle(true)
	if class, ownerMsg, ok := agenterr.ReplyFailure(reply); ok {
		life["failure_class"] = class.String()
		s.logLifecycle(compEventPush, "push", "error", life)
		logProviderFailure("event_push", target, class, reply)
		return mcp.NewToolResultError(ownerMsg), nil
	}
	s.logLifecycle(compEventPush, "push", "ok", life)
	return mcp.NewToolResultText(fmt.Sprintf("Pushed event %q to %q.\n\n%s", event, target, reply)), nil
}

// 🎯T429 clause 5 — WHY THIS TOOL HAS A CLOCK OF ITS OWN.
//
// The specimen is a push that WORKED: 3873 bytes landed in the receiver's
// transcript at 10:22:51Z, line 115 of session 76cef0a9, and the caller was
// told `The operation timed out`. Nothing in the daemon wrote that sentence.
// PushEvent blocks on the receiver's REPLY, and a reply is a whole turn — the
// fleet allows it ten minutes (fleet.defaultReplyTimeout) — so the tool call
// outlived the caller's own MCP client bound and the client answered on the
// daemon's behalf. The daemon then finished the push into a caller that had
// stopped listening.
//
// A verdict invented by a client that never looked at the receiver is the same
// defect as a verdict invented by a submit loop that never looked at the
// receiver, so it takes the same fix: the daemon decides, from the receiver's
// records, before anyone else's clock expires. That requires a bound of its own,
// below any plausible client bound, after which delivery is answered separately
// from the reply — which is the honest split anyway, because delivery and
// completion were never the same question.
//
// The push is NOT cancelled at the bound. It is a real turn in a real agent, and
// killing it to make a tool call tidy would destroy work; it runs on, and its
// reply reaches the fleet on the ordinary notify path.
const defaultEventPushReplyWindow = 45 * time.Second

// EventPushReplyWindowEnv overrides defaultEventPushReplyWindow with a Go
// duration, for operators whose client bound differs and for oracles that need
// the bound to fire in test time.
const EventPushReplyWindowEnv = "JEVONS_EVENT_PUSH_REPLY_WINDOW"

func eventPushReplyWindow() time.Duration {
	raw := strings.TrimSpace(os.Getenv(EventPushReplyWindowEnv))
	if raw == "" {
		return defaultEventPushReplyWindow
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		slog.Warn("ignoring unusable event-push reply window",
			"component", compEventPush, "env", EventPushReplyWindowEnv,
			"value", raw, "using", defaultEventPushReplyWindow)
		return defaultEventPushReplyWindow
	}
	return d
}

// pushEventBounded runs the push and waits out its own window for the reply.
// timedOut says the window closed first — the push is still running, and the
// caller must decide delivery from the receiver rather than from this silence.
func (s *Server) pushEventBounded(target, event, text string) (reply string, err error, timedOut bool) {
	type outcome struct {
		reply string
		err   error
	}
	done := make(chan outcome, 1)
	go func() {
		r, e := s.butler.PushEvent(target, event, text)
		done <- outcome{r, e}
	}()
	select {
	case out := <-done:
		return out.reply, out.err, false
	case <-time.After(eventPushReplyWindow()):
		// The abandoned wait still has to land somewhere a human can read, or
		// this becomes a tool that silently drops replies. Same goroutine, no
		// second wait imposed on the caller.
		go func() {
			out := <-done
			slog.Info("🎯T429 event push completed after its reply window closed",
				"component", compEventPush, "target", target, "event", event,
				"reply_bytes", len(out.reply), "err", out.err)
		}()
		return "", nil, true
	}
}

// reportPushDeliveryUnjudged answers a push whose reply the transport could not
// see, from what the receiver's own records show (🎯T429 clauses 1, 4 and 5).
//
// Never an MCP error result. An error result is read by every caller as "this
// did not happen", and the whole finding of this target is that the payload
// usually DID: six confirmed instances in one day, all in that direction, and
// nine composer copies stacked by re-sends on top of them.
func (s *Server) reportPushDeliveryUnjudged(target, event string, ev TurnEvidence, pushErr error, timedOut bool) *mcp.CallToolResult {
	claim := describeTransportClaim(pushErr)
	why := "the reply did not arrive inside this daemon's push window"
	if !timedOut {
		why = fmt.Sprintf("the push transport reported %q, which is a statement about the CALL and not about %q", claim, target)
	}

	if ev.Positive() {
		slog.Info("🎯T429 event push delivered; reply unseen",
			"component", compEventPush, "target", target, "event", event,
			"evidence", ev.Detail, "transport_claim", claim, "timed_out", timedOut)
		return mcp.NewToolResultText(fmt.Sprintf(
			"Pushed event %q to %q — DELIVERED (status: delivered_confirmed), reply not seen.\n\n"+
				"Confirmed from %[2]q's own records: %s.\n"+
				"No reply is reported here because %s; the turn runs on and its reply reaches the "+
				"fleet on the normal notify path.\n"+
				"DO NOT re-push: the payload is already in %[2]q's turn, and a second copy stacks behind it.",
			event, target, evidenceDetail(ev), why))
	}

	slog.Warn("🎯T429 event push unconfirmed; transport could not see the payload's fate",
		"component", compEventPush, "target", target, "event", event,
		"evidence", ev.Detail, "transport_claim", claim, "timed_out", timedOut)
	return mcp.NewToolResultText(fmt.Sprintf(
		"Pushed event %q to %q — NOT CONFIRMED (status: delivered_unconfirmed). This is an "+
			"unknown, not a failure: %s, and %s.\n\n"+
			"DO NOT re-push and do not retry with interrupt=true on this verdict — a re-push on a "+
			"payload that did land stacks a second copy in %[2]q's composer.\n"+
			"Decide it by reading %[2]q's own session records (jevons_transcript_read): a user "+
			"message carrying the payload, or a queue-operation enqueue/dequeue/remove/popAll or "+
			"queued_command attachment carrying it, means it was delivered. Absence at "+
			"user-message level alone does not mean it was lost.",
		event, target, why, evidenceDetail(ev)))
}

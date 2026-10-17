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
	"github.com/marcelocantos/jevons/internal/sendq"
)

// registerEventPushTools exposes 🎯T34 event-triggered push via MCP so
// generic event sources (scripts, CI hooks, other agents) do not
// hard-code delivery paths.
func (s *Server) registerEventPushTools() {
	s.addTool(
		mcp.NewTool("jevons_event_push",
			mcp.WithDescription("Push an event-driven message into a target participant (butler thread or fleet agent) so it acts next (🎯T34/T114/T111.2). One deliver path by name/id: rehydrates a stopped process; returns a typed error if undeliverable. Never fails with 'no thread' when a registered agent exists. Use when a dependency landed, CI went green, a worker finished, a timer elapsed, etc. — not for owner chat (use jevons_thread_direct)."),
			mcp.WithString("target", mcp.Required(), mcp.Description("Thread ID or fleet agent name to push into")),
			mcp.WithString("event", mcp.Required(), mcp.Description("Event source/kind, e.g. ci, worker-finished, timer, dependency")),
			mcp.WithString("text", mcp.Required(), mcp.Description("What the agent should do next / what happened")),
			mcp.WithString("directive_family", mcp.Description("Optional explicit operation scope for supersession; requires directive_kind")),
			mcp.WithString("directive_kind", mcp.Description("authorization or hold; only a later hold cancels pending authorization in the same recipient/family")),
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
	directive, derr := parseSendDirective(args)
	if derr != nil {
		return mcp.NewToolResultError(derr.Error()), nil
	}
	// A direct hold must cancel older pending authorizations before delivery.
	// No prose classifier: callers must opt into a typed family and kind.
	if directive != nil && directive.Kind == "hold" && s.flightState(target) != FlightInFlight {
		_, _, removed, err := s.sendQueue().ApplyDirective(target, wire, *directive, false, time.Now())
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		life["superseded"] = len(removed)
	}

	// 🎯T428. This tool is the one notification door that does NOT pass through
	// deliverToOverseer — butler.PushEvent reaches the agent process directly —
	// so the guard is applied here against the same ledger and the same wire
	// bytes, and a coach judgment that arrives once by send and once by push
	// still counts as one batch. Non-overseer targets are unaffected: this
	// target is about the channel the whole fleet reports into.
	var ticket notifyReplayTicket
	if s.isOverseerAgent(target) {
		if held, reason := s.batchAlreadyHeld(target, wire); held {
			life["suppressed_replay"] = reason
			s.logLifecycle(compEventPush, "push", "ok", life)
			return mcp.NewToolResultText(describeHeldBatch(target, reason)), nil
		}
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

	// 🎯T620: a turn already known in flight is the same busy class as
	// jevons_agent_send — hold the event wire on sendq for the next turn
	// boundary. Hitting the provider here is how 2026-09-02 came back as
	// `grok acp: prompt already in flight` with the payload nowhere.
	if s.flightState(target) == FlightInFlight {
		_ = s.EnsureAgentEventsWired(target)
		return s.queueBusyDirectiveEventPush(target, event, wire, life, ticket, directive)
	}

	reply, err, timedOut := s.pushEventBounded(target, event, text)
	switch {
	case isPromptInFlight(err):
		// Flight state was stale or never written; the provider still said
		// busy. Queue the same wire the idle path would have sent.
		return s.queueBusyDirectiveEventPush(target, event, wire, life, ticket, directive)
	case timedOut, err != nil && !ClassifySendError(err).DisprovesDelivery():
		// The push did not come back with an answer inside the window, or came
		// back with an error that is a failure to observe rather than evidence
		// the payload was refused. A transcript hit is not a receipt, so the
		// answer stays unconfirmed and is not recorded as delivered.
		life["reply_pending"] = true
		if err != nil {
			life["err"] = err.Error()
		}
		ticket.Settle(false)
		res := s.reportPushDeliveryUnjudged(target, event, err, timedOut)
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
		return s.toolFailure("event_push", target, err), nil
	}

	// The receiver answered. The reply is what the caller asked for. It is
	// assistant prose, not a structured receipt, so the batch stays
	// unconfirmed and is not recorded as delivered.
	ticket.Settle(false)
	if class, ownerMsg, ok := agenterr.ReplyFailure(reply); ok {
		life["failure_class"] = class.String()
		s.logLifecycle(compEventPush, "push", "error", life)
		s.logProviderFailure("event_push", target, class, reply)
		return mcp.NewToolResultError(ownerMsg), nil
	}
	s.ObserveProviderOK()
	s.logLifecycle(compEventPush, "push", "ok", life)
	return mcp.NewToolResultText(fmt.Sprintf("Pushed event %q to %q.\n\n%s", event, target, reply)), nil
}

// queueBusyDirectiveEventPush uses the same durable queue transaction for a
// typed busy event, including the hold that replaces pending authorizations.
func (s *Server) queueBusyDirectiveEventPush(target, event, wire string, life map[string]any, ticket notifyReplayTicket, directive *sendq.Directive) (*mcp.CallToolResult, error) {
	if directive == nil {
		return s.queueBusyEventPush(target, event, wire, life, ticket)
	}
	_, n, removed, err := s.sendQueue().ApplyDirective(target, wire, *directive, true, time.Now())
	if err != nil {
		ticket.Abandon()
		return mcp.NewToolResultError(fmt.Sprintf("event push NOT QUEUED: %v", err)), nil
	}
	ticket.Settle(true)
	life["status"], life["queued"], life["superseded"] = "queued", n, len(removed)
	s.logLifecycle(compEventPush, "push", "ok", life)
	return mcp.NewToolResultText(fmt.Sprintf("queued: %q event %q held (%d pending); superseded %d authorization(s)", target, event, n, len(removed))), nil
}

// queueBusyEventPush holds an event-push on sendq for the next turn boundary
// (🎯T620). Same busy class as jevons_agent_send: queued, never a delivery
// error whose text is grok acp: prompt already in flight. The queued bytes
// are the FormatEventPush wire, so drain delivers what idle PushEvent would
// have sent.
func (s *Server) queueBusyEventPush(target, event, wire string, life map[string]any, ticket notifyReplayTicket) (*mcp.CallToolResult, error) {
	n, qerr := s.enqueueAgentSend(target, wire)
	if qerr != nil {
		ticket.Abandon()
		life["err"] = qerr.Error()
		s.logLifecycle(compEventPush, "push", "error", life)
		return s.toolFailure("event_push", target, undeliverableQueueError(target, qerr)), nil
	}
	// The batch is held (sendq), so a same-bytes retry must not stack a
	// second copy the way a toolFailure retry did on 2026-09-02.
	ticket.Settle(true)
	life["status"] = "queued"
	life["queued"] = n
	s.logLifecycle(compEventPush, "push", "ok", life)
	return mcp.NewToolResultText(fmt.Sprintf(
		"queued: %q has a turn in flight; event %q held (%d pending) for delivery when the current turn ends — "+
			"held by the daemon, not pasted into the agent's composer. Not a delivery error.",
		target, event, n)), nil
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
// see. A transcript hit is not a receipt, so the answer is unconfirmed either
// way. Never an MCP error result: an error result is read as "this did not
// happen", and a re-push on that reading stacks a second copy.
func (s *Server) reportPushDeliveryUnjudged(target, event string, pushErr error, timedOut bool) *mcp.CallToolResult {
	claim := describeTransportClaim(pushErr)
	why := "the reply did not arrive inside this daemon's push window"
	if !timedOut {
		why = fmt.Sprintf("the push transport reported %q, which is a statement about the CALL and not about %q", claim, target)
	}
	slog.Warn("event push unconfirmed; no structured receipt",
		"component", compEventPush, "target", target, "event", event,
		"transport_claim", claim, "timed_out", timedOut)
	return mcp.NewToolResultText(fmt.Sprintf(
		"Pushed event %q to %q — NOT CONFIRMED (status: delivered_unconfirmed). This is an "+
			"unknown, not a failure: %s. No structured receipt settled this push, and a "+
			"transcript search is not a receipt.\n\n"+
			"DO NOT re-push and do not retry with interrupt=true on this verdict — a re-push on a "+
			"payload that did land stacks a second copy in %[2]q's composer.",
		event, target, why))
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/mark3labs/mcp-go/mcp"
)

// 🎯T792 / 🎯T758 — a jevons_agent_start launch belongs to the daemon, not to
// the caller's request. The 30s tools/call deadline (🎯T254.5.1) answers the
// caller; it must not cancel a launch that is still making progress, or a
// slow host leaves a registered, stopped, unbriefed seat that the daemon
// then reaps (unbriefed_seat, 🎯T433). The launch runs on a context
// detached from the request (launchAgentBounded still bounds it), and a
// retry under the same name joins the in-flight launch instead of
// launching twice.
//
// 🎯T758 names the outcome explicitly so a transport error or client
// timeout is never a bare socket/timeout that the caller reads as
// "not started, retry": outcome=started | already-starting | not-started.

const (
	// StartOutcomeStarted is a launch that took effect (or an already-running
	// same-name no-op that reports the existing seat).
	StartOutcomeStarted = "started"
	// StartOutcomeAlreadyStarting is a launch still in flight past the
	// caller's deadline; the daemon continues it.
	StartOutcomeAlreadyStarting = "already-starting"
	// StartOutcomeNotStarted is a refusal or failure that left no running seat.
	StartOutcomeNotStarted = "not-started"
)

type startResult struct {
	res *mcp.CallToolResult
	err error
}

type startFlight struct {
	done chan struct{}
	out  startResult
}

type startFlights struct {
	mu sync.Mutex
	m  map[string]*startFlight
}

func startPendingText(name string) string {
	return fmt.Sprintf(
		"outcome=%s: start of %q is still running past this call's deadline and CONTINUES in the daemon (🎯T792/🎯T758): "+
			"the launch and opening brief are not cancelled. Observe it with jevons_agent_list "+
			"(running, prompt_delivered) — do NOT re-send the brief; a retry of jevons_agent_start "+
			"under the same name joins the in-flight launch.",
		StartOutcomeAlreadyStarting, name)
}

// withStartOutcome prefixes a start failure that did not leave a running seat.
func withStartOutcome(outcome, msg string) string {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return "outcome=" + outcome
	}
	if strings.Contains(msg, "outcome=") {
		return msg
	}
	return "outcome=" + outcome + ": " + msg
}

// detachStart wraps the jevons_agent_start handler: launch on a context
// detached from the request, dedupe in-flight launches by name.
func (s *Server) detachStart(h toolHandler) toolHandler {
	return s.detachFlight(func(req mcp.CallToolRequest) string {
		return strings.TrimSpace(argString(req, "name"))
	}, startPendingText, h)
}

type toolHandler = func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)

func argString(req mcp.CallToolRequest, key string) string {
	v, _ := req.GetArguments()[key].(string)
	return v
}

// detachFlight is the 🎯T792 mechanism, shared with 🎯T790 (model switch): run h on
// a context detached from the request, join a same-key call to the in-flight
// one, and answer a caller whose deadline passes with pending(key).
func (s *Server) detachFlight(key func(mcp.CallToolRequest) string, pending func(string) string, h toolHandler) toolHandler {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if ctx == nil {
			ctx = context.Background()
		}
		name := key(req)
		fl := &s.startFlights
		fl.mu.Lock()
		if fl.m == nil {
			fl.m = map[string]*startFlight{}
		}
		f, joined := fl.m[name]
		if !joined {
			f = &startFlight{done: make(chan struct{})}
			fl.m[name] = f
			detached := context.WithoutCancel(ctx)
			go func() {
				res, err := h(detached, req)
				f.out = startResult{res, err}
				fl.mu.Lock()
				delete(fl.m, name)
				fl.mu.Unlock()
				close(f.done)
			}()
		}
		fl.mu.Unlock()
		select {
		case <-f.done:
			return f.out.res, f.out.err
		case <-ctx.Done():
			return mcp.NewToolResultError(pending(name)), nil
		}
	}
}

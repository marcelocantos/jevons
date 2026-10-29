// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"github.com/marcelocantos/jevons/internal/seatstate"
	"strings"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/thread"
)

// ModelSwitcher is the fleet capability behind jevons_agent_switch_model:
// move a seat to another provider or model in place, keeping its
// conversation (claudia 🎯T195). Implemented by *fleet.Claudia; an optional
// dependency so the tool is simply unavailable when it is not wired, rather
// than half-working.
type ModelSwitcher interface {
	SwitchModel(name string, to claudia.Provider, model string, force bool) (claudia.SeatSwitch, error)
	Launch(t *thread.Thread) error
}

// SetModelSwitcher wires the in-place model switch.
func (s *Server) SetModelSwitcher(m ModelSwitcher) { s.switcher = m }

func (s *Server) registerAgentSwitchModel() {
	s.addTool(
		mcp.NewTool("jevons_agent_switch_model",
			mcp.WithDescription(
				"Move an existing agent to another provider or model in place (claudia 🎯T195). "+
					"The seat keeps its name, session and whole conversation: nothing is summarised and no "+
					"successor is started. A running seat switches between turns; a stopped seat's next start "+
					"resumes on the new model. A conversation too large for the new model is compacted first "+
					"by the model that wrote it. "+
					"Use this instead of stopping and re-creating an agent, which starts it with no history at all. "+
					"A CONTEXT BLOW is not a reason to change provider (🎯T561): while the seat's own provider still "+
					"has weekly remaining, a cross-provider switch is refused unless owner_asked=true."),
			mcp.WithString("name", mcp.Required(),
				mcp.Description("Agent to switch (registry name, e.g. jevons-po)")),
			mcp.WithString("provider",
				mcp.Description("Destination plan (claude, codex, grok, cursor); empty keeps the seat's own")),
			mcp.WithString("model",
				mcp.Description("Destination model (e.g. claude-sonnet-5); empty = the destination plan's standard model. Visible afterwards in jevons_agent_list")),
			mcp.WithBoolean("force",
				mcp.Description("Interrupt a turn in flight to switch now; without it a running turn refuses the switch")),
			mcp.WithBoolean("owner_asked",
				mcp.Description("The owner explicitly asked for this cross-provider move (🎯T561); without it a seat whose provider still has weekly remaining is kept on that provider")),
		),
		s.handleAgentSwitchModel,
	)
}

func (s *Server) handleAgentSwitchModel(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name := strings.TrimSpace(argString(req, "name"))
	target := strings.TrimSpace(argString(req, "provider"))
	model := strings.TrimSpace(argString(req, "model"))
	args := req.GetArguments()
	force, _ := args["force"].(bool)
	ownerAsked, _ := args["owner_asked"].(bool)
	if name == "" {
		return mcp.NewToolResultError("name is required"), nil
	}
	if target == "" && model == "" {
		return mcp.NewToolResultError("provider or model is required"), nil
	}
	if target != "" {
		if msg := s.refuseContextRemintSwitch(name, target, ownerAsked); msg != "" {
			return mcp.NewToolResultError(msg), nil
		}
	}
	if s.switcher == nil {
		return mcp.NewToolResultError("model switching is not available (no fleet switcher wired)"), nil
	}
	// When the overseer itself makes this call, its turn is in flight until
	// this returns, so a switch from here either refuses or, with force,
	// interrupts the turn that asked. The owner drives it instead. Any other
	// caller (an owner-session client, a PO) is not that turn: a running
	// overseer turn still refuses below unless force is set. Refusing every
	// caller told an owner-session curl it was the overseer (2026-10-27).
	if s.registry != nil && mcpOriginFrom(ctx).Seat == name {
		if def := s.registry.Def(name); def != nil && def.Purpose == claudia.PurposeOverseer {
			return mcp.NewToolResultError(fmt.Sprintf(
				"%q is the overseer and is answering this call, so it cannot switch mid-turn from here. "+
					"The owner switches it from the fleet tree (POST /api/agents/switch-model).", name)), nil
		}
	}
	sw, err := s.switcher.SwitchModel(name, claudia.Provider(target), model, force)
	if errors.Is(err, claudia.ErrSeatNotAttached) && s.registry != nil {
		// After a broker restart an idle seat stays unattached until
		// something sends to it; the send path adopts on demand, and an
		// owner's switch now does too instead of asking them to attach it
		// (six product owners on 2026-10-28).
		if _, aerr := s.registry.Adopt(name); aerr == nil {
			seatstate.ObserveRegistrySeat(s.registry, name)
			s.EnsureAgentEventsWired(name)
			sw, err = s.switcher.SwitchModel(name, claudia.Provider(target), model, force)
		}
	}
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if !sw.Changed {
		return mcp.NewToolResultText(fmt.Sprintf("%s is already on %s/%s.", name, sw.To, sw.Model)), nil
	}
	how := "in place; its conversation continues on the new model"
	if !sw.Live {
		how = "while stopped; its next start resumes the same conversation on the new model"
	}
	return mcp.NewToolResultText(fmt.Sprintf("%s switched %s/%s → %s/%s %s.",
		name, sw.From, sw.FromModel, sw.To, sw.Model, how)), nil
}

// 🎯T790 — a switch belongs to the daemon, not to the caller's request. A
// switch that first compacts a long conversation can take minutes; the 30s
// spawn-class deadline (🎯T254.5.1) would answer "timed out" while the
// switch went on, and the caller could not tell whether the agent had
// moved. The switch runs through the 🎯T792 detached-flight mechanism, and
// its outcome is retained so a retry (or a late reader) reads what happened
// instead of starting a second switch.

const switchOutcomeTTL = 15 * time.Minute

type switchOutcomes struct {
	mu sync.Mutex
	m  map[string]switchOutcome
}

type switchOutcome struct {
	res *mcp.CallToolResult
	at  time.Time
}

func switchKey(req mcp.CallToolRequest) string {
	return strings.TrimSpace(argString(req, "name")) + "→" + strings.TrimSpace(argString(req, "provider")) +
		"/" + strings.TrimSpace(argString(req, "model"))
}

func switchPendingText(key string) string {
	name, to, _ := strings.Cut(key, "→")
	return fmt.Sprintf(
		"switch of %q → %s is still running past this call's deadline and CONTINUES in the daemon (🎯T790): "+
			"it is not cancelled. Read the outcome by repeating this exact call (it returns the recorded "+
			"result, or joins the switch if still running — it never starts a second one), or watch "+
			"jevons_agent_list for the row's provider and model.", name, to)
}

// detachSwitch wraps the jevons_agent_switch_model handler.
func (s *Server) detachSwitch(h toolHandler) toolHandler {
	inner := s.detachFlight(switchKey, switchPendingText, func(ctx2 context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		res, err := h(ctx2, req)
		if err == nil && res != nil {
			o := &s.switchOutcomes
			o.mu.Lock()
			if o.m == nil {
				o.m = map[string]switchOutcome{}
			}
			o.m[switchKey(req)] = switchOutcome{res: res, at: time.Now()}
			o.mu.Unlock()
		}
		return res, err
	})
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		key := switchKey(req)
		o := &s.switchOutcomes
		o.mu.Lock()
		got, ok := o.m[key]
		if ok {
			delete(o.m, key) // read once
		}
		o.mu.Unlock()
		if ok && time.Since(got.at) < switchOutcomeTTL {
			return got.res, nil
		}
		res, err := inner(ctx, req)
		if res != nil && !(res.IsError && firstText(res) == switchPendingText(key)) {
			// The caller got the real outcome; nothing is left to read later.
			o.mu.Lock()
			delete(o.m, key)
			o.mu.Unlock()
		}
		return res, err
	}
}

func firstText(r *mcp.CallToolResult) string {
	if r == nil || len(r.Content) == 0 {
		return ""
	}
	if tc, ok := r.Content[0].(mcp.TextContent); ok {
		return tc.Text
	}
	return ""
}

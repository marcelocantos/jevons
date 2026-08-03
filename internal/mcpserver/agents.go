// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/cli"
)

// NotifyFunc injects a text message into the Jevon overseer's PTY input.
type NotifyFunc func(text string)

// SetRegistry attaches the agent registry to the MCP server and
// registers agent management tools.
func (s *Server) SetRegistry(registry *claudia.Registry) {
	s.registry = registry

	s.mcpSrv.AddTool(
		mcp.NewTool("jevons_agent_list",
			mcp.WithDescription("List all registered agents and their status (running/stopped)."),
		),
		s.handleAgentList,
	)

	s.mcpSrv.AddTool(
		mcp.NewTool("jevons_agent_start",
			mcp.WithDescription("Start a persistent fleet agent in a repo/directory (claudia backend: default from config/env, usually Grok). Creates and registers it if new. Records fleet lineage (parent) so only ancestors can later kill descendants. Purpose defaults to work (implementation agent); use purpose=aside for side-chat participants (🎯T114). Optional provider selects the claudia backend ad hoc (🎯T148). Optional target_id binds the agent to a bullseye frontier target for RHS engagement overlay (🎯T198) — never rely on name parsing."),
			mcp.WithString("name", mcp.Required(), mcp.Description("Unique agent name (free-form; hierarchical target ids keep literal dots — e.g. 'jv-t27.2-config', not digit-squash 'jv-t272-config'; 🎯T197)")),
			mcp.WithString("workdir", mcp.Required(), mcp.Description("Working directory for the agent (absolute or ~-relative repo path)")),
			mcp.WithString("model", mcp.Description("Model override (e.g. 'grok-4'; empty = provider default)")),
			mcp.WithString("provider", mcp.Description("Agent backend override (claudia provider id: grok, claude, …). Empty = keep stored provider on resume, else daemon default (config/env/grok). 🎯T148.")),
			mcp.WithString("actor", mcp.Description("Your agent name (who is starting the child). Used as default parent for lineage.")),
			mcp.WithString("parent", mcp.Description("Parent agent name for lineage (default: actor, else overseer). Required for correct kill authorization.")),
			mcp.WithString("purpose", mcp.Description("Fleet purpose: work (default), aside, or overseer (🎯T114). UI: work + aside → RHS fleet tree (asides 💡 chrome; 🎯T136); overseer uses main chat.")),
			mcp.WithString("target_id", mcp.Description("Optional bullseye target id this agent is engaged on (e.g. T10.2). Written to registry as target_id for Frontier engagement overlay (🎯T198). Empty = not mission-bound.")),
		),
		s.handleAgentStart,
	)

	s.mcpSrv.AddTool(
		mcp.NewTool("jevons_agent_send",
			mcp.WithDescription("Send a message to a running agent. Returns immediately — the agent processes asynchronously. When the agent responds, you will receive a notification with the response text. If a prompt is already in flight, the message is queued for after the turn (not a dead-end). Pass interrupt=true to cancel the in-flight turn and send immediately (🎯T111.1 stuck recovery)."),
			mcp.WithString("name", mcp.Required(), mcp.Description("Agent name")),
			mcp.WithString("text", mcp.Required(), mcp.Description("Message to send")),
			mcp.WithBoolean("interrupt", mcp.Description("If true and a prompt is in flight, interrupt that turn then send (stuck recovery without kill). Default false = queue for after the turn.")),
		),
		s.handleAgentSend,
	)

	s.mcpSrv.AddTool(
		mcp.NewTool("jevons_agent_stop",
			mcp.WithDescription("Stop a running agent process only. The agent stays registered and can be started again (resume). Not the same as kill."),
			mcp.WithString("name", mcp.Required(), mcp.Description("Agent name")),
		),
		s.handleAgentStop,
	)

	s.mcpSrv.AddTool(
		mcp.NewTool("jevons_agent_kill",
			mcp.WithDescription("Kill an agent and its descendant subtree: stop processes and remove from the fleet registry. Distinct from stop (pause only). Authorization: only an ancestor of the target (or the overseer) may kill; peers and reverse lineage are denied. Pass actor=your agent name. Cannot kill the overseer. Cross-tree kill via common-ancestor escalation is not direct (deferred)."),
			mcp.WithString("name", mcp.Required(), mcp.Description("Agent name to kill and deregister (subtree included)")),
			mcp.WithString("actor", mcp.Required(), mcp.Description("Your agent name (who is requesting the kill). Overseer uses the overseer name (usually 'jevons').")),
		),
		s.handleAgentKill,
	)
}

// SetNotify sets the callback for injecting notifications into the
// Jevon overseer (e.g. agent responses).
func (s *Server) SetNotify(fn NotifyFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notifyJevon = fn
}

// SetAgentEventHook registers a sink for every fleet-worker ACP event
// (progress, assistant, terminal stop). Used to drive RHS status chrome
// without owner/overseer polling (🎯T118). Nil clears the hook.
func (s *Server) SetAgentEventHook(fn func(name string, ev claudia.Event)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.agentEventHook = fn
}

func (s *Server) handleAgentList(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// 🎯T85: proactive silent-death sweep; surface recovery to the caller
	// (and overseer notify), not only logs.
	reps := SweepDeadAgents(s.registry, s.overseerName())
	if len(reps) > 0 {
		line := FormatDeadAgentReport(reps)
		slog.Info(line)
		s.notifyFleetHealth(line)
	}
	defs := s.registry.List()
	if len(defs) == 0 {
		return mcp.NewToolResultText(PrependFleetHealth("No agents registered.", reps)), nil
	}

	var b strings.Builder
	for _, d := range defs {
		proc := s.registry.Get(d.Name)
		status := "stopped"
		if proc != nil && proc.Alive() {
			status = "running"
		}
		parent := d.Parent
		if parent == "" {
			parent = "-"
		}
		purpose := d.Purpose
		if purpose == "" {
			purpose = claudia.PurposeWork
		}
		fmt.Fprintf(&b, "%-20s %-10s purpose=%-8s parent=%-12s %s (session: %s)\n",
			d.Name, status, purpose, parent, d.WorkDir, sessionDisplay(d.SessionID))
	}
	// 🎯T111.4 thin surface: PO/boss with zero children while multi-slice
	// missions should have fan-out — visible without only RHS eyeballing.
	if hints := FormatFanOutHints(s.registry, s.overseerName()); hints != "" {
		b.WriteString("\n")
		b.WriteString(hints)
	}
	return mcp.NewToolResultText(PrependFleetHealth(b.String(), reps)), nil
}

// notifyFleetHealth injects a fleet outage/recovery note into the overseer
// when a notify hook is wired (live daemon). Tests may leave notify nil.
func (s *Server) notifyFleetHealth(line string) {
	if s == nil || line == "" {
		return
	}
	s.mu.Lock()
	fn := s.notifyJevon
	s.mu.Unlock()
	if fn == nil {
		return
	}
	// Distinct prefix so activity strip / overseer can treat as system note.
	fn("[Fleet health] " + line)
}

func (s *Server) handleAgentStart(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	name, _ := args["name"].(string)
	workdir, _ := args["workdir"].(string)
	model, _ := args["model"].(string)
	providerArg, _ := args["provider"].(string)
	actor, _ := args["actor"].(string)
	parent, _ := args["parent"].(string)
	purpose, _ := args["purpose"].(string)
	targetID, _ := args["target_id"].(string)
	// Also accept mission / bullseye_target aliases (same field).
	if targetID == "" {
		if v, ok := args["mission"].(string); ok {
			targetID = v
		}
	}
	if targetID == "" {
		if v, ok := args["bullseye_target"].(string); ok {
			targetID = v
		}
	}
	targetID = normalizeAgentTargetID(targetID)

	life := map[string]any{"name": name, "workdir": workdir}

	if name == "" || workdir == "" {
		s.logLifecycle(compAgentLifecycle, "start", "error", map[string]any{
			"name": name, "err": "name and workdir are required",
		})
		return mcp.NewToolResultError("name and workdir are required"), nil
	}

	// Budget clamp: both new spawn and re-launch of a pause/kill-clamped
	// or spawn-halted agent must be refused before EnsureAgent/Launch.
	if blocked := s.checkSpawnAllowed(); blocked != nil {
		s.logLifecycle(compAgentLifecycle, "start", "error", map[string]any{
			"name": name, "err": "spawn_halted",
		})
		return blocked, nil
	}
	if blocked := s.checkResumeAllowed(name); blocked != nil {
		s.logLifecycle(compAgentLifecycle, "start", "error", map[string]any{
			"name": name, "err": "resume_halted",
		})
		return blocked, nil
	}

	// Expand ~ in workdir.
	if strings.HasPrefix(workdir, "~/") {
		home, _ := os.UserHomeDir()
		workdir = home + workdir[1:]
	}
	life["workdir"] = workdir

	// Lineage: parent defaults to actor, else overseer root.
	if parent == "" {
		parent = actor
	}
	if parent == "" {
		parent = s.overseerName()
	}
	life["parent"] = parent
	if parent == name {
		s.logLifecycle(compAgentLifecycle, "start", "error", map[string]any{
			"name": name, "parent": parent, "err": "parent_equals_name",
		})
		return mcp.NewToolResultError("parent cannot equal agent name"), nil
	}

	// 🎯T114: purpose defaults to work for agent_start; aside is explicit.
	purpose = strings.TrimSpace(purpose)
	switch purpose {
	case "", claudia.PurposeWork:
		purpose = claudia.PurposeWork
	case claudia.PurposeAside, claudia.PurposeOverseer:
		// allowed
	default:
		s.logLifecycle(compAgentLifecycle, "start", "error", map[string]any{
			"name": name, "purpose": purpose, "err": "invalid_purpose",
		})
		return mcp.NewToolResultError(fmt.Sprintf("purpose %q invalid; use work, aside, or overseer", purpose)), nil
	}
	life["purpose"] = purpose

	existed := s.registry.Def(name) != nil
	life["existed"] = existed
	def, err := s.registry.EnsureAgentWithParent(name, workdir, model, parent, true)
	if err != nil {
		life["err"] = err.Error()
		s.logLifecycle(compAgentLifecycle, "start", "error", life)
		return mcp.NewToolResultError(fmt.Sprintf("register failed: %v", err)), nil
	}
	// Refresh copy after Ensure (Register may have stored a different pointer).
	if d := s.registry.Def(name); d != nil {
		def = d
	}
	// 🎯T148: ad hoc provider override wins; else keep stored; else default.
	// Never unconditionally force Grok on resume.
	def.Provider = cli.SelectAgentProvider(providerArg, def.Provider, s.resolvedDefaultProvider())
	// Set parent only when minting or when legacy entry has empty parent.
	if !existed || def.Parent == "" {
		def.Parent = parent
	}
	// Purpose: set on mint; backfill empty purpose on re-start.
	if !existed || def.Purpose == "" {
		def.Purpose = purpose
	}
	// 🎯T198: target_id on mint, or when caller supplies a non-empty id (rebind).
	if targetID != "" {
		def.TargetID = targetID
	}
	if err := s.registry.Register(*def); err != nil {
		life["err"] = err.Error()
		s.logLifecycle(compAgentLifecycle, "start", "error", life)
		return mcp.NewToolResultError(fmt.Sprintf("register failed: %v", err)), nil
	}

	proc, err := s.registry.Launch(name)
	if err != nil {
		life["err"] = err.Error()
		life["session_id"] = sessionDisplay(def.SessionID)
		s.logLifecycle(compAgentLifecycle, "start", "error", life)
		return mcp.NewToolResultError(fmt.Sprintf("start failed: %v", err)), nil
	}

	// Wire events: broadcast to web UI and notify Jevon on agent responses.
	s.wireAgentEvents(name, proc)

	life["session_id"] = sessionDisplay(def.SessionID)
	life["purpose"] = def.Purpose
	life["parent"] = def.Parent
	life["provider"] = string(def.Provider)
	if def.TargetID != "" {
		life["target_id"] = def.TargetID
	}
	s.logLifecycle(compAgentLifecycle, "start", "ok", life)

	msg := fmt.Sprintf(
		"Agent %q started (session: %s, workdir: %s, parent: %s, purpose: %s, provider: %s",
		name, sessionDisplay(def.SessionID), def.WorkDir, def.Parent, def.Purpose, def.Provider)
	if def.TargetID != "" {
		msg += fmt.Sprintf(", target_id: %s", def.TargetID)
	}
	msg += ")"
	return mcp.NewToolResultText(msg), nil
}

// normalizeAgentTargetID strips 🎯 and whitespace for registry TargetID (🎯T198).
func normalizeAgentTargetID(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	s = strings.TrimPrefix(s, "🎯")
	return strings.TrimSpace(s)
}

func (s *Server) handleAgentSend(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	name, _ := args["name"].(string)
	text, _ := args["text"].(string)
	interrupt, _ := args["interrupt"].(bool)

	if name == "" || text == "" {
		return mcp.NewToolResultError("name and text are required"), nil
	}

	// 🎯T104 under fan-out: first send carries standing local-delivery brief.
	s.mu.Lock()
	if s.fleetBriefed == nil {
		s.fleetBriefed = map[string]bool{}
	}
	text, injected := EnsureFleetBrief(s.fleetBriefed, name, text)
	s.mu.Unlock()
	if injected {
		slog.Info("fleet standing brief injected on first send", "name", name)
	}

	// 🎯T111.1: rehydrate + send, or queue/interrupt when prompt in flight.
	result, err := s.sendToAgent(name, text, interrupt)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	msg := result.Message
	if injected {
		msg += " (standing fleet brief T104/T78 prepended on first send)"
	}
	return mcp.NewToolResultText(msg), nil
}

func (s *Server) handleAgentStop(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	name, _ := args["name"].(string)
	if name == "" {
		s.logLifecycle(compAgentLifecycle, "stop", "error", map[string]any{
			"err": "name is required",
		})
		return mcp.NewToolResultError("name is required"), nil
	}

	s.registry.Stop(name)
	s.logLifecycle(compAgentLifecycle, "stop", "ok", map[string]any{"name": name})
	return mcp.NewToolResultText(fmt.Sprintf("Agent %q stopped (still registered; start again to resume).", name)), nil
}

func (s *Server) handleAgentKill(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	name, _ := args["name"].(string)
	actor, _ := args["actor"].(string)
	life := map[string]any{"name": name, "actor": actor}
	if name == "" {
		s.logLifecycle(compAgentLifecycle, "kill", "error", map[string]any{
			"err": "name is required",
		})
		return mcp.NewToolResultError("name is required"), nil
	}
	if s.registry == nil {
		s.logLifecycle(compAgentLifecycle, "kill", "error", map[string]any{
			"name": name, "err": "registry unavailable",
		})
		return mcp.NewToolResultError("agent registry not available"), nil
	}
	// Default actor for the overseer only when identity is proven via session;
	// POs/bosses must always pass actor explicitly.
	if actor == "" && s.transcript != nil && s.transcript.GetID != nil {
		sid := s.transcript.GetID()
		for _, d := range s.registry.List() {
			if d.SessionID == sid {
				actor = d.Name
				break
			}
		}
		if actor == "" {
			actor = s.overseerName()
		}
		life["actor"] = actor
	}
	if err := canKill(s.registry, actor, name, s.isOverseerAgent); err != nil {
		life["err"] = err.Error()
		s.logLifecycle(compAgentLifecycle, "kill", "error", life)
		return mcp.NewToolResultError(err.Error()), nil
	}
	desc := s.registry.Descendants(name)
	if err := killSubtree(s.registry, name); err != nil {
		life["err"] = err.Error()
		s.logLifecycle(compAgentLifecycle, "kill", "error", life)
		return mcp.NewToolResultError(fmt.Sprintf("kill failed: %v", err)), nil
	}
	life["descendants"] = len(desc)
	s.logLifecycle(compAgentLifecycle, "kill", "ok", life)
	msg := fmt.Sprintf(
		"Agent %q killed by %q: process stopped and deregistered (will not auto-start; gone from agent list).",
		name, actor,
	)
	if len(desc) > 0 {
		msg += fmt.Sprintf(" Also killed %d descendant(s): %s.", len(desc), strings.Join(desc, ", "))
	}
	return mcp.NewToolResultText(msg), nil
}

// isOverseerAgent reports whether name is the owner-chat overseer.
// Prefer session-id match via transcript ops; fall back to the conventional
// overseer name used by default config.
func (s *Server) isOverseerAgent(name string) bool {
	if name == "" {
		return false
	}
	if s.transcript != nil && s.transcript.GetID != nil {
		sid := s.transcript.GetID()
		if sid != "" {
			if def := s.registry.Def(name); def != nil && def.SessionID == sid {
				return true
			}
		}
	}
	return name == s.overseerName()
}

func (s *Server) overseerName() string {
	// Conventional default; config overseer_name is almost always "jevons".
	return "jevons"
}

// wireAgentEvents sets up the event handler for an agent process.
// It broadcasts to the web UI and notifies Jevon when the agent
// produces a text response.
func (s *Server) wireAgentEvents(name string, proc *claudia.Agent) {
	proc.SubscribeEvents(s.agentEventSink(name))
}

// WireRunningAgents subscribes the completion-notify sink for every
// currently-running agent except the overseer. Agents auto-started at boot
// via registry.StartAll are launched WITHOUT going through handleAgentStart,
// so their events were never wired — a worker that existed before a restart
// (e.g. a resumed jevons-po) would finish its work but its reply would never
// reach the overseer. Call once after StartAll (🎯T61). The overseer is
// excluded because it gets its own event stream via Server.AttachOverseer.
func (s *Server) WireRunningAgents(overseerName string) {
	if s.registry == nil {
		return
	}
	for _, def := range s.registry.List() {
		if def.Name == overseerName {
			continue
		}
		if proc := s.registry.Get(def.Name); proc != nil && proc.Alive() {
			s.wireAgentEvents(def.Name, proc)
			slog.Info("wired completion-notify for auto-started agent", "agent", def.Name)
		}
	}
}

// agentEventSink returns the per-agent event handler: it broadcasts every
// event to the web UI, accumulates the turn's assistant text (across any
// mid-turn tool_use pauses), and notifies the overseer once the turn
// reaches a terminal stop.
//
// A Grok worker signals turn completion with a terminal *assistant* event
// (StopReason end_turn/stop_sequence/max_tokens — Event.IsTerminalStop),
// NOT a "system" event. The original code keyed the flush on a "system"
// event, a Claude-harness concept that Grok never emits, so worker replies
// accumulated but were never delivered — the overseer's "you will be
// notified when it responds" promise silently failed for every worker
// (🎯T61). Keying on IsTerminalStop is what actually delivers the reply.
func (s *Server) agentEventSink(name string) func(claudia.Event) {
	var mu sync.Mutex
	var responseText strings.Builder

	return func(ev claudia.Event) {
		// Broadcast raw event to web UI activity feed.
		s.broadcastAgentEvent(name, ev)

		mu.Lock()
		defer mu.Unlock()

		// Accumulate assistant text first so a terminal event that also
		// carries its last content block is included before we flush.
		if ev.Type == "assistant" && ev.Text != "" {
			responseText.WriteString(ev.Text)
		}
		// tool_use pauses are mid-turn (the worker will continue after tool
		// results); only a terminal stop ends the turn and delivers.
		if ev.IsTerminalStop() {
			text := responseText.String()
			responseText.Reset()
			if text != "" {
				// Notify overseer first so the done report is delivered before
				// the worker leaves the registry (🎯T165).
				s.notify(name, text)
			}
			// 🎯T111.1: deliver any nudges queued while the prompt was in flight.
			s.drainAgentSendQueue(name)
			// 🎯T165: finished work agents auto stop+Remove (not persona-only).
			s.maybeReapDoneWorkAgent(name, text)
		}
	}
}

// notify injects an agent response notification into Jevon's PTY.
func (s *Server) notify(agentName, text string) {
	s.mu.Lock()
	fn := s.notifyJevon
	s.mu.Unlock()

	if fn == nil {
		slog.Warn("agent response but no notify function set", "agent", agentName)
		return
	}

	// Truncate very long responses for the notification.
	if len(text) > 2000 {
		text = text[:1997] + "..."
	}

	msg := fmt.Sprintf("[Agent %s responded]\n%s", agentName, text)
	slog.Info("notifying jevon", "agent", agentName, "len", len(text))
	fn(msg)
}

// broadcastAgentEvent fans a worker event to the optional progress/UI hook
// (🎯T118). Previously a no-op stub; the HTTP server wires ObserveAgentProgress
// + agents_changed so fleet rows update without poll.
// Also feeds the idle-nudge activity tracker (🎯T207) when wired.
func (s *Server) broadcastAgentEvent(name string, ev claudia.Event) {
	s.mu.Lock()
	hook := s.agentEventHook
	tracker := s.idleActivity
	s.mu.Unlock()
	if tracker != nil {
		tracker.Observe(name, ev)
	}
	if hook != nil {
		hook(name, ev)
	}
}

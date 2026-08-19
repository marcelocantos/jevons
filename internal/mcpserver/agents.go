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

	"github.com/marcelocantos/jevons/internal/agentreport"
	"github.com/marcelocantos/jevons/internal/cli"
	"github.com/marcelocantos/jevons/internal/cost"
	"github.com/marcelocantos/jevons/internal/fleet"
	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/fleetlog"
	"github.com/marcelocantos/jevons/internal/gate"
	"github.com/marcelocantos/jevons/internal/mcpattach"
	"github.com/marcelocantos/jevons/internal/targetfile"
)

// prefixRehydrate puts the lost-session account in front of whatever
// agent_start would otherwise have said. It leads because it changes
// what the caller must do next: the agent is running, but blank, and
// the brief has to be re-sent (🎯T313).
func prefixRehydrate(rehydrated, msg string) string {
	if rehydrated == "" {
		return msg
	}
	return rehydrated + "\n\n" + msg
}

// NotifyFunc injects a text message into the Jevon overseer's PTY input.
type NotifyFunc func(text string)

// SetRegistry attaches the agent registry to the MCP server and
// registers agent management tools.
func (s *Server) SetRegistry(registry *claudia.Registry) {
	s.registry = registry

	s.mcpSrv.AddTool(
		mcp.NewTool("jevons_agent_list",
			mcp.WithDescription("List all registered agents and their status (running/stopped). Also lists finished-and-reaped names as recoverable addresses (🎯T401) so a never-existed name is distinguishable from one the product auto-deregistered — learn the address is closed BEFORE composing gate feedback into a void."),
		),
		s.handleAgentList,
	)

	s.mcpSrv.AddTool(
		mcp.NewTool("jevons_agent_start",
			mcp.WithDescription("Start a persistent fleet agent in a repo/directory (claudia backend: default from config/env, usually Grok). Creates and registers it if new. Records fleet lineage (parent) so only ancestors can later kill descendants. Purpose defaults to work (implementation agent); use purpose=aside for side-chat participants (🎯T114). Optional provider selects the claudia backend ad hoc (🎯T148). When provider is omitted on mint, the owner-visible default (config.yaml provider, then JEVONS_PROVIDER, then grok) wins — a leftover llm-portfolio.json or the compiled T325.2 seed must not silently override it (🎯T476). The start result cites which knob selected the provider. Optional target_id binds the agent to a bullseye frontier target for RHS engagement overlay (🎯T198) — never rely on name parsing. 🎯T222: refuses a second work agent when target_id is already engaged or the ledger status is set_aside/achieved (force_engage=true overrides)."),
			mcp.WithString("name", mcp.Required(), mcp.Description("Unique agent name (free-form; hierarchical target ids keep literal dots — e.g. 'jv-t27.2-config', not digit-squash 'jv-t272-config'; 🎯T197)")),
			mcp.WithString("workdir", mcp.Required(), mcp.Description("Working directory for the agent (absolute or ~-relative repo path)")),
			mcp.WithString("model", mcp.Description("Model override (e.g. 'grok-4'; empty = provider default)")),
			mcp.WithString("provider", mcp.Description("Agent backend override (claudia provider id: grok, claude, codex, …). Empty = keep stored provider on resume; on mint follow config.yaml / daemon default (🎯T476). The start result cites which knob won (explicit vs config vs leftover portfolio file). 🎯T148.")),
			mcp.WithString("task_type", mcp.Description("LLM portfolio task class (🎯T325.2): ceo, code_implement, mechanical, design_prose, ops_classify, journey_grok, ideation. Recorded for capacity tables and loser-knob citation; omitted provider on mint follows config.yaml (🎯T476), not this class. Empty = derive from purpose (work→code_implement, aside→ideation, overseer→ceo).")),
			mcp.WithString("actor", mcp.Description("Your agent name (who is starting the child). Used as default parent for lineage.")),
			mcp.WithString("parent", mcp.Description("Parent agent name for lineage (default: actor, else overseer). Required for correct kill authorization.")),
			mcp.WithString("purpose", mcp.Description("Fleet purpose: work (default), aside, or overseer (🎯T114). UI: work + aside → RHS fleet tree (asides 💡 chrome; 🎯T136); overseer uses main chat.")),
			mcp.WithString("target_id", mcp.Description("Optional bullseye target id this agent is engaged on (e.g. T10.2). Written to registry as target_id for Frontier engagement overlay (🎯T198). Empty = not mission-bound.")),
			mcp.WithBoolean("force_engage", mcp.Description("If true, allow a second work agent on an already-engaged or closed target (deliberate override 🎯T222). Default false.")),
			mcp.WithString("prompt", mcp.Description("Optional opening brief delivered after Launch. Confirmed turn-begin required — empty pane / unsubmitted paste fails the tool loudly (🎯T305). Prefer this over start-then-send for unattended spawns.")),
		),
		s.handleAgentStart,
	)

	s.mcpSrv.AddTool(
		mcp.NewTool("jevons_agent_send",
			mcp.WithDescription("Send a message to a running agent. Returns immediately — the agent processes asynchronously. When the agent responds, you will receive a notification with the response text. If a prompt is already in flight, the message is queued for after the turn (not a dead-end). Pass interrupt=true to cancel the in-flight turn and send immediately (🎯T111.1 stuck recovery). Pass actor=your agent name so lineage authorization runs against the real caller (🎯T321). An auto-reaped agent (🎯T401) is still a reachable address: the send reports reaped-with-reason, names the recovery call (jevons_agent_start under the same name), and holds the message in sendq — never a bare \"agent is not running\". A never-registered name remains an ordinary not-found."),
			mcp.WithString("name", mcp.Required(), mcp.Description("Agent name")),
			mcp.WithString("text", mcp.Required(), mcp.Description("Message to send")),
			mcp.WithString("actor", mcp.Required(), mcp.Description("Your agent name (who is sending). Overseer uses the overseer name (usually 'jevons'). Required so lineage denial is enforceable per-caller (🎯T321).")),
			mcp.WithBoolean("interrupt", mcp.Description("If true and a prompt is in flight, interrupt that turn then send (stuck recovery without kill). Default false = queue for after the turn.")),
		),
		s.handleAgentSend,
	)

	s.mcpSrv.AddTool(
		mcp.NewTool("jevons_agent_stop",
			mcp.WithDescription("Stop a running agent process and park it (🎯T414): the agent stays registered, and the park is a standing instruction that outlives the process — no delivery, restart, idle sweep or repair mission revives it until the park is lifted with jevons_fleet_intent state=working. Not the same as kill (which deregisters)."),
			mcp.WithString("name", mcp.Required(), mcp.Description("Agent name")),
			mcp.WithString("actor", mcp.Description("Your agent name (who is parking it). Default: the overseer.")),
			mcp.WithString("reason", mcp.Description("Why it is being stood down — shown to whoever later wonders why nothing is restarting it.")),
		),
		s.handleAgentStop,
	)

	s.mcpSrv.AddTool(
		mcp.NewTool("jevons_fleet_intent",
			mcp.WithDescription("Read or set the deliberate answer to \"should this agent be running?\" (🎯T414). Every fleet control — spawn, nudge, revive, repressure, repair mission, delivery start, worker-idle notification — reads this and declines when it says do not run, naming the intent. Observed process state alone never authorises a start. States: working, parked, blocked_provider, blocked_owner, reaped. Omit state to read the current intent; omit name to set the fleet-wide intent (a provider wall stands the whole fleet down)."),
			mcp.WithString("name", mcp.Description("Agent name. Omit to read everything, or (with state) to set the FLEET-WIDE intent.")),
			mcp.WithString("state", mcp.Description("working | parked | blocked_provider | blocked_owner | reaped. Omit to read.")),
			mcp.WithString("actor", mcp.Description("Who is deciding (owner, overseer, a product path). Recorded with the intent.")),
			mcp.WithString("reason", mcp.Description("Why — recorded so the cockpit can say what is holding an agent down, not merely that something is.")),
		),
		s.handleFleetIntent,
	)

	s.mcpSrv.AddTool(
		mcp.NewTool("jevons_agent_kill",
			mcp.WithDescription("Kill an agent and its descendant subtree: stop processes and remove from the fleet registry. Distinct from stop (pause only). Idempotent: if the agent is already not registered (e.g. auto-reaped after a done report), returns success without error. Authorization: only an ancestor of the target (or the overseer) may kill; peers and reverse lineage are denied. Pass actor=your agent name. Cannot kill the overseer. Cross-tree kill via common-ancestor escalation is not direct (deferred)."),
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
	reps := SweepDeadAgents(s.registry, s.overseerName(), s.fleetIntent())
	if len(reps) > 0 {
		line := FormatDeadAgentReport(reps)
		slog.Info(line)
		s.notifyFleetHealth(line)
	}
	// 🎯T459: reap fleet panes the registry does not know about before
	// we report the count the host is deciding against.
	s.SweepOrphanPanes()
	defs := s.registry.List()
	notices := s.RemovalAccount().Recent(0)
	if len(defs) == 0 {
		body := "No agents registered."
		if reaped := FormatReapedListSection(s.fleetIntent()); reaped != "" {
			body += "\n" + reaped
		}
		if extra := s.FormatHostCostLines(0); extra != "" {
			body += "\n" + extra
		}
		return mcp.NewToolResultText(fleetlog.PrependNotices(
			PrependFleetHealth(body, reps), notices)), nil
	}

	var b strings.Builder
	for _, d := range defs {
		proc := s.registry.Get(d.Name)
		alive := proc != nil && proc.Alive()
		// 🎯T305: zero-turn live seats are never_briefed, not running.
		// 🎯T444: and the seat's own session records break the tie, because
		// both of the other inputs go stale across a backend re-mint.
		status := s.agentPhase(d, alive)
		parent := d.Parent
		if parent == "" {
			parent = "-"
		}
		purpose := d.Purpose
		if purpose == "" {
			purpose = claudia.PurposeWork
		}
		fmt.Fprintf(&b, "%-20s %-14s purpose=%-8s parent=%-12s %s (session: %s)\n",
			d.Name, status, purpose, parent, d.WorkDir, sessionDisplay(d.SessionID))
	}
	// 🎯T111.4 thin surface: PO/boss with zero children while multi-slice
	// missions should have fan-out — visible without only RHS eyeballing.
	if hints := FormatFanOutHints(s.registry, s.overseerName()); hints != "" {
		b.WriteString("\n")
		b.WriteString(hints)
	}
	// 🎯T401: finished-and-reaped names stay visible as recoverable addresses
	// so the gate learns the seat is closed BEFORE composing into a void.
	if reaped := FormatReapedListSection(s.fleetIntent()); reaped != "" {
		b.WriteString("\n")
		b.WriteString(reaped)
	}
	if extra := s.FormatHostCostLines(len(defs)); extra != "" {
		b.WriteString("\n")
		b.WriteString(extra)
	}
	return mcp.NewToolResultText(fleetlog.PrependNotices(
		PrependFleetHealth(b.String(), reps), notices)), nil
}

// notifyFleetHealth delivers a fleet outage/recovery note to the overseer
// through the single deliver-by-name path (🎯T309.3). Tests may leave the
// overseer seam unwired, in which case delivery fails quietly here — a health
// note is ambient chatter, not a report someone is waiting on.
func (s *Server) notifyFleetHealth(line string) {
	if s == nil || line == "" {
		return
	}
	// Distinct prefix so activity strip / overseer can treat as system note.
	if _, err := s.deliverByName(s.overseerName(), "[Fleet health] "+line, OriginAgent, false); err != nil {
		slog.Debug("fleet health note undelivered", "err", err)
	}
}

func (s *Server) handleAgentStart(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	name, _ := args["name"].(string)
	workdir, _ := args["workdir"].(string)
	model, _ := args["model"].(string)
	providerArg, _ := args["provider"].(string)
	taskTypeArg, _ := args["task_type"].(string)
	actor, _ := args["actor"].(string)
	parent, _ := args["parent"].(string)
	purpose, _ := args["purpose"].(string)
	targetID, _ := args["target_id"].(string)
	prompt, _ := args["prompt"].(string)
	// Aliases: text / brief for start prompt (🎯T305).
	if strings.TrimSpace(prompt) == "" {
		if v, ok := args["text"].(string); ok {
			prompt = v
		}
	}
	if strings.TrimSpace(prompt) == "" {
		if v, ok := args["brief"].(string); ok {
			prompt = v
		}
	}
	forceEngage := boolArg(args["force_engage"])
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
	// 🎯T460: a new worker pane is load, not already-open Build work.
	// Critical host pressure refuses the spawn with a reason the PO can
	// read; owner seats and control-plane repair still pass.
	if blocked := s.checkHostSpawnAllowed(purpose, name); blocked != nil {
		s.logLifecycle(compAgentLifecycle, "start", "error", map[string]any{
			"name": name, "err": "host_saturated", "purpose": purpose,
		})
		return blocked, nil
	}

	// 🎯T414: a start is the control this target is named for, so it asks the
	// same question as the rest — should this agent be running? A standing
	// park or a provider wall declines it here, naming the intent, because
	// otherwise every automated caller (frontier consume, PO proactive, the
	// restart reattach) reaches a parked fleet through this one door.
	//
	// Reaped is the exception, and the distinction is the registry row rather
	// than the name: resurrecting the row that was reaped is the 🎯T413 bug,
	// while starting a fresh agent under a name the fleet once used is
	// ordinary re-use.
	rowExisted := s.registry != nil && s.registry.Def(name) != nil
	if dec := s.AllowFleetControl(name, fleetintent.ControlSpawn); !dec.Allow {
		if dec.Blocking != fleetintent.Reaped || rowExisted {
			life["err"] = dec.Reason
			s.logLifecycle(compAgentLifecycle, "start", "error", life)
			return mcp.NewToolResultError(fmt.Sprintf(
				"refusing to start %q — %s (%s). Lift it with jevons_fleet_intent state=working before starting.",
				name, fleetintent.Describe(dec.Blocking), dec.Reason)), nil
		}
		s.MarkAgentWorking(name, actor, "fresh start under a previously reaped name")
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

	// 🎯T222: work + target_id → no second implementer; closed targets refused.
	if purpose == claudia.PurposeWork && targetID != "" {
		if msg := s.refuseEngagedOrClosedTarget(name, workdir, targetID, forceEngage); msg != "" {
			life["err"] = "engagement_gate"
			life["target_id"] = targetID
			s.logLifecycle(compAgentLifecycle, "start", "error", life)
			return mcp.NewToolResultError(msg), nil
		}
	}

	def, existed, routeNote, err := s.stitchAgentStart(name, workdir, model, providerArg, taskTypeArg, parent, purpose, targetID, prompt)
	if err != nil {
		life["err"] = err.Error()
		life["existed"] = existed
		s.logLifecycle(compAgentLifecycle, "start", "error", life)
		return mcp.NewToolResultError(fmt.Sprintf("register failed: %v", err)), nil
	}
	life["existed"] = existed
	if routeNote != "" {
		life["provider_knob"] = routeNote
	}

	// 🎯T313: an agent whose transcript is gone cannot be resumed, and
	// claudia rightly refuses to paper over that. Rotate it onto a fresh
	// session here — before Launch — so a start request recovers in one
	// call instead of dead-ending into a manual kill → start → re-brief.
	// The rehydrate is reported to the caller, never silent.
	rehydrated := ""
	if lost, ok, err := fleet.RehydrateLostSessionIn(s.registry, name); err != nil {
		slog.Warn("lost-session rehydrate failed; falling through to launch",
			"name", name, "err", err)
	} else if ok {
		rehydrated = lost.Describe()
		life["rehydrated_from"] = lost.OldSession
		if d := s.registry.Def(name); d != nil {
			def = d
		}
	}

	proc, err := s.registry.Launch(name)
	if err != nil {
		life["err"] = err.Error()
		life["session_id"] = sessionDisplay(def.SessionID)
		s.logLifecycle(compAgentLifecycle, "start", "error", life)
		return mcp.NewToolResultError(prefixRehydrate(rehydrated,
			fmt.Sprintf("start failed: %v", err))), nil
	}

	// Wire events: broadcast to web UI and notify Jevon on agent responses.
	s.wireAgentEvents(name, proc)

	// 🎯T305 Failure A: optional start prompt must reach the pane and
	// begin a turn, or start fails loudly (no silent outcome=ok).
	prompt = strings.TrimSpace(prompt)
	briefNote := ""
	if prompt != "" {
		if err := s.deliverStartPrompt(name, prompt); err != nil {
			released, kept := s.startBriefFailureTeardown(name, existed, err)
			if kept {
				// 🎯T518: queued / delivered_unconfirmed means the brief is
				// HELD — by this daemon's queue or by the receiver — or the
				// instrument could not decide. Neither is never-landed, and
				// retiring the seat here is what removed jv-t515-relayrecord
				// as unbriefed_seat seven minutes after a healthy queued
				// start, destroying the very queue that would have finished
				// the delivery. The seat stays; the caller is told the brief
				// is pending and must not re-send it (🎯T416: a re-send on
				// this verdict stacks a second copy).
				life["brief_in_flight"] = true
				life["brief_verdict"] = err.Error()
				briefNote = fmt.Sprintf(
					" Opening brief is IN FLIGHT, not yet confirmed as a turn: %v."+
						" The daemon holds it for delivery at the next turn boundary."+
						" Do NOT re-send it — a re-send stacks a second copy (🎯T416/🎯T518).",
					err)
				prompt = "" // the result must not claim prompt_delivered=true
			} else {
				// Stop the process so agent_list does not report a phantom
				// running/never_briefed seat as successful work, and (🎯T387)
				// retire a row this call minted so the target is not left
				// engaged by a worker that never ran.
				if released {
					life["seat_released"] = true
					// 🎯T433: the tool error below reaches only the caller, and a
					// caller LLM dropping it is how a mint died twice with nobody
					// told. The seat's parent hears about the lost mint by name,
					// with the error verbatim, on the durable send path.
					s.notifySpawnFailure(def.Parent, def.TargetID, name, err.Error())
				}
				life["err"] = err.Error()
				life["session_id"] = sessionDisplay(def.SessionID)
				s.logLifecycle(compAgentLifecycle, "start", "error", life)
				return mcp.NewToolResultError(prefixRehydrate(rehydrated,
					fmt.Sprintf("start failed: %v", err))), nil
			}
		} else {
			life["prompt_delivered"] = true
		}
	}

	life["session_id"] = sessionDisplay(def.SessionID)
	life["purpose"] = def.Purpose
	life["parent"] = def.Parent
	life["provider"] = string(def.Provider)
	if def.TargetID != "" {
		life["target_id"] = def.TargetID
	}
	s.logLifecycle(compAgentLifecycle, "start", "ok", life)

	msg := formatAgentStartResult(name, def.WorkDir, def.Parent, string(def.Purpose), def.TargetID,
		string(def.Provider), sessionDisplay(def.SessionID), routeNote, prompt)
	msg += briefNote
	// 🎯T379: the agent has just inherited the provider's user-scoped MCP
	// list. Any entry pointing at a port nothing serves will sit in "still
	// connecting" forever, silently costing this agent those tools — so say
	// so here, where the caller who spawned it can act.
	msg += s.noteAgentMCPHealth(name, def.Provider)
	return mcp.NewToolResultText(prefixRehydrate(rehydrated, msg)), nil
}

// formatAgentStartResult is the owner-visible jevons_agent_start text.
// 🎯T476: routeNote must already cite which knob selected the provider.
func formatAgentStartResult(name, workdir, parent, purpose, targetID, provider, session, routeNote, prompt string) string {
	msg := fmt.Sprintf(
		"Agent %q started (session: %s, workdir: %s, parent: %s, purpose: %s, provider: %s",
		name, session, workdir, parent, purpose, provider)
	if targetID != "" {
		msg += fmt.Sprintf(", target_id: %s", targetID)
	}
	if routeNote != "" {
		msg += fmt.Sprintf(", %s", routeNote)
	}
	if prompt != "" {
		msg += ", prompt_delivered=true"
	}
	msg += ")"
	return msg
}

// stitchAgentStart mints or updates a fleet agent registry row the same way
// jevons_agent_start does before registry.Launch (🎯T148 / 🎯T215 / 🎯T476).
//
// Hermetic Session stitch surface: Provider selection (config.yaml on mint
// when provider omitted — 🎯T476), SessionID mint, Parent/Purpose/TargetID
// dual-write — without spawning Grok or Claude. Materialized stays false
// until a real Launch succeeds in claudia.
//
// routeNote always cites which knob selected the provider.
func (s *Server) stitchAgentStart(name, workdir, model, providerArg, taskTypeArg, parent, purpose, targetID, prompt string) (*claudia.AgentDef, bool, string, error) {
	if s == nil || s.registry == nil {
		return nil, false, "", fmt.Errorf("no agent registry")
	}
	existed := s.registry.Def(name) != nil
	def, err := s.registry.EnsureAgentWithParent(name, workdir, model, parent, true)
	if err != nil {
		return nil, existed, "", err
	}
	// Refresh copy after Ensure (Register may have stored a different pointer).
	if d := s.registry.Def(name); d != nil {
		def = d
	}

	// 🎯T148 + 🎯T476 provider selection:
	//   1. non-empty providerArg → ad hoc override (knob=explicit)
	//   2. resume with stored provider → keep (knob=resume)
	//   3. mint with empty provider → config.yaml / daemon default
	//      (knob=config). Leftover llm-portfolio.json and the compiled
	//      T325.2 seed are named as losers when they would have disagreed.
	//      Portfolio model pins do not apply when config won.
	stored := ""
	if def != nil {
		stored = string(def.Provider)
	}
	pick := s.mintProviderPick(providerArg, stored, existed, taskTypeArg, purpose)
	if pick.Knob == cost.KnobPlanDest && strings.TrimSpace(pick.Provider) == "" {
		return nil, existed, pick.Cite(), fmt.Errorf(
			"plan dest empty: all published providers fail mint thresholds; refusing to land on a hot dest (🎯T390.1.5)")
	}
	def.Provider = claudia.Provider(pick.Provider)
	routeNote := pick.Cite()

	// 🎯T324: session-truth model binding for this Launch/SessionID.
	// Explicit pin from the tool arg wins; empty pin on mint (or empty
	// stored model on resume) gets the provider default so cold Grok
	// agents expose a condensable id, not a bare mark forever.
	if pin := strings.TrimSpace(model); pin != "" {
		def.Model = pin
	} else if !existed || strings.TrimSpace(def.Model) == "" {
		def.Model = cli.BindSessionModel("", def.Provider)
	}
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
	// 🎯T510: host Goal is set on mint only. A remint / provider switch
	// keeps the stored objective so claudia's Session loop continues.
	if !existed {
		def.Goal = fleet.WorkSessionGoal(def.Purpose, def.TargetID, prompt, def.AutoStart)
		if def.SandboxMode == "" {
			def.SandboxMode = fleet.CodexWorkSandbox(def.Provider, def.Purpose)
		}
	}
	// 🎯T528: remint must not reopen Continue when the Goal's TargetIDs
	// are already achieved in the ledger (clear durable Goal).
	if strings.TrimSpace(def.Goal) != "" {
		statuses := fleet.LoadGoalTargetStatuses(def.WorkDir, def.Goal)
		if fleet.GoalMissionEvidencedComplete(def.Goal, statuses) {
			def.Goal = ""
		}
	}
	if s.mcp.URL != "" {
		def.MCPServers = mcpattach.SessionServers(s.mcp, def.Provider, def.WorkDir)
	}
	def.MCPExclusive = mcpattach.Exclusive
	if err := s.registry.Register(*def); err != nil {
		return nil, existed, "", err
	}
	if d := s.registry.Def(name); d != nil {
		def = d
	}
	return def, existed, routeNote, nil
}

// launchConfigFromDef is the Config handoff registry.Launch would pass into
// claudia Start (Provider, SessionID, RequireResume←Materialized, Goal).
// Hermetic tests assert this stitch without spawning a process (🎯T215 / 🎯T510).
func launchConfigFromDef(def *claudia.AgentDef) (provider claudia.Provider, sessionID string, requireResume bool) {
	cfg := startConfigFromDef(def)
	return cfg.Provider, cfg.SessionID, cfg.RequireResume
}

func startConfigFromDef(def *claudia.AgentDef) claudia.Config {
	if def == nil {
		return claudia.Config{}
	}
	return claudia.Config{
		Provider:      def.Provider,
		SessionID:     def.SessionID,
		RequireResume: def.Materialized,
		Model:         def.Model,
		Goal:          effectiveSessionGoal(def),
		SandboxMode:   def.SandboxMode,
		MCPServers:    def.MCPServers,
		MCPExclusive:  def.MCPExclusive,
	}
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

// workAgentsEngagedOnTarget returns registered work agents bound to targetID
// **in the ledger that scopeWorkdir resolves to**, excluding excludeName
// (same-name resume). Skips overseer purpose. 🎯T222, scoped by 🎯T389.
//
// scopeWorkdir is the workdir of the work being asked about — the spawning
// agent's, or the repo whose frontier is being swept. An agent bound to
// claudia 🎯T19 is not an implementer of orthograph 🎯T19, so it neither
// blocks that spawn nor answers to that id. Empty scopeWorkdir asks
// unscoped (the pre-T389 reading) and matches every ledger.
func workAgentsEngagedOnTarget(reg *claudia.Registry, targetID, scopeWorkdir, excludeName string) []string {
	want := normalizeAgentTargetID(targetID)
	if reg == nil || want == "" {
		return nil
	}
	wantLedger := targetfile.LedgerKey(scopeWorkdir)
	excludeName = strings.TrimSpace(excludeName)
	var names []string
	for _, d := range reg.List() {
		if normalizeAgentTargetID(d.TargetID) != want {
			continue
		}
		if !targetfile.SameLedger(wantLedger, targetfile.LedgerKey(d.WorkDir)) {
			continue
		}
		if d.Purpose == claudia.PurposeOverseer || d.Name == "jevons" {
			continue
		}
		// Durable POs are not implementers for engagement thrash (kickoff
		// workers are work agents with target_id; PO usually has empty TargetID).
		if d.Name == excludeName {
			continue
		}
		// Only purpose=work counts as engaged implementer (asides residual).
		p := strings.TrimSpace(d.Purpose)
		if p == "" {
			p = claudia.PurposeWork
		}
		if p != claudia.PurposeWork {
			continue
		}
		names = append(names, d.Name)
	}
	return names
}

// loadTargetStatusForKickoff looks up ledger status for targetID under cwd.
// Tests may override. Missing ledger → empty status (engagement-only gate).
var loadTargetStatusForKickoff = targetfile.LoadTargetStatusFromCwd

// refuseEngagedOrClosedTarget returns a non-empty error message when
// agent_start must not spawn a second implementer (🎯T222).
func (s *Server) refuseEngagedOrClosedTarget(name, workdir, targetID string, force bool) string {
	if force {
		return ""
	}
	targetID = normalizeAgentTargetID(targetID)
	if targetID == "" {
		return ""
	}
	var engaged []string
	if s != nil && s.registry != nil {
		engaged = workAgentsEngagedOnTarget(s.registry, targetID, workdir, name)
	}
	status, _ := loadTargetStatusForKickoff(workdir, targetID)
	dec := targetfile.GateKickoff(status, engaged, force)
	if dec.Allow {
		return ""
	}
	return dec.Message
}

func (s *Server) handleAgentSend(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	name, _ := args["name"].(string)
	text, _ := args["text"].(string)
	actor, _ := args["actor"].(string)
	interrupt, _ := args["interrupt"].(bool)

	if name == "" || text == "" {
		return mcp.NewToolResultError("name and text are required"), nil
	}

	// 🎯T321: name the caller so AuthorizeDeliver runs against a real actor
	// rather than the shared-transport blank. Same defaulting pattern as kill:
	// empty actor may resolve from the overseer session; fleet callers must
	// pass actor explicitly.
	actor = strings.TrimSpace(actor)
	if actor == "" && s.transcript != nil && s.transcript.GetID != nil {
		sid := s.transcript.GetID()
		if s.registry != nil {
			for _, d := range s.registry.List() {
				if d.SessionID == sid {
					actor = d.Name
					break
				}
			}
		}
		if actor == "" {
			actor = s.overseerName()
		}
	}
	if actor == "" {
		return mcp.NewToolResultError("actor is required (pass your agent name; overseer uses the overseer name)"), nil
	}

	// 🎯T104 under fan-out: first send carries standing local-delivery brief.
	s.mu.Lock()
	if s.fleetBriefed == nil {
		s.fleetBriefed = map[string]bool{}
	}
	text, injected := EnsureFleetBrief(s.fleetBriefed, name, text)
	s.mu.Unlock()
	if injected {
		// 🎯T425: the brief is the daemon's own prose and it is almost entirely
		// role-conditional, so it opens by naming the role its reader occupies.
		// The sender's message below is untouched.
		text = s.withIdentity(name, text)
		slog.Info("fleet standing brief injected on first send", "name", name)
	}

	// 🎯T111.1 / 🎯T321: rehydrate + send under the caller's lineage, or
	// queue/interrupt when prompt in flight.
	result, err := s.sendToAgentAs(actor, name, text, interrupt)
	if err != nil {
		// 🎯T283: deliverToSender already formats send failures; this also
		// classifies the rehydrate/launch arm, which reaches the provider too.
		return toolFailure("agent_send", name, err), nil
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
	// 🎯T408 via 🎯T414: stopping without killing is an instruction, and the
	// instruction is the part that used to evaporate. The process ends here;
	// the park outlives it, the delivery that would restart the agent, and the
	// daemon restart that would reattach it.
	actor, _ := args["actor"].(string)
	if strings.TrimSpace(actor) == "" {
		actor = s.overseerName()
	}
	reason, _ := args["reason"].(string)
	s.MarkAgentParked(name, actor, strings.TrimSpace(reason))
	s.logLifecycle(compAgentLifecycle, "stop", "ok", map[string]any{"name": name, "actor": actor})
	// 🎯T418 clause 6: if this stop left the fleet with queued work and
	// nobody live to press Enter, say so now — the cockpit may relaunch
	// the overseer on the next tick.
	s.reportFleetMuteIfNeeded()
	return mcp.NewToolResultText(fmt.Sprintf(
		"Agent %q stopped and parked (still registered; nothing revives it — not a delivery, not a restart, not the idle sweep — until the park is lifted with jevons_fleet_intent name=%q state=working).",
		name, name)), nil
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
	// Idempotent kill (🎯T229): desired state is "not registered". After
	// T165/T195 auto-reap, PO/overseer hygiene kills race the reaper and used
	// to log lifecycle_error for every double-kill. Already-gone is success.
	if s.isOverseerAgent(name) {
		life["err"] = fmt.Sprintf("refusing to kill %q — that is the overseer", name)
		s.logLifecycle(compAgentLifecycle, "kill", "error", life)
		return mcp.NewToolResultError(fmt.Sprintf("refusing to kill %q — that is the overseer", name)), nil
	}
	if s.registry.Def(name) == nil {
		if actor == "" {
			life["err"] = "actor is required (pass your agent name; overseer uses the overseer name)"
			s.logLifecycle(compAgentLifecycle, "kill", "error", life)
			return mcp.NewToolResultError("actor is required (pass your agent name; overseer uses the overseer name)"), nil
		}
		life["already_gone"] = true
		life["descendants"] = 0
		s.logLifecycle(compAgentLifecycle, "kill", "ok", life)
		return mcp.NewToolResultText(fmt.Sprintf(
			"Agent %q already not registered (idempotent kill by %q; no-op).",
			name, actor,
		)), nil
	}
	if err := canKill(s.registry, actor, name, s.isOverseerAgent); err != nil {
		life["err"] = err.Error()
		s.logLifecycle(compAgentLifecycle, "kill", "error", life)
		return mcp.NewToolResultError(err.Error()), nil
	}
	desc := s.registry.Descendants(name)
	refuse, reason, restartNames := ClassifyKillHeldSendq(name, desc, s.pendingAgentSends)
	if refuse {
		life["err"] = reason
		s.logLifecycle(compAgentLifecycle, "kill", "error", life)
		return mcp.NewToolResultError(reason), nil
	}
	held := s.snapshotHeldSeats(restartNames)
	newParent := ""
	if len(held) > 0 {
		newParent = s.surviveDrainParent(name, actor)
	}
	if err := s.killSubtreeAndClearTurns(name); err != nil {
		life["err"] = err.Error()
		s.logLifecycle(compAgentLifecycle, "kill", "error", life)
		return mcp.NewToolResultError(fmt.Sprintf("kill failed: %v", err)), nil
	}
	restarted := s.restartHeldSendqForDrain(held, newParent, actor)
	life["descendants"] = len(desc)
	if len(restarted) > 0 {
		life["t530_restarted"] = restarted
	}
	s.logLifecycle(compAgentLifecycle, "kill", "ok", life)
	msg := fmt.Sprintf(
		"Agent %q killed by %q: process stopped and deregistered (will not auto-start; gone from agent list).",
		name, actor,
	)
	if len(desc) > 0 {
		msg += fmt.Sprintf(" Also killed %d descendant(s): %s.", len(desc), strings.Join(desc, ", "))
	}
	if len(restarted) > 0 {
		msg += fmt.Sprintf(
			" 🎯T530 restarted %d held-sendq seat(s) for drain under %q: %s.",
			len(restarted), newParent, strings.Join(restarted, ", "))
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
	if strings.EqualFold(name, s.overseerName()) {
		return true
	}
	if s.transcript != nil && s.transcript.GetID != nil && s.registry != nil {
		sid := s.transcript.GetID()
		if sid != "" {
			if def := s.registry.Def(name); def != nil && def.SessionID == sid {
				if isOverseerSeatRow(*def) {
					return true
				}
				// 🎯T452: a subordinate row that carries the overseer's
				// session id is not the overseer seat. Treating it as one
				// would deliver its brief into owner chat.
				slog.Error("🎯T452 refused a session-id claim on the overseer seat",
					"component", "agent_send",
					"name", name,
					"parent", def.Parent,
					"purpose", def.Purpose,
					"session_id", sid,
				)
			}
		}
	}
	return false
}

// isOverseerSeatRow reports whether a registry row could be the overseer:
// explicit purpose=overseer, or a root row (no parent).
func isOverseerSeatRow(d claudia.AgentDef) bool {
	if strings.EqualFold(strings.TrimSpace(d.Purpose), claudia.PurposeOverseer) {
		return true
	}
	return strings.TrimSpace(d.Parent) == ""
}

func (s *Server) overseerName() string {
	// Conventional default; config overseer_name is almost always "jevons".
	return "jevons"
}

// wireAgentEvents sets up the event handler for an agent process.
// It broadcasts to the web UI and notifies Jevon when the agent
// produces a text response.
//
// 🎯T426: idempotent per process object, so a launch road may call it without
// knowing whether some other road already did. Every launch road goes through
// this one function — see agent_wiring.go for why the wiring is now asserted
// as an invariant rather than performed as a step.
func (s *Server) wireAgentEvents(name string, proc *claudia.Agent) {
	if proc == nil {
		return
	}
	s.attachAgentSink(name, proc)
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

		// 🎯T392.4: count this turn's tool calls and act at the ceiling.
		s.observeTurnDepth(name, ev)

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
			// 🎯T528: close Session Goal when ledger/GOAL_STATUS evidences
			// complete — before Claudia's settle timer can inject Continue.
			s.clearSessionGoalIfComplete(name, text)
			// 🎯T416: the turn boundary the send path needs. This is the only
			// place the daemon learns an agent is idle — it never infers it
			// from a registry row or from having launched something, because a
			// pane can outlive the daemon and a pooled window was never
			// watched by this process.
			s.noteTurnEnded(name)
			// 🎯T236: latch structured failure class / empty terminal so fleet
			// recover can re-pressure without owner continue (T237 class).
			s.mu.Lock()
			tracker := s.idleActivity
			s.mu.Unlock()
			if tracker != nil {
				tracker.NoteTerminalOutcome(name, text)
			}
			if text != "" {
				// Notify overseer first so the done report is delivered before
				// the worker leaves the registry (🎯T165) — unless [silent]
				// (ops events: daemon-restarted / worker-idle fine chatter).
				s.notify(name, text)
			}
			// 🎯T111.1: deliver any nudges queued while the prompt was in flight.
			// Off the event goroutine since 🎯T416: the drain now waits for the
			// message to become a turn, and blocking an agent's event stream
			// for that window would stall its own progress reporting.
			go s.drainAgentSendQueue(name)
			// 🎯T165: finished work agents auto stop+Remove (not persona-only).
			s.maybeReapDoneWorkAgent(name, text)
		}
	}
}

// notify delivers an agent's turn-complete report to the overseer.
// Silent ops replies ([silent] prefix) are logged but not sent to the overseer
// so owner chat is not spammed with "workers fine / no continue needed".
//
// 🎯T309.3: a shim over deliverByName addressed to the overseer, not a
// privileged wire of its own. A worker reporting up and a PO being nudged now
// travel the same code; the overseer arm keeps the journal + notify-queue
// semantics that make 🎯T62's silent drop impossible.
func (s *Server) notify(agentName, text string) {
	if IsSilentAgentResponse(text) {
		slog.Info("agent response suppressed (silent)",
			"agent", agentName, "len", len(text))
		return
	}

	// 🎯T388: store the report BEFORE delivering it and before the 🎯T165/T195
	// reap can remove the agent, so the full text outlives its author. When
	// jv-t372-auto was asked to resend, jevons_agent_send answered "agent is
	// not running" and the content survived only because that worker happened
	// to have committed its reasoning to a design doc.
	handle := s.storeAgentReport(agentName, text)

	// 🎯T502: a bare acknowledgement ("No response requested.") is a
	// turn-boundary artefact, not a report — suppressing it here is a routing
	// decision, not amnesia: it is stored above like any report, the idle
	// signal still reaches the parent through the 🎯T207/T414 tracker, and
	// anything with a finish shape, an ask, or a T392.7 direct-route class
	// still escalates. See bareAckTurnReport.
	if bareAckTurnReport(text) {
		slog.Info("agent bare ack suppressed (not a report)",
			"agent", agentName, "len", len(text), "report_id", handle.ReportID)
		s.logLifecycle(compAgentLifecycle, "notify", "suppressed_bare_ack", map[string]any{
			"agent": agentName, "report_id": handle.ReportID,
		})
		return
	}

	// Fit the report to the delivery bound. This used to be text[:1997]+"...",
	// which lost the tail behind a marker indistinguishable from the author's
	// own ellipsis — and reports put their conclusions and asks at the end, so
	// a head-only cut ate exactly the part worth sending. Elide keeps both
	// ends, marks the gap unmistakably, and names the call that returns the
	// whole thing.
	elision := agentreport.Elide(text, agentreport.DeliveryBound, handle)
	if elision.Truncated {
		slog.Warn("agent report elided for delivery",
			"agent", agentName,
			"total_bytes", elision.TotalBytes,
			"elided_bytes", elision.ElidedBytes,
			"report_id", handle.ReportID,
			"retrievable", !handle.Empty())
	}

	msg := fmt.Sprintf("[Agent %s responded]\n%s", agentName, elision.Text)

	// 🎯T386: a green the report's own evidence does not support is flagged
	// here, in front of the report, before the overseer can accept it and
	// before a target retires on it. The banner rides outside the elision so
	// a long report cannot push the warning off the end.
	if flags := FalseGreenFlags(text); len(flags) > 0 {
		kinds := falseGreenKinds(flags)
		slog.Warn("T386 false-green flags on agent report",
			"agent", agentName, "flags", kinds)
		s.logLifecycle(compAgentLifecycle, "false_green", "flagged", map[string]any{
			"agent": agentName, "flags": kinds,
		})
		msg = gate.Banner(flags) + "\n\n" + msg
	}

	overseer := s.overseerName()
	res, err := s.deliverByName(overseer, msg, OriginAgent, false)
	if err != nil {
		// Undeliverable is loud: the reply the owner is waiting on did not
		// land, and that fact must reach the product log (🎯T61/T62).
		slog.Error("agent response notify failed",
			"agent", agentName, "overseer", overseer, "len", len(text), "err", err)
		s.logLifecycle(compAgentLifecycle, "notify", "error", map[string]any{
			"agent": agentName, "overseer": overseer, "err": err.Error(),
		})
		return
	}
	slog.Info("notifying jevon",
		"agent", agentName, "len", len(text), "status", res.Status)
}

// broadcastAgentEvent fans a worker event to the optional progress/UI hook
// (🎯T118). Previously a no-op stub; the HTTP server wires ObserveAgentProgress
// + agents_changed so fleet rows update without poll.
// Also feeds the idle activity tracker and enter-idle → parent events (🎯T207).
func (s *Server) broadcastAgentEvent(name string, ev claudia.Event) {
	s.mu.Lock()
	hook := s.agentEventHook
	tracker := s.idleActivity
	s.mu.Unlock()
	if tracker != nil {
		prevPhase, nextPhase, enteredIdle := tracker.ObserveTransition(name, ev)
		if enteredIdle {
			// 🎯T414: the transition travels with the call so the emitter can
			// put the real edge through ShouldEmitWorkerIdle — the gate that
			// reads intent — rather than asserting the edge it assumes.
			s.emitWorkerIdleToParent(name, prevPhase, nextPhase)
		}
	}
	if hook != nil {
		hook(name, ev)
	}
}

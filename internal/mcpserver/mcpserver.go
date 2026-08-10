// Copyright 2025 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package mcpserver exposes jevon worker management as MCP tools,
// replacing the jevon-ctl CLI binary with an in-process MCP server.
package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/audit"
	"github.com/marcelocantos/jevons/internal/butler"
	"github.com/marcelocantos/jevons/internal/capacity"
	"github.com/marcelocantos/jevons/internal/cli"
	"github.com/marcelocantos/jevons/internal/cost"
	"github.com/marcelocantos/jevons/internal/discovery"
	"github.com/marcelocantos/jevons/internal/doit"
	"github.com/marcelocantos/jevons/internal/eventlog"
	"github.com/marcelocantos/jevons/internal/research"
	"github.com/marcelocantos/jevons/internal/rsi"
	"github.com/marcelocantos/jevons/internal/secauditor"
	"github.com/marcelocantos/jevons/internal/wakebatch"
	"github.com/marcelocantos/jevons/internal/workers"
	"github.com/marcelocantos/jevons/internal/writconf"
)

// ScreenshotFunc requests a screenshot from connected clients and returns the file path.
type ScreenshotFunc func() (string, error)

// TranscriptOps provides transcript manipulation functions.
type TranscriptOps struct {
	Read     func(sessionID string) ([]map[string]any, error)
	Truncate func(sessionID string, keepTurns int) error
	GetID    func() string // current Jevon claude session ID (from claudia registry)
}

// Server wraps an MCP server that provides worker management tools.
type Server struct {
	registry   *claudia.Registry
	scanner    *discovery.Scanner
	butler     *butler.Butler
	workerWD   string
	screenshot ScreenshotFunc
	transcript *TranscriptOps

	// spawnGuard / resumeGuard are the budget clamp-down gates (T36.1):
	// every MCP path that creates or re-launches a worker must consult
	// them so spawnHalted cannot be bypassed via jwork / agent_start.
	// Nil means unguarded (tests / cost DB unavailable).
	spawnGuard  func() error
	resumeGuard func(id string, auto bool) error

	mcpSrv    *server.MCPServer
	transport *server.StreamableHTTPServer

	toolsListCount int64

	mu          sync.Mutex
	notifyJevon NotifyFunc
	// overseerDeliver is the overseer arm of the single deliver-by-name path
	// (🎯T309.3). Wired from main to server.DeliverToOverseerAs so an
	// overseer-addressed send reuses the owner chat journal and notify queue.
	// Nil falls back to notifyJevon for agent-origin text (see deliverToOverseer).
	overseerDeliver OverseerDeliverFunc
	// resolveSender overrides fleet-agent process resolution on that same
	// path. Nil — the product path — resolves via the registry. Test seam.
	resolveSender senderResolver
	// observeTurnWitness overrides turn-evidence observation (🎯T387): what
	// the AGENT did after a send, as opposed to what the send call returned.
	// Nil — the product path — watches the live claudia process. Test seam.
	observeTurnWitness turnWitness
	// agentEventHook receives every fleet worker event (progress, assistant, …)
	// so the HTTP server can maintain RHS progress chrome (🎯T118).
	agentEventHook func(name string, ev claudia.Event)
	costSnapshot   func() (*cost.Snapshot, error)

	// grokRun shells out to the Grok CLI for mid-session MCP reconnect (🎯T60).
	// Nil uses defaultGrokRun (exec of grok on PATH). Tests inject a fake.
	grokRun grokRunFunc

	// fleetBriefed tracks agents that already received FleetStandingBrief
	// on first jevons_agent_send (🎯T104 under fan-out).
	fleetBriefed map[string]bool

	// agentTurnBegan tracks agents that have begun ≥1 confirmed turn in
	// this daemon process (start prompt or successful send — 🎯T305).
	// Distinct from registry Materialized (durable). Used so agent_list
	// can report never_briefed vs running for live zero-turn seats.
	agentTurnBegan map[string]bool

	// selfTestEnv builds the 🎯T110 pack environment (shared with HTTP).
	selfTestEnv SelfTestEnvFunc

	// workers tracks jwork lifecycle in SQLite + SSE (🎯T8.2). Nil = no-op.
	workers *workers.Tracker
	// doitEng is the execution-safety engine (🎯T8.3). Nil = spawn unguarded
	// by policy (tests without engine).
	doitEng *doit.Engine

	// agentSendQ is a per-agent FIFO of pending sends when the ACP session is
	// busy (🎯T115). Guarded by mu. Nil until first enqueue.
	agentSendQ map[string][]string

	// eventLogTail tails durable product logs (🎯T120). Nil = tool unregistered.
	eventLogTail EventLogTailFunc
	// eventLogger dual-writes server lifecycle events via HTTP Server.LogEvent
	// when wired from main (🎯T128.4). Nil = fall through to eventJournal/slog.
	eventLogger EventLoggerFunc
	// eventJournal is the durable product journal for MCP lifecycle dual-write
	// (🎯T128.1 / T128.4). Same file as GET /api/logs when SetEventJournal is wired.
	eventJournal *eventlog.Journal

	// migrator moves an existing agent between backends, carrying a
	// handover to the successor (🎯T285). Nil = jevons_agent_migrate
	// unregistered rather than half-working.
	migrator Migrator

	// defaultProvider is the daemon-wide claudia backend for new agents
	// when agent_start / thread_spawn / jwork omit provider (🎯T148).
	// Empty means cli.ResolveProvider falls through to env / grok at use time.
	defaultProvider string

	// llmPortfolio is the multi-provider task-type routing seed (🎯T325.2).
	// Nil → cost.DefaultPortfolio(). Soft-cap overlays may come from budget.
	llmPortfolio *cost.Portfolio
	// providerSoftCaps overlays portfolio soft caps (from budget.json).
	providerSoftCaps map[string]int

	// rsiLoop is the residual phrase/eventlog mint path (🎯T92; opt-in product residual).
	// Nil until SetRSILoop; jevons_rsi_cycle requires it. Product path is rsiCoach (🎯T243).
	rsiLoop *rsi.Loop

	// rsiCoach posts judgments to the overseer; never files bullseye (🎯T243).
	// Nil until SetRSICoach.
	rsiCoach *rsi.Coach

	// staffOps holds cooldown state for one bounded ops cycle (🎯T325.4).
	// Nil until registerStaffOpsTools; pure policy in internal/staffops.
	staffOps *staffOpsState
	// sentinel holds cooldown/budget/grace state for durable 🎯T219 loop.
	// Nil until ensureSentinelRuntime / registerSentinelTools.
	sentinel *sentinelRuntime

	// secAuditor is the standing security interest (🎯T335). Nil until wired.
	secAuditor *secauditor.Interest
	// writExec confines high-risk fleet exec under writ (🎯T335).
	writExec      writconf.Executor
	writBin       string
	writAvailable bool

	// idleActivity tracks ACP phase for enter-idle detection (🎯T207).
	// Nil until StartIdleNudgeLoop; broadcastAgentEvent Observes transitions.
	idleActivity *IdleActivityTracker
	// idleNudgeLedger carries durable backoff/max for the 🎯T315 re-pressure
	// actuator and the post-restart resume sweep; may be nil (no StateDir).
	idleNudgeLedger *IdleNudgeLedger
	// idlePressureHooks are the optional 🎯T316/T317 collaborator seams for
	// the idle re-pressure actuator. Zero value = conservative defaults.
	idlePressureHooks IdlePressureHooks
	// impatience is the 🎯T317 ladder (T316 set + sinks). Nil = T315-only
	// SweepIdleNudges path. Installed from main via SetImpatienceEngine.
	impatience *ImpatienceEngine
	// idleEventLast debounces worker-idle events per agent name.
	idleEventLast map[string]time.Time

	// ownerNotifier writes deterministic owner notices that depend on no
	// agent (🎯T415). Nil means exhaustion is logged but not reported,
	// which is the pre-T415 behaviour and is said loudly at the point of
	// use rather than discovered later.
	ownerNotifier OwnerNotifier
	// exhaustion dedups repeated convergence exhaustion per agent.
	exhaustion exhaustionState
	// recoverBin is the detached diagnostician (🎯T415.1); stateDir is
	// what it reads. Empty leaves diagnosis unavailable, which the
	// deterministic notice does not depend on.
	recoverBin string
	stateDir   string

	// wakeBatch coalesces machine-generated events into one digest per
	// recipient (🎯T392.2). Debouncing above is per-worker and stops the
	// same worker firing twice; this is per-recipient and stops four
	// different workers each buying a full coordinator turn. Nil means
	// batching is off and every event delivers immediately.
	wakeBatch *wakebatch.Batcher
	// idleNudgeSweep is set by StartIdleNudgeLoop for cockpit fleet health
	// (dead-handle sweep only — no auto-continue ladder).
	idleNudgeSweep func(postRestart bool)

	// ideaStateDir roots the durable idea ledger (state_dir/ideas.json, 🎯T325.3).
	// Empty until SetIdeaStateDir; idea tools stay unregistered.
	ideaStateDir string

	// agentReportDir roots the durable agent-report store (🎯T388) so a
	// terminal report outlives the agent that wrote it. Empty until
	// SetAgentReportDir; jevons_agent_report_read stays unregistered and an
	// over-bound report is marked as cut without a retrieval handle.
	// Guarded by mu.
	agentReportDir string

	// research is the ambient research staff cycle (🎯T356): periodic context
	// refresh plus async feed triggers, writing durable versioned notes.
	// Nil until SetResearchAgent.
	research *research.Agent

	// auditor is the periodic full-scan audit cycle (🎯T357): bounded passes
	// over code, skills, and prompts on an advanced-tier model, folded into
	// durable residue. Nil until SetAuditor.
	auditor *audit.Auditor

	// capacityGov admits, defers, or degrades background work against the
	// remaining budget and concurrent load (🎯T359). Nil until
	// SetCapacityGovernor — ambient loops then run ungated, as before.
	capacityGov *capacity.Governor
}

// TriggerIdleNudgeSweep runs one fleet health + recover sweep (postRestart=false).
// 🎯T236: also re-pressures open-mission workers after stuck-busy / terminal failure.
// No-op until StartIdleNudgeLoop has registered the actuator.
func (s *Server) TriggerIdleNudgeSweep() {
	if s == nil {
		return
	}
	s.mu.Lock()
	f := s.idleNudgeSweep
	s.mu.Unlock()
	if f != nil {
		f(false)
	}
}

// TriggerFleetRecoverSweep runs one open-mission stuck/failure recover pass (🎯T236).
// Safe for cockpit hooks; no-op when registry unset.
func (s *Server) TriggerFleetRecoverSweep() {
	if s == nil || s.registry == nil {
		return
	}
	s.runFleetRecoverSweep(false)
}

// SweepFleetHealth runs SweepDeadAgents for the given overseer name
// (log-only report). Safe for cockpit hooks.
func (s *Server) SweepFleetHealth(overseerName string) {
	if s == nil || s.registry == nil {
		return
	}
	if overseerName == "" {
		overseerName = "jevons"
	}
	if reps := SweepDeadAgents(s.registry, overseerName); len(reps) > 0 {
		slog.Info("cockpit fleet health", "report", FormatDeadAgentReport(reps))
	}
}

// SetDefaultProvider sets the daemon-wide claudia backend used when spawn
// tools omit provider (🎯T148). Pass the already-resolved default
// (cli.ResolveProvider("", cfg.Provider)); empty re-resolves from env at use.
func (s *Server) SetDefaultProvider(provider string) {
	s.defaultProvider = strings.TrimSpace(provider)
}

// SetLLMPortfolio installs the multi-provider routing seed (🎯T325.2).
// Nil clears to DefaultPortfolio at route time.
func (s *Server) SetLLMPortfolio(p *cost.Portfolio) {
	if s == nil {
		return
	}
	s.llmPortfolio = p
}

// SetProviderSoftCaps overlays session soft caps from budget.json
// provider_soft_caps (🎯T325.2). Nil/empty leaves compiled defaults.
func (s *Server) SetProviderSoftCaps(caps map[string]int) {
	if s == nil {
		return
	}
	s.providerSoftCaps = caps
}

// effectivePortfolio returns the routing seed with soft-cap overlays applied.
func (s *Server) effectivePortfolio() *cost.Portfolio {
	base := cost.DefaultPortfolio()
	if s != nil && s.llmPortfolio != nil {
		base = s.llmPortfolio
	}
	if s != nil && len(s.providerSoftCaps) > 0 {
		return base.MergeSoftCaps(s.providerSoftCaps)
	}
	return base
}

// harnessLoadCounts tallies registered agents by claudia provider id
// (session soft-cap input for portfolio routing — never USD).
func (s *Server) harnessLoadCounts() cost.LoadCounts {
	load := cost.LoadCounts{}
	if s == nil || s.registry == nil {
		return load
	}
	for _, d := range s.registry.List() {
		p := strings.ToLower(strings.TrimSpace(string(d.Provider)))
		if p == "" {
			p = string(cli.DefaultProvider)
		}
		load[p]++
	}
	return load
}

// resolvedDefaultProvider returns the effective default for new agents.
func (s *Server) resolvedDefaultProvider() claudia.Provider {
	// defaultProvider is the config-resolved value from main; pass as cfg
	// so env is only consulted when main left it empty.
	return cli.ResolveProvider("", s.defaultProvider)
}

// New creates an MCP server providing the jevons tool surface. The durable
// thread model (butler) and jwork are the only worker lifecycles; the legacy
// manager-backed session tools were removed (🎯T41).
// transcript may be nil if transcript ops are not available.
func New(workerWD string, screenshot ScreenshotFunc, transcript *TranscriptOps) *Server {
	s := &Server{
		workerWD:   workerWD,
		screenshot: screenshot,
		transcript: transcript,
	}

	mcpSrv := server.NewMCPServer("jevons", "1.0.0")
	s.mcpSrv = mcpSrv

	if s.screenshot != nil {
		mcpSrv.AddTool(
			mcp.NewTool("jevons_screenshot",
				mcp.WithDescription("Take a screenshot of the connected mobile client's current screen. Returns the file path of the saved PNG image."),
			),
			s.handleScreenshot,
		)
	}

	if s.transcript != nil {
		mcpSrv.AddTool(
			mcp.NewTool("jevons_transcript_read",
				mcp.WithDescription("Read a conversation transcript. With agent=<name>, returns THAT agent's transcript only (registry session_id) — never substitutes the caller's/overseer transcript (🎯T304). Omit agent to read the active overseer/Jevon session. Returns turns with role and text."),
				mcp.WithString("agent", mcp.Description("Fleet agent name whose transcript to read (e.g. jv-t300-loading-earlier). Required for supervision of workers; omit for the active overseer session.")),
			),
			s.handleTranscriptRead,
		)
		mcpSrv.AddTool(
			mcp.NewTool("jevons_transcript_rewind",
				mcp.WithDescription("Rewind the Jevon conversation to keep only the first N turns. A turn is a user message + assistant response. Set turns to 0 for a complete reset. The next message will start a fresh conversation."),
				mcp.WithNumber("turns", mcp.Required(), mcp.Description("Number of turns to keep (0 = reset)")),
			),
			s.handleTranscriptRewind,
		)
	}

	s.registerJwork()
	s.registerMCPReconnect()
	s.registerAgentMigrate()
	s.registerStaffOpsTools()
	s.registerSentinelTools()
	s.registerWritSecurityTools() // 🎯T335 security auditor + writ confinement

	s.transport = server.NewStreamableHTTPServer(mcpSrv, server.WithStateLess(true))
	return s
}

// RegisterRoutes adds the MCP endpoint to the given mux. Requests are
// logged at debug level with the JSON-RPC method — MCP clients fail
// silently (a server that never connects just means "no tools"), so
// visibility here is the only way to diagnose tool-wiring gaps (🎯T50).
func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	mux.Handle("/mcp", mcpRequestLogger(s.transport, &s.toolsListCount))
}

// ToolsListCount reports how many MCP tools/list requests have been
// served since boot — the boot-time oracle for "did any agent actually
// attach our tools?" (🎯T50: the Grok CLI drops servers silently).
func (s *Server) ToolsListCount() int64 { return atomic.LoadInt64(&s.toolsListCount) }

func mcpRequestLogger(next http.Handler, toolsList *int64) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var method string
		if r.Body != nil {
			body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
			if err == nil {
				r.Body = io.NopCloser(bytes.NewReader(body))
				var m struct {
					Method string `json:"method"`
				}
				_ = json.Unmarshal(body, &m)
				method = m.Method
			}
		}
		if method == "tools/list" {
			atomic.AddInt64(toolsList, 1)
		}
		slog.Debug("mcp request", "http", r.Method, "rpc", method, "ua", r.UserAgent())
		next.ServeHTTP(w, r)
	})
}

// SetBudgetGuards wires the cost enforcer's AllowSpawn / AllowResume into
// every MCP worker-launch path. Call with enforcer methods when the cost
// guard is live; leave unset when the usage DB is unavailable.
func (s *Server) SetBudgetGuards(spawn func() error, resume func(id string, auto bool) error) {
	s.spawnGuard = spawn
	s.resumeGuard = resume
}

// SetWorkersTracker attaches the 🎯T8.2 worker observability store + SSE hub.
func (s *Server) SetWorkersTracker(t *workers.Tracker) {
	s.workers = t
}

// SetDoitEngine attaches the 🎯T8.3 execution-safety engine for jwork gating.
func (s *Server) SetDoitEngine(eng *doit.Engine) {
	s.doitEng = eng
}

// checkSpawnAllowed refuses new worker launch when the budget clamp has
// halted spawning. Returns an MCP tool-error result when blocked.
func (s *Server) checkSpawnAllowed() *mcp.CallToolResult {
	if s.spawnGuard == nil {
		return nil
	}
	if err := s.spawnGuard(); err != nil {
		return mcp.NewToolResultError(err.Error())
	}
	return nil
}

// checkResumeAllowed refuses re-launch of a named worker when the budget
// clamp blocks resume (spawn-halt, throttle window, pause/kill clamp).
func (s *Server) checkResumeAllowed(id string) *mcp.CallToolResult {
	if s.resumeGuard == nil {
		return nil
	}
	if err := s.resumeGuard(id, false); err != nil {
		return mcp.NewToolResultError(err.Error())
	}
	return nil
}

// --- tool handlers ---

func (s *Server) handleScreenshot(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, err := s.screenshot()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("screenshot failed: %v", err)), nil
	}
	return mcp.NewToolResultText(path), nil
}

// handleTranscriptRead returns turns for a named agent or the active overseer
// session. 🎯T304: agent=<name> must resolve that agent's registry session only;
// never fall back to GetID (caller/overseer) when a name is given — silent
// substitution makes supervisors treat another seat's words as the worker's.
func (s *Server) handleTranscriptRead(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.transcript == nil || s.transcript.Read == nil {
		return mcp.NewToolResultError("transcript reader unavailable"), nil
	}

	args := req.GetArguments()
	agentName := strings.TrimSpace(str(args["agent"]))

	var sessionID string
	switch {
	case agentName != "":
		if s.registry == nil {
			return mcp.NewToolResultError("agent registry unavailable"), nil
		}
		def := s.registry.Def(agentName)
		if def == nil {
			// Explicit not-found — do not substitute another agent's transcript.
			return mcp.NewToolResultError(fmt.Sprintf("agent %q not found", agentName)), nil
		}
		sessionID = strings.TrimSpace(def.SessionID)
		if sessionID == "" {
			// Explicit empty — no silent substitution of caller/overseer.
			return mcp.NewToolResultText(fmt.Sprintf(
				"agent %q has no session yet (empty transcript).", agentName)), nil
		}
	default:
		if s.transcript.GetID == nil {
			return mcp.NewToolResultText("No active Jevon session."), nil
		}
		sessionID = strings.TrimSpace(s.transcript.GetID())
		if sessionID == "" {
			return mcp.NewToolResultText("No active Jevon session."), nil
		}
	}

	turns, err := s.transcript.Read(sessionID)
	if err != nil {
		if agentName != "" {
			// Named agent: surface not-found/empty explicitly; never retry GetID.
			return mcp.NewToolResultError(fmt.Sprintf(
				"agent %q transcript not found (session %s): %v",
				agentName, sessionDisplay(sessionID), err)), nil
		}
		return mcp.NewToolResultError(fmt.Sprintf("read failed: %v", err)), nil
	}
	if len(turns) == 0 {
		if agentName != "" {
			return mcp.NewToolResultText(fmt.Sprintf(
				"agent %q transcript is empty.", agentName)), nil
		}
		return mcp.NewToolResultText("Transcript is empty."), nil
	}

	var b strings.Builder
	if agentName != "" {
		fmt.Fprintf(&b, "agent=%s session=%s turns=%d\n",
			agentName, sessionDisplay(sessionID), len(turns))
	}
	for i, turn := range turns {
		role, _ := turn["role"].(string)
		text, _ := turn["text"].(string)
		fmt.Fprintf(&b, "Turn %d [%s]: %s\n", i+1, role, truncate(text, 200))
	}
	return mcp.NewToolResultText(b.String()), nil
}

func (s *Server) handleTranscriptRewind(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	turnsF, _ := args["turns"].(float64)
	keepTurns := int(turnsF)

	sessionID := s.transcript.GetID()
	if sessionID == "" {
		return mcp.NewToolResultText("No active session to rewind."), nil
	}

	if err := s.transcript.Truncate(sessionID, keepTurns); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("rewind failed: %v", err)), nil
	}

	if keepTurns == 0 {
		return mcp.NewToolResultText("Truncated session to zero turns. Restart the Jevon agent to begin a fresh conversation."), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf("Rewound to %d turns. The truncated context will be used on the next message.", keepTurns)), nil
}

func truncate(s string, max int) string {
	if len(s) > max {
		return s[:max] + "\n... (truncated)"
	}
	return s
}

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

	"github.com/marcelocantos/jevons/internal/butler"
	"github.com/marcelocantos/jevons/internal/cli"
	"github.com/marcelocantos/jevons/internal/cost"
	"github.com/marcelocantos/jevons/internal/discovery"
	"github.com/marcelocantos/jevons/internal/doit"
	"github.com/marcelocantos/jevons/internal/eventlog"
	"github.com/marcelocantos/jevons/internal/rsi"
	"github.com/marcelocantos/jevons/internal/workers"
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

	// defaultProvider is the daemon-wide claudia backend for new agents
	// when agent_start / thread_spawn / jwork omit provider (🎯T148).
	// Empty means cli.ResolveProvider falls through to env / grok at use time.
	defaultProvider string

	// rsiLoop is the residual phrase/eventlog mint path (🎯T92; opt-in product residual).
	// Nil until SetRSILoop; jevons_rsi_cycle requires it. Product path is rsiCoach (🎯T243).
	rsiLoop *rsi.Loop

	// rsiCoach posts judgments to the overseer; never files bullseye (🎯T243).
	// Nil until SetRSICoach.
	rsiCoach *rsi.Coach

	// idleActivity tracks ACP phase for enter-idle detection (🎯T207).
	// Nil until StartIdleNudgeLoop; broadcastAgentEvent Observes transitions.
	idleActivity *IdleActivityTracker
	// idleNudgeLedger legacy path (auto-nudge ladder retired); may be nil.
	idleNudgeLedger *IdleNudgeLedger
	// idleEventLast debounces worker-idle events per agent name.
	idleEventLast map[string]time.Time
	// idleNudgeSweep is set by StartIdleNudgeLoop for cockpit fleet health
	// (dead-handle sweep only — no auto-continue ladder).
	idleNudgeSweep func(postRestart bool)
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
				mcp.WithDescription("Read the Jevon conversation transcript. Returns an array of turns with role and text."),
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

func (s *Server) handleTranscriptRead(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	sessionID := s.transcript.GetID()
	if sessionID == "" {
		return mcp.NewToolResultText("No active Jevon session."), nil
	}
	turns, err := s.transcript.Read(sessionID)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("read failed: %v", err)), nil
	}
	if len(turns) == 0 {
		return mcp.NewToolResultText("Transcript is empty."), nil
	}

	var b strings.Builder
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

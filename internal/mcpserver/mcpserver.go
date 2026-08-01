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

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/butler"
	"github.com/marcelocantos/jevons/internal/cost"
	"github.com/marcelocantos/jevons/internal/discovery"
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

	mu           sync.Mutex
	notifyJevon  NotifyFunc
	costSnapshot func() (*cost.Snapshot, error)

	// grokRun shells out to the Grok CLI for mid-session MCP reconnect (🎯T60).
	// Nil uses defaultGrokRun (exec of grok on PATH). Tests inject a fake.
	grokRun grokRunFunc
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

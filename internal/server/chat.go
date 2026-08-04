// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/agenterr"
)

// defaultOverseerName is the fallback registry name of the persistent
// overseer process backing /ws/chat; config overrides it (🎯T44).
const defaultOverseerName = "jevons"

// historyReplayTurns caps how many recent turns /ws/chat replays on
// connect (🎯T57). Older turns are fetched on demand via /api/history.
const historyReplayTurns = 30

// handleHistory serves older journal lines for "load earlier" paging
// (🎯T57): GET /api/history?end=<idx>&limit=<n> returns the window
// [end-limit, end) as a JSON array of raw wire frames, plus the new
// window start and the total line count. The client prepends them.
func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	clog := s.chatLog
	s.mu.RUnlock()
	if clog == nil {
		http.Error(w, `{"error":"no history"}`, http.StatusServiceUnavailable)
		return
	}
	end, _ := strconv.Atoi(r.URL.Query().Get("end"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 2000 {
		limit = 200
	}
	lines, total, err := clog.ReadRange(end-limit, end)
	if err != nil {
		http.Error(w, `{"error":"history read failed"}`, http.StatusInternalServerError)
		return
	}
	raw := make([]json.RawMessage, len(lines))
	for i, ln := range lines {
		raw[i] = json.RawMessage(ln)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"start": end - len(lines), "total": total, "lines": raw,
	})
}

// SetProcess attaches the persistent Claude process for the /ws/chat endpoint.
func (s *Server) SetProcess(proc *claudia.Agent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.proc = proc
}

// CurrentProcess returns the live overseer process (nil if none).
func (s *Server) CurrentProcess() *claudia.Agent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.proc
}

// waitForOverseer holds an open /ws/chat connection after history was
// already sent while the overseer is dead. Serves pings so the browser
// watchdog stays quiet, rejects chat turns with a short error, and
// returns a live process once AttachOverseer has run — or nil if the
// client disconnects. Must not Close the socket on "still down"; that
// path caused reconnect storms that wiped and re-hydrated the transcript.
func (s *Server) waitForOverseer(ctx context.Context, conn *websocket.Conn) *claudia.Agent {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	type readResult struct {
		data []byte
		err  error
	}
	reads := make(chan readResult, 1)
	go func() {
		for {
			_, data, err := conn.Read(ctx)
			select {
			case reads <- readResult{data: data, err: err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if cur := s.CurrentProcess(); cur != nil && cur.Alive() {
				slog.Info("chat: overseer recovered; attaching live stream")
				payload, _ := json.Marshal(map[string]string{
					"type": "status", "text": "overseer is back",
				})
				wctx, cancel := context.WithTimeout(ctx, 2*time.Second)
				_ = conn.Write(wctx, websocket.MessageText, payload)
				cancel()
				return cur
			}
		case rr := <-reads:
			if rr.err != nil {
				return nil
			}
			msg := strings.TrimSpace(string(rr.data))
			if msg == "" {
				continue
			}
			if msg == `{"type":"ping"}` {
				wctx, cancel := context.WithTimeout(ctx, 2*time.Second)
				_ = conn.Write(wctx, websocket.MessageText, []byte(`{"type":"pong"}`))
				cancel()
				continue
			}
			// Still down: do not drop the socket. Nack chat/control so the
			// owner sees a stable history instead of a reconnect flicker.
			s.mu.RLock()
			reason := s.overseerDownReason
			s.mu.RUnlock()
			if reason == "" {
				reason = "the overseer is not running"
			}
			payload, _ := json.Marshal(map[string]string{"type": "error", "error": reason})
			wctx, cancel := context.WithTimeout(ctx, 2*time.Second)
			_ = conn.Write(wctx, websocket.MessageText, payload)
			cancel()
		}
	}
}

// AttachOverseer makes agent the current overseer and (re)subscribes its
// event stream to the chat broadcast + status handler. It is called at
// startup and again after a rewind swaps the process — so every overseer
// reference resolves indirectly through s.proc and stays correct across
// the swap, and no /ws/chat connection is left holding a dead handle.
//
// Idempotent on re-attach (🎯T210): prior DeliverOverseerEvent subscription
// is removed before adding a new one. Without this, rewind + cockpit
// AttachOverseer on the same process stacked two fans and every assistant
// token was journaled/broadcast twice (GotGot / CheckingChecking).
func (s *Server) AttachOverseer(agent *claudia.Agent) {
	if agent == nil {
		return
	}
	s.mu.Lock()
	if s.overseerEventSub != 0 && s.proc != nil {
		s.proc.UnsubscribeEvents(s.overseerEventSub)
		s.overseerEventSub = 0
	}
	s.proc = agent
	s.overseerEventSub = agent.SubscribeEvents(s.DeliverOverseerEvent)
	s.mu.Unlock()
}

// ensureOverseerStreamIDLocked returns the open response stream id, minting
// one if needed (🎯T223). Caller must hold s.mu.
func (s *Server) ensureOverseerStreamIDLocked() string {
	if s.overseerStreamID == "" {
		s.overseerStreamSeq++
		s.overseerStreamID = "s" + strconv.FormatUint(s.overseerStreamSeq, 10)
	}
	return s.overseerStreamID
}

// clearOverseerStreamID drops the open stream label after a terminal stop.
func (s *Server) clearOverseerStreamID() {
	s.mu.Lock()
	s.overseerStreamID = ""
	s.mu.Unlock()
}

// DeliverOverseerEvent is the live event path for the overseer: normalise
// to the chat wire shape, broadcast to /ws/chat listeners, then update
// turn/idle status. Extracted so tests can drive the same path without
// a live claudia.Agent.
func (s *Server) DeliverOverseerEvent(ev claudia.Event) {
	// Any ACP traffic resets stuck-busy idle (🎯T204).
	s.NoteOverseerProgress()
	// Normalise ACP/raw provider events into the stable chat wire
	// shape the web UI understands (🎯T39). Raw ACP payloads have
	// no type/message.content, so a pass-through leaves the
	// working indicator stuck forever.
	// 🎯T223: stamp stream_id on assistant/progress fragments so journal
	// and client join by identity across interleave (not adjacency).
	var streamID string
	if ev.Type == "assistant" || ev.Type == "progress" {
		s.mu.Lock()
		streamID = s.ensureOverseerStreamIDLocked()
		s.mu.Unlock()
	}
	if line, ok := chatWireLine(ev); ok {
		if streamID != "" {
			line = stampStreamID(line, streamID)
		}
		s.BroadcastChat(line)
	}
	if ev.IsTerminalStop() {
		s.clearOverseerStreamID()
	}
	s.HandleAgentEvent(ev)
}

// SendToOverseer delivers text to the current overseer process.
func (s *Server) SendToOverseer(text string) error {
	// The owner talking to Jevons is the strongest owner-present signal —
	// feed the budget dead-man's switch so it never stops a fleet the
	// owner is actively directing.
	if s.activityHook != nil {
		s.activityHook()
	}
	// Queue rather than send directly: the overseer's ACP session handles
	// one prompt at a time, so a note delivered while it is mid-turn returns
	// "prompt already in flight". Historically that error was logged and the
	// note DROPPED — silently losing worker replies (🎯T62). Enqueue and try
	// to drain; anything not delivered now is retried on the next
	// turn-complete (HandleAgentEvent).
	s.mu.Lock()
	s.notifyQueue = append(s.notifyQueue, text)
	depth := len(s.notifyQueue)
	s.mu.Unlock()
	// 🎯T128.3: enqueue is Info so queue growth is visible at production default.
	slog.Info("notify_queue",
		"component", "notify_queue",
		"decision", "enqueue",
		"depth", depth,
	)
	s.drainOverseerNotes()
	return nil
}

// sendNotes delivers one coalesced note batch to the overseer. The
// notifySender seam lets tests stub delivery; nil uses the live process.
func (s *Server) sendNotes(text string) error {
	if s.notifySender != nil {
		return s.notifySender(text)
	}
	proc := s.CurrentProcess()
	if proc == nil || !proc.Alive() {
		return fmt.Errorf("overseer not running")
	}
	return proc.Send(text)
}

// notifyErrClass classifies overseer-notify delivery failures for structured
// logs (🎯T128.3). Stable short codes for rg / dashboards.
// Busy uses agenterr.IsPromptBusy so Claude/Task strings classify like Grok ACP (🎯T214 J6).
func notifyErrClass(err error) string {
	if err == nil {
		return ""
	}
	if agenterr.IsPromptBusy(err) {
		return "busy"
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "overseer not running"):
		return "not_running"
	default:
		return "other"
	}
}

// drainOverseerNotes attempts to deliver all queued async notifications to
// the overseer as a single coalesced prompt. If the overseer is busy
// ("prompt already in flight") or otherwise unreachable, the batch is
// requeued at the front and left for the next turn-complete to retry —
// nothing is dropped. A drain-in-progress guard serializes concurrent
// callers (a worker reply and a turn-complete can race). Called from
// SendToOverseer (new note) and HandleAgentEvent (overseer went idle).
func (s *Server) drainOverseerNotes() {
	s.mu.Lock()
	if s.notifyDraining || len(s.notifyQueue) == 0 {
		s.mu.Unlock()
		return
	}
	batch := s.notifyQueue
	s.notifyQueue = nil
	s.notifyDraining = true
	s.mu.Unlock()

	err := s.sendNotes(strings.Join(batch, "\n\n"))

	s.mu.Lock()
	s.notifyDraining = false
	if err != nil {
		// Overseer busy or down — put the batch back at the front so order
		// is preserved, and wait for the next turn-complete to retry.
		s.notifyQueue = append(batch, s.notifyQueue...)
		depth := len(s.notifyQueue)
		s.mu.Unlock()
		// 🎯T128.3: busy-defer must be Info (not Debug) with depth + err_class.
		slog.Info("notify_queue",
			"component", "notify_queue",
			"decision", "defer",
			"depth", depth,
			"deferred", len(batch),
			"err_class", notifyErrClass(err),
			"err", err,
		)
		return
	}
	// Successful prompt delivery: mark waiting so stuck-busy can see an
	// in-flight turn even when only notify/owner notes are on the wire.
	s.waiting = true
	s.overseerLastProgress = time.Now()
	depth := len(s.notifyQueue)
	s.mu.Unlock()
	slog.Info("notify_queue",
		"component", "notify_queue",
		"decision", "drain",
		"depth", depth,
		"drained", len(batch),
	)
}

// handleCost serves the live cost snapshot: burn-rates, the "what is
// burning right now" view, and any tripped runaway signals.
// When the budget clamp is off (budget.json disabled=true), the source
// returns {"disabled":true,"accounting":"disabled"} so the UI can hide
// dollar figures without treating it as an error (🎯T137).
func (s *Server) handleCost(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if s.costSource == nil {
		// Unset source: subsystem never wired (or usage.db failed open).
		w.Write([]byte(`{"disabled":true,"accounting":"disabled","billable":false,"error":"cost monitoring not enabled"}`))
		return
	}
	snap := s.costSource()
	if snap == nil {
		w.Write([]byte(`{"error":"no snapshot yet"}`))
		return
	}
	if err := json.NewEncoder(w).Encode(snap); err != nil {
		slog.Warn("encode cost snapshot", "err", err)
	}
}

// rewindStrategy selects how product rewind rebinds the overseer session
// after the durable chatlog is truncated (🎯T214 J5).
//
//   - rewindNativeJSONL: Claude Session — claudia.RewindSession truncates the
//     provider JSONL; same SessionID is resumed (not a silent Grok-style rotate).
//   - rewindRotateRecap: Grok ACP and other providers without in-place session
//     truncate — journal-first rotate onto a fresh SessionID + chatlog recap.
type rewindStrategy int

const (
	rewindRotateRecap rewindStrategy = iota
	rewindNativeJSONL
)

// rewindStrategyForProvider is the hermetic policy oracle for 🎯T214 J5.
// Only Claude Session (and empty, which claudia treats as Claude) uses
// native JSONL rewind. Grok/Codex/Bedrock/unknown must not silently apply
// Claude JSONL rules — rotate+recap is the honest product path.
func rewindStrategyForProvider(p claudia.Provider) rewindStrategy {
	switch p {
	case claudia.ProviderClaude, "":
		return rewindNativeJSONL
	default:
		return rewindRotateRecap
	}
}

// RewindOverseer rolls the Jevons conversation back n user turns and
// resumes the overseer. Always journal-first (🎯T52): truncate the durable
// chatlog, stop the live process, then rebind the session:
//
//   - Claude (🎯T214): claudia.RewindSession on the same SessionID, then Launch
//     with Materialized/RequireResume. No silent Grok rotate applied wrongly.
//     Falls back to rotate+recap if native JSONL rewind fails (missing file,
//     wrong turn count) — failure is logged, not mislabeled as Grok-only success.
//   - Grok / others: rotate SessionID + seed recap (Grok ACP cannot truncate
//     in place). Residual: chatlog turn count vs Claude JSONL turn boundaries
//     may diverge when both are used; product n is chatlog turns.
func (s *Server) RewindOverseer(n int) error {
	if n < 1 {
		return fmt.Errorf("rewind: turns must be >= 1")
	}
	s.mu.RLock()
	reg := s.registry
	clog := s.chatLog
	s.mu.RUnlock()
	if reg == nil {
		return fmt.Errorf("rewind: no registry")
	}
	if clog == nil {
		return fmt.Errorf("rewind: no chat log")
	}
	def := reg.Def(s.overseerName)
	if def == nil {
		return fmt.Errorf("rewind: overseer not registered")
	}

	if err := clog.TruncateTurns(n); err != nil {
		return fmt.Errorf("rewind: %w", err)
	}

	reg.Stop(s.overseerName)
	// Stop clears ConnectURL/PID on the registry copy, but `def` was
	// snapshotted before Stop. Re-registering that snapshot re-persisted
	// the dead serve endpoint and forced Launch into reattach → connection
	// reset (🎯T204). Always clear connect endpoint before re-register.
	next := clearConnectEndpoint(*def)
	strategy := rewindStrategyForProvider(def.Provider)
	injectRecap := true

	if strategy == rewindNativeJSONL {
		if _, err := claudia.RewindSession(def.SessionID, def.WorkDir, n); err != nil {
			slog.Warn("rewind: Claude native JSONL rewind failed; falling back to rotate+recap",
				"provider", def.Provider, "session", def.SessionID, "err", err)
			// Fall through to rotate path.
			strategy = rewindRotateRecap
		} else {
			// Same session id, still materialized; resume the truncated JSONL.
			next.Materialized = true
			injectRecap = false
			slog.Info("rewind: Claude native JSONL path",
				"provider", def.Provider, "session", next.SessionID, "turns", n)
		}
	}

	if strategy == rewindRotateRecap {
		// Grok ACP (and fallback): cannot truncate the live session in place.
		next.SessionID = uuid.NewString()
		next.Materialized = false
		slog.Info("rewind: rotate+recap path",
			"provider", def.Provider, "new_session", next.SessionID, "turns", n)
	}

	if err := reg.Register(next); err != nil {
		return fmt.Errorf("rewind: register session: %w", err)
	}
	agent, lerr := reg.Launch(s.overseerName)
	if lerr != nil {
		// Leave stopped + clean def; cockpit converge (🎯T204) will retry.
		s.SetOverseerDownReason("rewind relaunch failed: " + lerr.Error())
		s.NotifyAgentsChanged()
		return fmt.Errorf("rewind: relaunch failed: %w", lerr)
	}
	s.AttachOverseer(agent)
	s.SetOverseerDownReason("")

	if injectRecap {
		if recap := clog.Recap(30, 6<<10); recap != "" {
			go func() {
				if err := s.SendToOverseer(
					"[Conversation rewound by the owner. The record below is the surviving context — read it, then acknowledge in ONE short sentence.]\n\n" + recap); err != nil {
					slog.Error("rewind recap send failed", "err", err)
				}
			}()
		}
	} else {
		// Native path: model already has truncated transcript; short ack only.
		go func() {
			if err := s.SendToOverseer(
				"[Conversation rewound by the owner — your session transcript was truncated in place. Acknowledge in ONE short sentence.]"); err != nil {
				slog.Error("rewind ack send failed", "err", err)
			}
		}()
	}
	return nil
}

// SetRegistry attaches the agent registry for the /api/agents endpoint.
func (s *Server) SetRegistry(reg *claudia.Registry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.registry = reg
}

// NotifyAgentsChanged pushes a non-journaled live frame so the RHS fleet
// panel refreshes without waiting on poll (🎯T82). Safe from any goroutine.
func (s *Server) NotifyAgentsChanged() {
	payload, err := json.Marshal(map[string]any{"type": "agents_changed"})
	if err != nil {
		return
	}
	s.broadcastChatLive(string(payload))
}

// GetAgent looks up a worker by name in the registry. Returns nil if
// no registry is attached or no such name is registered (or the
// registered agent has not been launched yet).
func (s *Server) GetAgent(name string) *claudia.Agent {
	s.mu.RLock()
	reg := s.registry
	s.mu.RUnlock()
	if reg == nil {
		return nil
	}
	return reg.Get(name)
}

// RegistryAgents returns the registered agent definitions. Used by
// the voice overseer to enumerate delegation targets.
func (s *Server) RegistryAgents() []claudia.AgentDef {
	s.mu.RLock()
	reg := s.registry
	s.mu.RUnlock()
	if reg == nil {
		return nil
	}
	return reg.List()
}

// agentInfo is the GET /api/agents JSON row consumed by the RHS fleet panel.
// 🎯T72.1: completeness is a server feed concern — every registry agent
// (durable or ephemeral child) must appear while registered. Parent comes
// from claudia AgentDef lineage (kill auth / 🎯T68 tree). Purpose (🎯T114)
// distinguishes work vs aside so UI can chrome asides without a second store.
// Phase/Step/Progress (🎯T118) are glanceable activity for worker secondary
// lines — filled from ACP progress when known, else status baseline.
type agentInfo struct {
	Name        string `json:"name"`
	WorkDir     string `json:"workdir"`
	Parent      string `json:"parent,omitempty"`
	Purpose     string `json:"purpose,omitempty"`
	Description string `json:"description,omitempty"`
	// TargetID is the bullseye target this agent is engaged on (🎯T198).
	// Empty when not mission-bound. UI merges with /api/frontier by equality.
	TargetID string `json:"target_id,omitempty"`
	Status   string `json:"status"`
	Phase    string `json:"phase,omitempty"`
	Step     string `json:"step,omitempty"`
	Progress string `json:"progress,omitempty"`
}

// listFleetAgents returns the RHS panel source of truth: every agent
// definition currently in the registry, with live status from the process
// map. No filtering by AutoStart or top-level name — PO-spawned children
// and other ephemeral fleet entries appear while they remain registered.
// Order is name-sorted so polls do not reshuffle solely from map iteration.
//
// 🎯T85: before listing, clear/rehydrate dead process handles so the panel
// never shows a zombie "running" hope for a silently dead worker. When any
// recovery runs, NotifyAgentsChanged pushes a live frame so the UI refreshes
// immediately (not only on the next poll).
func listFleetAgents(reg *claudia.Registry) []agentInfo {
	return listFleetAgentsNotifying(reg, nil, nil)
}

// listFleetAgentsNotifying is the same feed with an optional notify hook for
// recovery events (server wires agents_changed). Used by hermetic tests with
// notify=nil. progress may be nil (no ACP snapshots).
func listFleetAgentsNotifying(reg *claudia.Registry, onRecovered func(names []string), progress *AgentProgressHub) []agentInfo {
	if reg == nil {
		return []agentInfo{}
	}
	var recovered []string
	// Inline silent-death policy (same as mcpserver.deadRecoveryPlan) so the
	// HTTP feed path does not import mcpserver (cycle). Keep in sync.
	for _, d := range reg.List() {
		proc := reg.Get(d.Name)
		if proc == nil || proc.Alive() {
			continue
		}
		if d.AutoStart {
			if _, err := reg.Launch(d.Name); err != nil {
				reg.Stop(d.Name)
			} else {
				recovered = append(recovered, d.Name)
			}
		} else {
			reg.Stop(d.Name)
			recovered = append(recovered, d.Name) // cleared → stopped, still surface
		}
	}
	if len(recovered) > 0 && onRecovered != nil {
		onRecovered(recovered)
	}
	defs := reg.List()
	agents := make([]agentInfo, 0, len(defs))
	for _, d := range defs {
		status := "stopped"
		if proc := reg.Get(d.Name); proc != nil && proc.Alive() {
			status = "running"
		}
		purpose := d.Purpose
		if purpose == "" {
			purpose = claudia.PurposeWork
		}
		// Seed status baseline when no richer ACP snapshot exists yet.
		if progress != nil {
			progress.SetStatus(d.Name, status)
		}
		info := agentInfo{
			Name:        d.Name,
			WorkDir:     d.WorkDir,
			Parent:      d.Parent,
			Purpose:     purpose,
			Description: d.Description,
			TargetID:    strings.TrimSpace(d.TargetID),
			Status:      status,
		}
		if progress != nil {
			if p := progress.Get(d.Name); p.Summary != "" || p.Phase != "" || p.Step != "" {
				info.Phase = p.Phase
				info.Step = p.Step
				info.Progress = p.Summary
			}
		}
		agents = append(agents, info)
	}
	sort.Slice(agents, func(i, j int) bool {
		return agents[i].Name < agents[j].Name
	})
	return agents
}

// ObserveAgentProgress records an ACP/tool event for name and returns whether
// the glanceable summary changed (callers push agents_changed on true).
func (s *Server) ObserveAgentProgress(name string, ev claudia.Event) bool {
	if s == nil {
		return false
	}
	if s.agentProgress == nil {
		s.agentProgress = NewAgentProgressHub()
	}
	return s.agentProgress.Observe(name, ev)
}

// handleListAgents returns all registered fleet agents with status (🎯T72.1).
// 🎯T85: recovery during list triggers agents_changed so the RHS updates.
func (s *Server) handleListAgents(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	reg := s.registry
	s.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	if reg == nil {
		_ = json.NewEncoder(w).Encode([]agentInfo{})
		return
	}
	agents := listFleetAgentsNotifying(reg, func(names []string) {
		// 🎯T85: push UI refresh + optional client-visible signal after recovery.
		s.NotifyAgentsChanged()
	}, s.agentProgress)
	_ = json.NewEncoder(w).Encode(agents)
}

// handleChat is a direct WebSocket ↔ Claude PTY bridge.
// Client sends plain text messages, server sends raw JSONL lines.
func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, wsAcceptOptions())
	if err != nil {
		slog.Error("chat: accept failed", "err", err)
		return
	}
	defer conn.CloseNow()

	conn.SetReadLimit(1 << 20)
	ctx := r.Context()

	// 🎯T140: connection lifecycle spans — conn_id + concurrent + replay timing.
	connID := uuid.NewString()
	s.mu.Lock()
	s.chatConns++
	concurrent := s.chatConns
	proc := s.proc
	clog := s.chatLog
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		if s.chatConns > 0 {
			s.chatConns--
		}
		left := s.chatConns
		s.mu.Unlock()
		s.LogEvent("chat_conn", "close", map[string]any{
			"conn_id":    connID,
			"concurrent": left,
		})
		slog.Info("chat client disconnected", "conn_id", connID, "concurrent", left)
	}()

	s.LogEvent("chat_conn", "open", map[string]any{
		"conn_id":    connID,
		"concurrent": concurrent,
	})
	slog.Info("chat client connected", "conn_id", connID, "concurrent", concurrent)

	// First frame: client attaches conn_id before history firehose (🎯T140).
	{
		hello, _ := json.Marshal(map[string]any{
			"type":       "conn",
			"conn_id":    connID,
			"concurrent": concurrent,
		})
		wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		_ = conn.Write(wctx, websocket.MessageText, hello)
		cancel()
	}

	// Replay history BEFORE the liveness check: the jevons-owned chat log
	// is the durable record (🎯T30.1), and a dead overseer process must
	// not blank the conversation — completed turns always come back.
	//
	// Replay is CAPPED to the most recent turns (🎯T57): shipping the whole
	// journal on every reconnect is the dominant load cost on a long
	// history. Older turns stay in the durable log and are fetched on
	// demand via GET /api/history ("load earlier"); a history_meta frame
	// tells the client how many older lines exist.
	if clog != nil {
		replayStart := time.Now()
		var frames int
		var bytes int
		// 🎯T142: sealed-turn replay — merge token stream into one frame/turn.
		start, total, err := clog.ReplayTailSealed(historyReplayTurns, func(line string) error {
			frames++
			bytes += len(line) + 1
			writeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			return conn.Write(writeCtx, websocket.MessageText, []byte(line))
		})
		replayMS := time.Since(replayStart).Milliseconds()
		fields := map[string]any{
			"conn_id":    connID,
			"concurrent": concurrent,
			"frames":     frames,
			"bytes":      bytes,
			"ms":         replayMS,
			"older":      start,
			"total":      total,
			"sealed":     true, // 🎯T142
		}
		if err != nil {
			fields["err"] = err.Error()
			s.LogEvent("chat_conn", "replay_error", fields)
			slog.Warn("chat: chatlog replay failed", "conn_id", connID, "frames", frames, "bytes", bytes, "ms", replayMS, "err", err)
			payload, _ := json.Marshal(map[string]string{
				"type":  "error",
				"error": "history replay incomplete: " + err.Error(),
			})
			writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			_ = conn.Write(writeCtx, websocket.MessageText, payload)
			cancel()
		} else {
			s.LogEvent("chat_conn", "replay", fields)
			slog.Info("chat: chatlog replay", "conn_id", connID, "frames", frames, "bytes", bytes, "ms", replayMS, "older", start, "total", total)
			// Always emit history_meta (even when older==0) so the client can
			// close the connect span without waiting for idle timeout.
			meta, _ := json.Marshal(map[string]any{
				"type": "history_meta", "older": start, "total": total, "start": start,
				"conn_id": connID, "replay_frames": frames, "replay_bytes": bytes, "replay_ms": replayMS,
			})
			writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			_ = conn.Write(writeCtx, websocket.MessageText, meta)
			cancel()
		}
	} else if proc != nil {
		// Legacy fallback: provider JSONL (empty for Grok ACP).
		sendHistory(conn, ctx, proc.JSONLPath())
	}

	if proc == nil || !proc.Alive() {
		// Overseer is down (budget pause, crash, missing Grok CLI, …).
		// History was already replayed above — keep the socket OPEN so the
		// resilient browser transport does not thrash reconnect → wipe DOM
		// → re-replay ~10k stream frames → close (transcript flicker).
		// 🎯T54 still surfaces a legible error; recovery waits for AttachOverseer.
		s.mu.RLock()
		reason := s.overseerDownReason
		s.mu.RUnlock()
		if reason == "" {
			reason = "the overseer is not running"
		}
		payload, _ := json.Marshal(map[string]string{"type": "error", "error": reason})
		wctx, wcancel := context.WithTimeout(ctx, 5*time.Second)
		_ = conn.Write(wctx, websocket.MessageText, payload)
		wcancel()
		s.LogEvent("chat_conn", "degraded", map[string]any{
			"conn_id": connID, "reason": reason, "concurrent": concurrent,
		})
		slog.Info("chat: overseer down; holding connection after history replay", "conn_id", connID, "reason", reason)
		proc = s.waitForOverseer(ctx, conn)
		if proc == nil {
			return
		}
		// Fall through to subscribe on the recovered process.
	}

	// Subscribe to live JSONL events from the Claude process.
	ch := make(chan string, 256)
	s.mu.Lock()
	s.chatListeners = append(s.chatListeners, ch)
	s.mu.Unlock()

	defer func() {
		// 🎯T209: drop inspect multiplex subscription with the chat listener.
		s.clearInspectSub(ch)
		s.mu.Lock()
		for i, l := range s.chatListeners {
			if l == ch {
				s.chatListeners = append(s.chatListeners[:i], s.chatListeners[i+1:]...)
				break
			}
		}
		s.mu.Unlock()
	}()

	// Server → Client: forward raw JSONL lines.
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case line := <-ch:
				writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				if err := conn.Write(writeCtx, websocket.MessageText, []byte(line)); err != nil {
					cancel()
					return
				}
				cancel()
			}
		}
	}()

	// Client → Server: read messages and send to Claude PTY.
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		msg := strings.TrimSpace(string(data))
		if msg == "" {
			continue
		}
		// Heartbeat ping from the resilient browser transport. Echo
		// a pong so the watchdog stays quiet; do NOT forward to
		// Claude (it would be parsed as a chat turn).
		if msg == `{"type":"ping"}` {
			writeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			_ = conn.Write(writeCtx, websocket.MessageText, []byte(`{"type":"pong"}`))
			cancel()
			continue
		}
		// Interrupt control frame (sent by the client on Esc). A literal
		// "stop" is NOT special — it's a normal thing to say to Jevons.
		// Use the CURRENT process, not the one captured at connect, so a
		// rewind swap mid-connection is transparent.
		if msg == `{"type":"interrupt"}` {
			slog.Info("chat: interrupt")
			if cur := s.CurrentProcess(); cur != nil {
				if err := cur.Interrupt(); err != nil {
					slog.Error("chat: interrupt failed", "err", err)
				}
			}
			continue
		}

		// Control frames are JSON objects with a "type" field. Match on the
		// unmarshalled type — never HasPrefix on key order (Go map marshal and
		// probes may emit {"name":…,"type":"inspect_subscribe"} which must not
		// fall through into owner chat / overseer as a user turn) (🎯T209).
		if strings.HasPrefix(msg, "{") {
			var ctl struct {
				Type  string `json:"type"`
				Name  string `json:"name"`
				Turns int    `json:"turns"`
			}
			if err := json.Unmarshal([]byte(msg), &ctl); err == nil && ctl.Type != "" {
				switch ctl.Type {
				case "rewind":
					// Roll conversation back N user turns; tell clients to trim.
					slog.Info("chat: rewind", "turns", ctl.Turns)
					if err := s.RewindOverseer(ctl.Turns); err != nil {
						slog.Error("chat: rewind failed", "err", err)
						continue
					}
					s.broadcastChatLive(fmt.Sprintf(`{"type":"rewound","turns":%d}`, ctl.Turns))
					continue
				case "inspect_subscribe", "inspect_unsubscribe":
					// 🎯T209: RHS agent inspect multiplex on /ws/chat.
					name := strings.TrimSpace(ctl.Name)
					if ctl.Type == "inspect_unsubscribe" || name == "" {
						s.setInspectSub(ch, "")
						slog.Info("chat: inspect unsubscribe")
						continue
					}
					s.setInspectSub(ch, name)
					slog.Info("chat: inspect subscribe", "name", name)
					if line, ok := s.marshalAgentTranscriptHistory(name); ok {
						writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
						_ = conn.Write(writeCtx, websocket.MessageText, []byte(line))
						cancel()
					} else {
						payload, _ := json.Marshal(map[string]any{
							"type":  "agent_transcript",
							"kind":  inspectKindHistory,
							"name":  name,
							"turns": []any{},
							"empty": true,
							"error": "agent not found",
						})
						writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
						_ = conn.Write(writeCtx, websocket.MessageText, payload)
						cancel()
					}
					continue
				}
			}
		}

		slog.Info("chat: received", "msg", msg)
		// Echo the clean owner turn into the transcript as the single source
		// of the owner bubble; the ACP echo of the same turn (which arrives
		// prefixed) is dropped by chatWireLine to avoid a duplicate.
		s.BroadcastChat(chatUserEcho(msg))
		// Deliver to the overseer with the userTurnPrefix marker so the wire
		// layer can tell owner turns from injected notifications, and the
		// overseer can relay per the owner's instructions (🎯T63).
		if err := s.SendToOverseer(userTurnPrefix + msg); err != nil {
			// A refused/failed send must be visible on the wire, not just
			// in the server log (🎯T49; live drill found "prompt already
			// in flight" vanishing silently). Broadcast so every client —
			// and the journal — records that this turn was not delivered.
			// 🎯T237: structured failure_class + owner copy beyond bare Internal error.
			class, ownerMsg := agenterr.ClassifyAndFormat(err)
			if !class.IsFailure() {
				ownerMsg = err.Error()
			}
			slog.Error("chat: send to overseer failed",
				"err", err,
				"failure_class", class.String(),
				"transient", class.IsTransient(),
			)
			frame := map[string]string{
				"type":  "error",
				"error": "message not delivered: " + ownerMsg,
			}
			if class.IsFailure() {
				frame["failure_class"] = class.String()
			}
			payload, _ := json.Marshal(frame)
			s.BroadcastChat(string(payload))
		}
	}
}

// sendHistory reads the JSONL file and sends each line as a raw WebSocket
// message. Lines are read with bufio.Reader.ReadBytes so a multi-megabyte
// tool_result line cannot silently truncate the rest of the transcript
// (T38 / Fable F5). A line that cannot be written is logged and stops
// the replay rather than dropping the remainder without notice.
func sendHistory(conn *websocket.Conn, ctx context.Context, path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	r := bufio.NewReaderSize(f, 1<<20)
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			// Strip trailing newline; empty lines are skipped.
			if line[len(line)-1] == '\n' {
				line = line[:len(line)-1]
			}
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
			if len(line) > 0 {
				writeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
				werr := conn.Write(writeCtx, websocket.MessageText, line)
				cancel()
				if werr != nil {
					slog.Warn("chat: history write failed", "path", path, "err", werr)
					return
				}
			}
		}
		if err == io.EOF {
			return
		}
		if err != nil {
			slog.Warn("chat: history read failed", "path", path, "err", err)
			// Surface a structured error so the client knows replay was incomplete.
			payload, _ := json.Marshal(map[string]string{
				"type":  "error",
				"error": "history replay incomplete: " + err.Error(),
			})
			writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			_ = conn.Write(writeCtx, websocket.MessageText, payload)
			cancel()
			return
		}
	}
}

// BroadcastChat sends a JSONL line to all /ws/chat listeners, appending
// it first to the durable jevons-owned chat log (🎯T30.1) so the exact
// stream clients render is what a reconnect replays. An append failure
// is loud — losing durability silently is the failure mode this exists
// to kill — but does not block the live broadcast.
func (s *Server) BroadcastChat(line string) {
	s.mu.Lock()
	clog := s.chatLog
	s.mu.Unlock()
	if clog != nil {
		if err := clog.Append(line); err != nil {
			slog.Error("chat: DURABILITY FAILURE — chat log append failed", "err", err)
		}
	}
	s.broadcastChatLive(line)
}

// broadcastChatLive fans a line out to connected clients WITHOUT
// journaling it. Only for frames whose durable effect is already in the
// journal (the rewound control frame after TruncateTurns — journaling it
// too would double-trim replayed views).
func (s *Server) broadcastChatLive(line string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ch := range s.chatListeners {
		select {
		case ch <- line:
		default:
		}
	}
}

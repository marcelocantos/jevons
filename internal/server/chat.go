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

// AttachOverseer makes agent the current overseer and (re)subscribes its
// event stream to the chat broadcast + status handler. It is called at
// startup and again after a rewind swaps the process — so every overseer
// reference resolves indirectly through s.proc and stays correct across
// the swap, and no /ws/chat connection is left holding a dead handle.
func (s *Server) AttachOverseer(agent *claudia.Agent) {
	s.SetProcess(agent)
	agent.SubscribeEvents(s.DeliverOverseerEvent)
}

// DeliverOverseerEvent is the live event path for the overseer: normalise
// to the chat wire shape, broadcast to /ws/chat listeners, then update
// turn/idle status. Extracted so tests can drive the same path without
// a live claudia.Agent.
func (s *Server) DeliverOverseerEvent(ev claudia.Event) {
	// Normalise ACP/raw provider events into the stable chat wire
	// shape the web UI understands (🎯T39). Raw ACP payloads have
	// no type/message.content, so a pass-through leaves the
	// working indicator stuck forever.
	if line, ok := chatWireLine(ev); ok {
		s.BroadcastChat(line)
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
	s.mu.Unlock()
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
	}
	s.mu.Unlock()
	if err != nil {
		slog.Debug("overseer busy; notes deferred to next turn", "deferred", len(batch), "err", err)
	}
}

// handleCost serves the live cost snapshot: burn-rates, the "what is
// burning right now" view, and any tripped runaway signals.
func (s *Server) handleCost(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if s.costSource == nil {
		w.Write([]byte(`{"error":"cost monitoring not enabled"}`))
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

// RewindOverseer rolls the Jevons conversation back n user turns and
// resumes it. claudia requires the live process be stopped first (a
// running claude holds the conversation in memory and would re-append
// the dropped turns), so this stops the overseer, truncates its
// transcript at a turn boundary, relaunches with --resume, and
// re-attaches the event stream. The rewind is undoable via the
// .rewind-bak sidecar claudia leaves behind. The overseer is always
// relaunched, even if the truncate fails, so the chat is never left dead.
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

	// A Grok ACP session cannot be truncated in place, so rewind is
	// journal-first (🎯T52): truncate the durable record, then rotate the
	// overseer onto a fresh session seeded with a recap of the trimmed
	// history. This rotation is specific to rewind — unlike routine boot,
	// which now RESUMES the real session (🎯T58): the overseer's MCP tools
	// come from ~/.grok/config.toml and reattach on session/load, so a
	// restart no longer needs to rotate-and-recap. Here we must rotate
	// because the whole point is to drop turns the live session still holds.
	if err := clog.TruncateTurns(n); err != nil {
		return fmt.Errorf("rewind: %w", err)
	}

	reg.Stop(s.overseerName)
	rotated := *def
	rotated.SessionID = uuid.NewString()
	rotated.Materialized = false
	if err := reg.Register(rotated); err != nil {
		return fmt.Errorf("rewind: rotate session: %w", err)
	}
	agent, lerr := reg.Launch(s.overseerName)
	if lerr != nil {
		return fmt.Errorf("rewind: relaunch failed: %w", lerr)
	}
	s.AttachOverseer(agent)

	if recap := clog.Recap(30, 6<<10); recap != "" {
		go func() {
			if err := s.SendToOverseer(
				"[Conversation rewound by the owner. The record below is the surviving context — read it, then acknowledge in ONE short sentence.]\n\n" + recap); err != nil {
				slog.Error("rewind recap send failed", "err", err)
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
// from claudia AgentDef lineage (kill auth / 🎯T68 tree).
type agentInfo struct {
	Name    string `json:"name"`
	WorkDir string `json:"workdir"`
	Parent  string `json:"parent,omitempty"`
	Status  string `json:"status"`
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
	return listFleetAgentsNotifying(reg, nil)
}

// listFleetAgentsNotifying is the same feed with an optional notify hook for
// recovery events (server wires agents_changed). Used by hermetic tests with
// notify=nil.
func listFleetAgentsNotifying(reg *claudia.Registry, onRecovered func(names []string)) []agentInfo {
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
		agents = append(agents, agentInfo{
			Name:    d.Name,
			WorkDir: d.WorkDir,
			Parent:  d.Parent,
			Status:  status,
		})
	}
	sort.Slice(agents, func(i, j int) bool {
		return agents[i].Name < agents[j].Name
	})
	return agents
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
	})
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
	slog.Info("chat client connected")

	s.mu.Lock()
	proc := s.proc
	clog := s.chatLog
	s.mu.Unlock()

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
		start, total, err := clog.ReplayTail(historyReplayTurns, func(line string) error {
			writeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			return conn.Write(writeCtx, websocket.MessageText, []byte(line))
		})
		if err != nil {
			slog.Warn("chat: chatlog replay failed", "err", err)
			payload, _ := json.Marshal(map[string]string{
				"type":  "error",
				"error": "history replay incomplete: " + err.Error(),
			})
			writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			_ = conn.Write(writeCtx, websocket.MessageText, payload)
			cancel()
		} else if start > 0 {
			meta, _ := json.Marshal(map[string]any{
				"type": "history_meta", "older": start, "total": total, "start": start,
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
		// Overseer is down (usually a missing/unauth Grok CLI on a fresh
		// install). Send a legible error frame the UI renders BEFORE the
		// socket closes, so a first-run stranger sees the cause and the
		// fix instead of a blank, silent chat (🎯T54).
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
		conn.Close(websocket.StatusInternalError, "overseer not running")
		return
	}

	// Subscribe to live JSONL events from the Claude process.
	ch := make(chan string, 256)
	s.mu.Lock()
	s.chatListeners = append(s.chatListeners, ch)
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		for i, l := range s.chatListeners {
			if l == ch {
				s.chatListeners = append(s.chatListeners[:i], s.chatListeners[i+1:]...)
				break
			}
		}
		s.mu.Unlock()
		slog.Info("chat client disconnected")
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

		// Rewind control frame: roll the conversation back N user turns
		// and resume. Tell all clients to trim their view to match.
		if strings.HasPrefix(msg, `{"type":"rewind"`) {
			var ctl struct {
				Turns int `json:"turns"`
			}
			_ = json.Unmarshal([]byte(msg), &ctl)
			slog.Info("chat: rewind", "turns", ctl.Turns)
			if err := s.RewindOverseer(ctl.Turns); err != nil {
				slog.Error("chat: rewind failed", "err", err)
				continue
			}
			s.broadcastChatLive(fmt.Sprintf(`{"type":"rewound","turns":%d}`, ctl.Turns))
			continue
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
			slog.Error("chat: send to overseer failed", "err", err)
			payload, _ := json.Marshal(map[string]string{
				"type":  "error",
				"error": "message not delivered: " + err.Error(),
			})
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

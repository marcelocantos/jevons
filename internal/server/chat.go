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
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/marcelocantos/claudia"
)

// defaultOverseerName is the fallback registry name of the persistent
// overseer process backing /ws/chat; config overrides it (🎯T44).
const defaultOverseerName = "jevons"

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
	proc := s.CurrentProcess()
	if proc == nil || !proc.Alive() {
		return fmt.Errorf("overseer not running")
	}
	return proc.Send(text)
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
	s.mu.RUnlock()
	if reg == nil {
		return fmt.Errorf("rewind: no registry")
	}
	def := reg.Def(s.overseerName)
	if def == nil {
		return fmt.Errorf("rewind: overseer not registered")
	}

	reg.Stop(s.overseerName)
	_, rerr := claudia.RewindSession(def.SessionID, def.WorkDir, n)

	agent, lerr := reg.Launch(s.overseerName)
	if lerr != nil {
		return fmt.Errorf("rewind: relaunch failed: %w", lerr)
	}
	s.AttachOverseer(agent)

	if rerr != nil {
		return fmt.Errorf("rewind: %w", rerr)
	}
	return nil
}

// SetRegistry attaches the agent registry for the /api/agents endpoint.
func (s *Server) SetRegistry(reg *claudia.Registry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.registry = reg
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

// handleListAgents returns all registered agents with their status.
func (s *Server) handleListAgents(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	reg := s.registry
	s.mu.RUnlock()

	if reg == nil {
		json.NewEncoder(w).Encode([]any{})
		return
	}

	defs := reg.List()
	type agentInfo struct {
		Name    string `json:"name"`
		WorkDir string `json:"workdir"`
		Parent  string `json:"parent,omitempty"`
		Status  string `json:"status"`
	}

	agents := make([]agentInfo, len(defs))
	for i, d := range defs {
		status := "stopped"
		if proc := reg.Get(d.Name); proc != nil && proc.Alive() {
			status = "running"
		}
		agents[i] = agentInfo{
			Name:    d.Name,
			WorkDir: d.WorkDir,
			Status:  status,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(agents)
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
	s.mu.Unlock()

	if proc == nil || !proc.Alive() {
		conn.Close(websocket.StatusInternalError, "claude not running")
		return
	}

	// Send JSONL history as raw lines.
	sendHistory(conn, ctx, proc.JSONLPath())

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
			s.BroadcastChat(fmt.Sprintf(`{"type":"rewound","turns":%d}`, ctl.Turns))
			continue
		}

		slog.Info("chat: received", "msg", msg)
		// Echo the user turn into the transcript. Grok ACP does not
		// publish a user event for client prompts, and the browser
		// only renders user bubbles from the wire (not optimistically).
		s.BroadcastChat(chatUserEcho(msg))
		if err := s.SendToOverseer(msg); err != nil {
			slog.Error("chat: send to overseer failed", "err", err)
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

// BroadcastChat sends a JSONL line to all /ws/chat listeners.
func (s *Server) BroadcastChat(line string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ch := range s.chatListeners {
		select {
		case ch <- line:
		default:
		}
	}
}

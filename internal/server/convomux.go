// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
)

// Conversation mux (🎯T537.1): one WebSocket, independent channels.
// Legacy /ws/chat is unchanged until the owner cutover (T505).
//
// Envelope: {"v":1,"ch":"transcript:jevons-po","t":"open|frame|meta|page|send|close|hello|error","body":{...}}
//
// Channel "transcript:{name}" is the only conversation API. Root, PO, and
// workers open the same channel kind. Live frames and page-older use it too.

const muxVersion = 1

type muxEnvelope struct {
	V    int             `json:"v"`
	Ch   string          `json:"ch"`
	T    string          `json:"t"`
	Body json.RawMessage `json:"body,omitempty"`
}

type muxHub struct {
	mu    sync.Mutex
	conns map[*muxSession]struct{}
}

type muxSession struct {
	send        chan []byte
	mu          sync.Mutex
	transcripts map[string]struct{}
}

func newMuxHub() *muxHub {
	return &muxHub{conns: make(map[*muxSession]struct{})}
}

func transcriptChannel(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	return "transcript:" + name
}

func parseTranscriptChannel(ch string) (name string, ok bool) {
	const p = "transcript:"
	if !strings.HasPrefix(ch, p) {
		return "", false
	}
	name = strings.TrimSpace(ch[len(p):])
	if name == "" {
		return "", false
	}
	return name, true
}

func encodeMux(ch, t string, body any) ([]byte, error) {
	env := muxEnvelope{V: muxVersion, Ch: ch, T: t}
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		env.Body = b
	}
	return json.Marshal(env)
}

func (h *muxHub) add(sess *muxSession) {
	if h == nil || sess == nil {
		return
	}
	h.mu.Lock()
	h.conns[sess] = struct{}{}
	h.mu.Unlock()
}

func (h *muxHub) remove(sess *muxSession) {
	if h == nil || sess == nil {
		return
	}
	h.mu.Lock()
	delete(h.conns, sess)
	h.mu.Unlock()
}

func (sess *muxSession) watch(name string) {
	sess.mu.Lock()
	if sess.transcripts == nil {
		sess.transcripts = make(map[string]struct{})
	}
	sess.transcripts[name] = struct{}{}
	sess.mu.Unlock()
}

func (sess *muxSession) unwatch(name string) {
	sess.mu.Lock()
	delete(sess.transcripts, name)
	sess.mu.Unlock()
}

func (sess *muxSession) watching(name string) bool {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	_, ok := sess.transcripts[name]
	return ok
}

func (sess *muxSession) enqueue(payload []byte) {
	if sess == nil {
		return
	}
	select {
	case sess.send <- payload:
	default:
	}
}

func (h *muxHub) fanTranscript(name, frameJSON string) {
	if h == nil || name == "" || strings.TrimSpace(frameJSON) == "" {
		return
	}
	var body any
	if err := json.Unmarshal([]byte(frameJSON), &body); err != nil {
		body = map[string]any{"raw": frameJSON}
	}
	payload, err := encodeMux(transcriptChannel(name), "frame", body)
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for sess := range h.conns {
		if sess.watching(name) {
			sess.enqueue(payload)
		}
	}
}

func (s *Server) muxFanTranscript(name, frameJSON string) {
	if s == nil || s.mux == nil {
		return
	}
	s.mux.fanTranscript(name, frameJSON)
}

func (s *Server) handleMux(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, wsAcceptOptions())
	if err != nil {
		slog.Error("mux: accept failed", "err", err)
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(1 << 20)
	ctx := r.Context()

	sess := &muxSession{
		send:        make(chan []byte, 256),
		transcripts: make(map[string]struct{}),
	}
	if s.mux == nil {
		s.mux = newMuxHub()
	}
	s.mux.add(sess)
	defer s.mux.remove(sess)

	hello, _ := encodeMux("", "hello", map[string]any{
		"conn_id": uuid.NewString(),
	})
	wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	_ = conn.Write(wctx, websocket.MessageText, hello)
	cancel()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case payload, ok := <-sess.send:
				if !ok {
					return
				}
				wctx, cancel := context.WithTimeout(ctx, 30*time.Second)
				err := conn.Write(wctx, websocket.MessageText, payload)
				cancel()
				if err != nil {
					return
				}
			}
		}
	}()

	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		s.handleMuxRaw(ctx, conn, sess, data)
	}
}

// handleMuxRaw consumes one client→server mux frame. Vanilla chat ping
// ({"type":"ping"}) is accepted on this socket too (🎯T537.2.1): React's
// daily driver talks /ws/mux, not /ws/chat, and owner_health heartbeat
// must still tick.
func (s *Server) handleMuxRaw(ctx context.Context, conn muxConn, sess *muxSession, data []byte) {
	msg := strings.TrimSpace(string(data))
	if msg == `{"type":"ping"}` {
		s.NoteOwnerUIHeartbeat()
		if conn == nil {
			return
		}
		wctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		_ = conn.Write(wctx, websocket.MessageText, []byte(`{"type":"pong"}`))
		cancel()
		return
	}
	var env muxEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return
	}
	s.handleMuxEnvelope(ctx, conn, sess, env)
}

type muxConn interface {
	Write(ctx context.Context, typ websocket.MessageType, data []byte) error
}

func (s *Server) handleMuxEnvelope(ctx context.Context, conn muxConn, sess *muxSession, env muxEnvelope) {
	name, isTranscript := parseTranscriptChannel(env.Ch)
	switch env.T {
	case "open":
		if !isTranscript {
			s.muxWrite(ctx, conn, env.Ch, "error", map[string]any{"error": "unknown channel"})
			return
		}
		sess.watch(name)
		if err := s.writeMuxReplay(ctx, conn, name); err != nil {
			slog.Warn("mux: replay failed", "name", name, "err", err)
		}
	case "close":
		if isTranscript {
			sess.unwatch(name)
		}
	case "page":
		if !isTranscript {
			return
		}
		var body struct {
			End   int `json:"end"`
			Limit int `json:"limit"`
		}
		_ = json.Unmarshal(env.Body, &body)
		s.writeMuxPage(ctx, conn, name, body.End, body.Limit)
	case "send":
		if !isTranscript {
			return
		}
		var body struct {
			Text string `json:"text"`
		}
		_ = json.Unmarshal(env.Body, &body)
		text := strings.TrimSpace(body.Text)
		if text == "" {
			return
		}
		if _, err := s.sendToNamedAgentAs(name, text, sendOriginOwner); err != nil {
			s.muxWrite(ctx, conn, env.Ch, "error", map[string]any{"error": err.Error()})
		}
	}
}

func (s *Server) muxWrite(ctx context.Context, conn muxConn, ch, t string, body any) {
	payload, err := encodeMux(ch, t, body)
	if err != nil {
		return
	}
	wctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	_ = conn.Write(wctx, websocket.MessageText, payload)
}

func (s *Server) writeMuxReplay(ctx context.Context, conn muxConn, name string) error {
	ch := transcriptChannel(name)
	writeLine := func(line string) error {
		if echoedOwnerTurnLine(line) {
			return nil
		}
		frame := stampConversationName(line, name)
		var body any
		if err := json.Unmarshal([]byte(frame), &body); err != nil {
			body = map[string]any{"raw": frame}
		}
		s.muxWrite(ctx, conn, ch, "frame", body)
		return nil
	}
	var start, total int
	if s.isOverseerAgent(name) {
		s.mu.RLock()
		clog := s.chatLog
		s.mu.RUnlock()
		if clog != nil {
			var err error
			start, total, err = clog.ReplayTailSealed(historyReplayTurns, writeLine)
			if err != nil {
				return err
			}
		}
	} else {
		j := s.agentJournalsFor()
		if j != nil {
			path := j.path(name)
			if path != "" {
				if l := j.logFor(name); l != nil {
					var err error
					start, total, err = l.ReplayTailSealed(historyReplayTurns, writeLine)
					if err != nil {
						return err
					}
				}
			}
		}
	}
	s.muxWrite(ctx, conn, ch, "meta", map[string]any{
		"older": start, "total": total, "start": start,
	})
	return nil
}

func muxPageBody(start, total int, lines []json.RawMessage) map[string]any {
	if lines == nil {
		lines = []json.RawMessage{}
	}
	older := start
	if len(lines) == 0 {
		older = 0
		start = 0
	}
	return map[string]any{"start": start, "older": older, "total": total, "lines": lines}
}

func (s *Server) writeMuxPage(ctx context.Context, conn muxConn, name string, end, limit int) {
	if limit <= 0 || limit > 2000 {
		limit = 200
	}
	ch := transcriptChannel(name)
	if s.isOverseerAgent(name) {
		s.mu.RLock()
		clog := s.chatLog
		s.mu.RUnlock()
		if clog == nil {
			s.muxWrite(ctx, conn, ch, "page", muxPageBody(0, 0, nil))
			return
		}
		lines, total, err := clog.ReadRange(end-limit, end)
		if err != nil {
			s.muxWrite(ctx, conn, ch, "error", map[string]any{"error": "history read failed"})
			return
		}
		out := make([]json.RawMessage, 0, len(lines))
		for _, ln := range lines {
			if echoedOwnerTurnLine(ln) {
				continue
			}
			out = append(out, json.RawMessage(stampConversationName(ln, name)))
		}
		s.muxWrite(ctx, conn, ch, "page", muxPageBody(end-len(lines), total, out))
		return
	}
	j := s.agentJournalsFor()
	if j == nil {
		s.muxWrite(ctx, conn, ch, "page", muxPageBody(0, 0, nil))
		return
	}
	l := j.logFor(name)
	if l == nil {
		s.muxWrite(ctx, conn, ch, "page", muxPageBody(0, 0, nil))
		return
	}
	lines, total, err := l.ReadRange(end-limit, end)
	if err != nil {
		s.muxWrite(ctx, conn, ch, "error", map[string]any{"error": "history read failed"})
		return
	}
	out := make([]json.RawMessage, 0, len(lines))
	for _, ln := range lines {
		if echoedOwnerTurnLine(ln) {
			continue
		}
		out = append(out, json.RawMessage(stampConversationName(ln, name)))
	}
	s.muxWrite(ctx, conn, ch, "page", muxPageBody(end-len(lines), total, out))
}

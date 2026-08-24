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
	"github.com/marcelocantos/jevons/internal/chatlog"
	"github.com/marcelocantos/jevons/internal/muxwin"
)

// Conversation mux (🎯T537.1 / T537.1.3): one WebSocket, one windowed
// CQRS stream per transcript:{name}. Client submits a coalesced [lo, hi)
// plus halo; the server delivers the missing slice and then streams every
// in-window change until the client submits a new window. Hi=0 is exclusive
// EOF (following). Legacy /ws/chat is unchanged until T505.

const muxVersion = 1

type muxEnvelope struct {
	V    int             `json:"v"`
	Ch   string          `json:"ch"`
	T    string          `json:"t"`
	Body json.RawMessage `json:"body,omitempty"`
}

type muxWatch struct {
	visible    muxwin.Resolved
	sub        muxwin.Resolved
	subscribed bool
	sent       map[string]struct{}
}

type muxHub struct {
	mu     sync.Mutex
	conns  map[*muxSession]struct{}
	events map[string][]muxwin.Event
	stamps map[string][]muxwin.ToolStamp
}

type muxSession struct {
	send        chan []byte
	mu          sync.Mutex
	transcripts map[string]*muxWatch
}

func newMuxHub() *muxHub {
	return &muxHub{
		conns:  make(map[*muxSession]struct{}),
		events: make(map[string][]muxwin.Event),
	}
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

func (h *muxHub) replaceEvents(name string, evs []muxwin.Event) {
	if h == nil {
		return
	}
	h.mu.Lock()
	if h.events == nil {
		h.events = make(map[string][]muxwin.Event)
	}
	h.events[name] = evs
	h.mu.Unlock()
}

func (h *muxHub) eventsFor(name string) []muxwin.Event {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.events[name]
}

func (sess *muxSession) ensure(name string) *muxWatch {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if sess.transcripts == nil {
		sess.transcripts = make(map[string]*muxWatch)
	}
	w := sess.transcripts[name]
	if w == nil {
		w = &muxWatch{
			visible: muxwin.Resolved{Lo: 1, Hi: 0, Following: true},
			sent:    make(map[string]struct{}),
		}
		sess.transcripts[name] = w
	}
	return w
}

func (sess *muxSession) watch(name string) {
	sess.ensure(name)
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

func (sess *muxSession) watchGet(name string) *muxWatch {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	return sess.transcripts[name]
}

func (sess *muxSession) enqueue(payload []byte) bool {
	if sess == nil {
		return false
	}
	select {
	case sess.send <- payload:
		return true
	default:
		return false
	}
}

func muxEventBody(ev muxwin.Event, op, text string) map[string]any {
	m := map[string]any{
		"id":    ev.ID,
		"index": ev.Index,
		"op":    op,
		"type":  ev.Type,
		"event": json.RawMessage(ev.Body),
	}
	if ev.TS != "" {
		m["timestamp"] = ev.TS
	}
	if text != "" {
		m["text"] = text
	}
	return m
}

func muxWindowMeta(r muxwin.Resolved, n int) map[string]any {
	older := 0
	if r.Lo > 1 {
		older = r.Lo
	}
	hi := r.Hi
	if r.Following {
		hi = 0
	}
	return map[string]any{
		"lo": r.Lo, "hi": hi, "n": n, "following": r.Following,
		"start": r.Lo, "older": older, "total": n,
	}
}

func (s *Server) muxTranscriptMeta(r muxwin.Resolved, n int) map[string]any {
	m := muxWindowMeta(r, n)
	if s == nil {
		return m
	}
	m["working"] = s.publishedWorkingLevel()
	m["owner_ux"] = s.ownerUXLevel()
	m["overseer_down"] = s.overseerDownSample()
	return m
}

func (s *Server) overseerDownSample() string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	reason := strings.TrimSpace(s.overseerDownReason)
	proc := s.proc
	name := s.overseerName
	reg := s.registry
	s.mu.RUnlock()
	alive := proc != nil && proc.Alive()
	if !alive && reg != nil && name != "" {
		if p := reg.Get(name); p != nil && p.Alive() {
			alive = true
		}
	}
	if alive && reason == "" {
		return ""
	}
	if reason == "" {
		return "the overseer is not running"
	}
	return reason
}

func (h *muxHub) fanMeta(name string, body map[string]any) {
	if h == nil || name == "" || body == nil {
		return
	}
	payload, err := encodeMux(transcriptChannel(name), "meta", body)
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for sess := range h.conns {
		w := sess.watchGet(name)
		if w == nil || !w.subscribed {
			continue
		}
		sess.enqueue(payload)
	}
}

func (s *Server) muxFanOverseerLevel() {
	if s == nil || s.mux == nil {
		return
	}
	s.mu.RLock()
	name := s.overseerName
	s.mu.RUnlock()
	if name == "" {
		name = "jevons"
	}
	s.mux.fanMeta(name, map[string]any{
		"working":       s.publishedWorkingLevel(),
		"owner_ux":      s.ownerUXLevel(),
		"overseer_down": s.overseerDownSample(),
	})
}

func (h *muxHub) fanTranscript(name, frameJSON string) (applied []muxwin.ToolStamp) {
	if h == nil || name == "" || strings.TrimSpace(frameJSON) == "" {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.events == nil {
		h.events = make(map[string][]muxwin.Event)
	}
	next, folds := muxwin.ApplyLiveAll(h.events[name], frameJSON)
	var extra []muxwin.LiveFold
	if len(h.stamps[name]) > 0 {
		var rest []muxwin.ToolStamp
		next, extra, applied, rest = muxwin.ApplyStamps(next, h.stamps[name])
		h.stamps[name] = rest
		folds = append(folds, extra...)
	}
	if len(folds) == 0 {
		return applied
	}
	h.events[name] = next
	h.fanFoldsLocked(name, folds)
	return applied
}

func (h *muxHub) fanFoldsLocked(name string, folds []muxwin.LiveFold) {
	for _, fold := range folds {
		changed, op := fold.Event, fold.Op
		payload, err := encodeMux(transcriptChannel(name), "frame", muxEventBody(changed, op, fold.Text))
		if err != nil {
			return
		}
		for sess := range h.conns {
			w := sess.watchGet(name)
			if w == nil || !w.subscribed || !muxwin.Contains(w.sub, changed.Index) {
				continue
			}
			if op == "put" {
				if _, ok := w.sent[changed.ID]; ok {
					continue
				}
			}
			if !sess.enqueue(payload) {
				continue
			}
			if w.sent == nil {
				w.sent = make(map[string]struct{})
			}
			w.sent[changed.ID] = struct{}{}
		}
	}
}

func (h *muxHub) applyStampNow(name string, st muxwin.ToolStamp) bool {
	if h == nil || name == "" {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	next, folds, applied, _ := muxwin.ApplyStamps(h.events[name], []muxwin.ToolStamp{st})
	if len(applied) == 0 {
		return false
	}
	h.events[name] = next
	h.fanFoldsLocked(name, folds)
	return true
}

func (h *muxHub) queueToolStamp(name string, st muxwin.ToolStamp) {
	if h == nil || name == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.stamps == nil {
		h.stamps = make(map[string][]muxwin.ToolStamp)
	}
	h.stamps[name] = append(h.stamps[name], st)
}

func (s *Server) muxFanTranscript(name, frameJSON string) {
	if s == nil || s.mux == nil {
		return
	}
	for _, st := range s.mux.fanTranscript(name, frameJSON) {
		if line := chatToolStampLine(st.Name, st.Input); line != "" {
			s.persistChatLine(line)
		}
	}
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
		transcripts: make(map[string]*muxWatch),
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
		sess.ensure(name)
		lo, hi := muxOpenWindow(env.Body)
		if err := s.writeMuxWindow(ctx, conn, sess, name, lo, hi, true); err != nil {
			slog.Warn("mux: window failed", "name", name, "err", err)
		}
	case "window":
		if !isTranscript {
			return
		}
		var body struct {
			Lo int `json:"lo"`
			Hi int `json:"hi"`
		}
		_ = json.Unmarshal(env.Body, &body)
		sess.ensure(name)
		if err := s.writeMuxWindow(ctx, conn, sess, name, body.Lo, body.Hi, false); err != nil {
			slog.Warn("mux: window failed", "name", name, "err", err)
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
			Before string `json:"before"`
			Limit  int    `json:"limit"`
			End    int    `json:"end"`
		}
		_ = json.Unmarshal(env.Body, &body)
		if body.Before != "" {
			s.writeMuxPageBefore(ctx, conn, sess, name, body.Before, body.Limit)
			return
		}
		// Legacy end/limit: exclusive coalesced index, not raw journal.
		lo := body.End - body.Limit
		if lo < 1 {
			lo = 1
		}
		s.writeMuxWindow(ctx, conn, sess, name, lo, body.End, false)
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

func muxOpenWindow(raw json.RawMessage) (lo, hi int) {
	lo, hi = -muxwin.DefaultFollow, 0
	if len(raw) == 0 {
		return lo, hi
	}
	var body struct {
		Lo *int `json:"lo"`
		Hi *int `json:"hi"`
	}
	if json.Unmarshal(raw, &body) != nil {
		return lo, hi
	}
	if body.Lo != nil {
		lo = *body.Lo
	}
	if body.Hi != nil {
		hi = *body.Hi
	}
	return lo, hi
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

// First-paint journal read. A full Replay of the daily overseer log
// (~87MB / 350k lines) is what made the first screen wait on fold+index.
// Grow only when the tail does not yet hold DefaultFollow events.
const muxFirstPaintBytesMax = 8 << 20

// muxCoalesced returns the coalesced transcript. A filled hub cache is
// reused so page/window do not rebuild the journal on every PageUp.
// force rebuilds from disk (first open / empty cache) via a byte tail
// from EOF — not a full Replay.
func (s *Server) muxCoalesced(name string, force bool) []muxwin.Event {
	if s.mux != nil && !force {
		if evs := s.mux.eventsFor(name); len(evs) > 0 {
			return evs
		}
	}
	bytes := chatlog.DefaultTailBytes
	var events []muxwin.Event
	for {
		lines, truncated := s.muxJournalTail(name, bytes)
		events = muxwin.EventsFromLines(lines)
		if !truncated || len(events) >= muxwin.DefaultFollow || bytes >= muxFirstPaintBytesMax {
			break
		}
		bytes *= 2
		if bytes > muxFirstPaintBytesMax {
			bytes = muxFirstPaintBytesMax
		}
	}
	if s.mux != nil {
		s.mux.replaceEvents(name, events)
	}
	return events
}

func (s *Server) muxJournalTail(name string, maxBytes int) (lines []string, truncated bool) {
	collect := func(l *chatlog.Log) (lines []string, truncated bool) {
		if l == nil {
			return nil, false
		}
		raw, truncated, err := l.TailBytes(maxBytes)
		if err != nil {
			return nil, truncated
		}
		for _, line := range raw {
			if echoedOwnerTurnLine(line) {
				continue
			}
			lines = append(lines, stampConversationName(line, name))
		}
		return lines, truncated
	}
	if s.isOverseerAgent(name) {
		s.mu.RLock()
		clog := s.chatLog
		s.mu.RUnlock()
		return collect(clog)
	}
	j := s.agentJournalsFor()
	if j == nil {
		return nil, false
	}
	return collect(j.logFor(name))
}

func (s *Server) writeMuxWindow(ctx context.Context, conn muxConn, sess *muxSession, name string, lo, hi int, refresh bool) error {
	ch := transcriptChannel(name)
	events := s.muxCoalesced(name, refresh)
	resolved, err := muxwin.Resolve(lo, hi, len(events))
	if err != nil {
		s.muxWrite(ctx, conn, ch, "error", map[string]any{"error": err.Error()})
		return err
	}
	sub := muxwin.Subscribe(resolved, muxwin.KindsOf(events), muxwin.HaloProse)
	var watch *muxWatch
	if sess != nil {
		watch = sess.ensure(name)
		watch.visible = resolved
		watch.sub = sub
		watch.subscribed = true
	}
	var have map[int]struct{}
	if watch != nil {
		have = muxwin.HaveFromIDs(events, watch.sent)
	}
	for _, ev := range muxwin.Slice(events, muxwin.Need(sub, len(events), have)) {
		s.muxWrite(ctx, conn, ch, "frame", muxEventBody(ev, "put", ""))
		if watch != nil {
			if watch.sent == nil {
				watch.sent = make(map[string]struct{})
			}
			watch.sent[ev.ID] = struct{}{}
		}
	}
		s.muxWrite(ctx, conn, ch, "meta", s.muxTranscriptMeta(sub, len(events)))
	return nil
}

func (s *Server) writeMuxReplay(ctx context.Context, conn muxConn, name string) error {
	return s.writeMuxWindow(ctx, conn, nil, name, -muxwin.DefaultFollow, 0, true)
}

func (s *Server) writeMuxPageBefore(ctx context.Context, conn muxConn, sess *muxSession, name, before string, limit int) {
	ch := transcriptChannel(name)
	events := s.muxCoalesced(name, false)
	var watch *muxWatch
	var have map[int]struct{}
	if sess != nil {
		watch = sess.ensure(name)
		have = muxwin.HaveFromIDs(events, watch.sent)
	}
	page, err := muxwin.BeforeUnsent(events, before, limit, have)
	if err != nil {
		s.muxWrite(ctx, conn, ch, "error", map[string]any{"error": err.Error()})
		return
	}
	// Page is a fetch of older unsent events, not a new CQRS window.
	// The last open/window subscription keeps streaming as-is.
	sub := page
	out := muxwin.Slice(events, muxwin.Need(sub, len(events), have))
	lines := make([]json.RawMessage, 0, len(out))
	for _, ev := range out {
		s.muxWrite(ctx, conn, ch, "frame", muxEventBody(ev, "put", ""))
		if watch != nil {
			if watch.sent == nil {
				watch.sent = make(map[string]struct{})
			}
			watch.sent[ev.ID] = struct{}{}
		}
		lines = append(lines, json.RawMessage(mustJSON(muxEventBody(ev, "put", ""))))
	}
	older := 0
	if sub.Lo > 1 {
		older = sub.Lo
	}
	following := watch != nil && watch.sub.Following
	s.muxWrite(ctx, conn, ch, "page", map[string]any{
		"start": sub.Lo, "older": older, "total": len(events),
		"lo": sub.Lo, "hi": sub.Hi, "n": len(events), "following": following,
		"lines": lines,
	})
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("{}")
	}
	return b
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
	lo := end - limit
	if lo < 1 {
		lo = 1
	}
	_ = s.writeMuxWindow(ctx, conn, nil, name, lo, end, false)
}

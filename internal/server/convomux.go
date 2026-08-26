// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"encoding/json"
	"fmt"
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
	mu        sync.Mutex
	conns     map[*muxSession]struct{}
	events    map[string][]muxwin.Event
	tailBytes map[string]int
	truncated map[string]bool
	stamps    map[string][]muxwin.ToolStamp
	// journalN is the absolute coalesced length (statedb MAX(idx)).
	// Zero means "use len(events)" — the JSONL-tail fallback.
	journalN map[string]int
}

type muxSession struct {
	send        chan []byte
	mu          sync.Mutex
	transcripts map[string]*muxWatch
}

func newMuxHub() *muxHub {
	return &muxHub{
		conns:     make(map[*muxSession]struct{}),
		events:    make(map[string][]muxwin.Event),
		tailBytes: make(map[string]int),
		truncated: make(map[string]bool),
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
	h.replaceCache(name, evs, 0, false)
}

func (h *muxHub) replaceCache(name string, evs []muxwin.Event, bytes int, truncated bool) {
	h.replaceCacheN(name, evs, bytes, truncated, 0)
}

func (h *muxHub) replaceCacheN(name string, evs []muxwin.Event, bytes int, truncated bool, n int) {
	if h == nil {
		return
	}
	h.mu.Lock()
	if h.events == nil {
		h.events = make(map[string][]muxwin.Event)
	}
	if h.tailBytes == nil {
		h.tailBytes = make(map[string]int)
	}
	if h.truncated == nil {
		h.truncated = make(map[string]bool)
	}
	if h.journalN == nil {
		h.journalN = make(map[string]int)
	}
	h.events[name] = evs
	h.tailBytes[name] = bytes
	h.truncated[name] = truncated
	if n > 0 {
		h.journalN[name] = n
	}
	h.mu.Unlock()
}

func (h *muxHub) tailState(name string) (bytes int, truncated bool) {
	if h == nil {
		return 0, false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.tailBytes[name], h.truncated[name]
}

func (h *muxHub) eventsFor(name string) []muxwin.Event {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.events[name]
}

func (h *muxHub) absoluteN(name string) int {
	if h == nil {
		return 0
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.journalN[name]
}

func (h *muxHub) noteAbsoluteN(name string, n int) {
	if h == nil || n < 1 {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.journalN == nil {
		h.journalN = make(map[string]int)
	}
	if n > h.journalN[name] {
		h.journalN[name] = n
	}
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

func muxWindowMeta(r muxwin.Resolved, n int, truncated bool) map[string]any {
	older := 0
	if r.Lo > 1 {
		older = r.Lo
	} else if truncated {
		// Cache starts at 1, but older journal bytes still exist on disk
		// (🎯T494.1.4). older=0 here is what made PageUp stop at ~10 screens.
		older = 2
	}
	hi := r.Hi
	if r.Following {
		hi = 0
	}
	m := map[string]any{
		"lo": r.Lo, "hi": hi, "n": n, "following": r.Following,
		"start": r.Lo, "older": older, "total": n,
	}
	if truncated {
		m["truncated"] = true
	}
	return m
}

func (s *Server) muxTranscriptMeta(r muxwin.Resolved, n int, truncated bool) map[string]any {
	m := muxWindowMeta(r, n, truncated)
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
	folds, applied := h.applyLine(name, frameJSON)
	if len(folds) == 0 {
		return applied
	}
	h.mu.Lock()
	h.fanFoldsLocked(name, folds)
	h.mu.Unlock()
	return applied
}

func (h *muxHub) applyLine(name, frameJSON string) (folds []muxwin.LiveFold, applied []muxwin.ToolStamp) {
	if h == nil || name == "" || strings.TrimSpace(frameJSON) == "" {
		return nil, nil
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
		return nil, applied
	}
	h.events[name] = next
	if h.journalN == nil {
		h.journalN = make(map[string]int)
	}
	if n := h.journalN[name]; n > 0 {
		for _, f := range folds {
			if f.Event.Index > n {
				n = f.Event.Index
			}
		}
		h.journalN[name] = n
	}
	return folds, applied
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

func (h *muxHub) applyStampNow(name string, st muxwin.ToolStamp) (folds []muxwin.LiveFold, ok bool) {
	if h == nil || name == "" {
		return nil, false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	next, folds, applied, _ := muxwin.ApplyStamps(h.events[name], []muxwin.ToolStamp{st})
	if len(applied) == 0 {
		return nil, false
	}
	h.events[name] = next
	h.fanFoldsLocked(name, folds)
	return folds, true
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
	s.muxEnsureLive(name)
	folds, stamps := s.mux.applyLine(name, frameJSON)
	s.statedbUpsertFolds(name, folds)
	if len(folds) > 0 {
		s.mux.mu.Lock()
		s.mux.fanFoldsLocked(name, folds)
		s.mux.mu.Unlock()
	}
	for _, st := range stamps {
		if line := chatToolStampLine(st.Name, st.Input); line != "" && s.statedbN(name) == 0 {
			s.persistChatJSONL(line)
		}
	}
}

// muxEnsureLive seeds the hub cache from statedb so ApplyLive continues
// absolute indexes instead of restarting at 1.
func (s *Server) muxEnsureLive(name string) {
	if s == nil || s.mux == nil {
		return
	}
	if evs := s.mux.eventsFor(name); len(evs) > 0 {
		return
	}
	if s.stateStore() == nil {
		return
	}
	_ = s.muxCoalesced(name, false)
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
		// A send is a request to see the echo: unfreeze this session's
		// watch so a statedb-suffix following window still Contains the
		// new absolute index. Do not block the mux read loop on ACP
		// prompt-in-flight (that wedged heartbeats and the next send).
		if sess != nil {
			if w := sess.watchGet(name); w != nil {
				sess.mu.Lock()
				w.visible.Following = true
				w.visible.Hi = 0
				w.sub.Following = true
				w.sub.Hi = 0
				sess.mu.Unlock()
			}
		}
		ch := env.Ch
		go func() {
			if _, err := s.sendToNamedAgentAs(name, text, sendOriginOwner); err != nil {
				s.muxWrite(context.Background(), conn, ch, "error", map[string]any{"error": err.Error()})
			}
		}()
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

// muxPageGrowBytesMax is how far one session will walk backward from
// EOF on PageUp (🎯T494.1.4). Doubles per empty page. Residual: the
// rest of an 87MB journal past this cap is still on disk.
const muxPageGrowBytesMax = 32 << 20

func (s *Server) muxTailStartBytes() int {
	if s != nil && s.muxTailBytes > 0 {
		return s.muxTailBytes
	}
	return chatlog.DefaultTailBytes
}

func (s *Server) muxFirstPaintCap() int {
	if s != nil && s.muxTailBytes > 0 {
		return s.muxTailBytes
	}
	return muxFirstPaintBytesMax
}

func (s *Server) muxTruncated(name string) bool {
	if s == nil || s.mux == nil {
		return false
	}
	_, truncated := s.mux.tailState(name)
	return truncated
}

// muxCoalesced returns the coalesced transcript. A filled hub cache is
// reused so page/window do not rebuild the journal on every PageUp.
// When statedb has rows, the cache is the live suffix and indexes are
// journal-absolute (🎯T548.2). Otherwise force rebuilds from a JSONL
// byte tail — not a full Replay.
func (s *Server) muxCoalesced(name string, force bool) []muxwin.Event {
	if s.mux != nil && !force {
		if evs := s.mux.eventsFor(name); len(evs) > 0 {
			return evs
		}
	}
	if evs, ok := s.muxCoalescedFromDB(name); ok {
		return evs
	}
	bytes := s.muxTailStartBytes()
	capBytes := s.muxFirstPaintCap()
	var events []muxwin.Event
	var truncated bool
	for {
		var lines []string
		lines, truncated = s.muxJournalTail(name, bytes)
		events = muxwin.EventsFromLines(lines)
		if !truncated || len(events) >= muxwin.DefaultFollow || bytes >= capBytes {
			break
		}
		bytes *= 2
		if bytes > capBytes {
			bytes = capBytes
		}
	}
	if s.mux != nil {
		s.mux.replaceCache(name, events, bytes, truncated)
	}
	return events
}

func (s *Server) muxCoalescedFromDB(name string) ([]muxwin.Event, bool) {
	db := s.stateStore()
	if db == nil {
		return nil, false
	}
	n := s.statedbN(name)
	if n == 0 {
		s.importJSONL(name, s.muxJSONLPath(name))
		n = s.statedbN(name)
	}
	if n == 0 {
		return nil, false
	}
	lo := s.statedbTailStart(name, muxwin.DefaultFollow)
	if lo < 1 {
		lo = n - muxwin.DefaultFollow + 1
		if lo < 1 {
			lo = 1
		}
	}
	events := s.statedbRange(name, lo, n+1)
	if s.mux != nil {
		s.mux.replaceCacheN(name, events, 0, lo > 1, n)
	}
	return events, true
}

func (s *Server) muxJSONLPath(name string) string {
	if s.isOverseerAgent(name) {
		s.mu.RLock()
		clog := s.chatLog
		s.mu.RUnlock()
		if clog != nil {
			return clog.Path()
		}
		return ""
	}
	j := s.agentJournalsFor()
	if j == nil {
		return ""
	}
	return j.path(name)
}

func (s *Server) muxAbsoluteN(name string, events []muxwin.Event) int {
	if s.mux != nil {
		if n := s.mux.absoluteN(name); n > 0 {
			return n
		}
	}
	if n := s.statedbN(name); n > 0 {
		return n
	}
	return len(events)
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
	// Vanilla first-paint is historyReplayTurns user turns (🎯T57).
	// Negative following lo used to mean last-N *events*; with statedb
	// that is a handful of tool/step rows and an empty-looking pane.
	if hi == 0 && lo < 0 {
		if start := s.statedbTailStart(name, -lo); start > 0 {
			lo = start
		}
	}
	events := s.muxCoalesced(name, refresh)
	n := s.muxAbsoluteN(name, events)
	resolved, err := muxwin.Resolve(lo, hi, n)
	if err != nil {
		s.muxWrite(ctx, conn, ch, "error", map[string]any{"error": err.Error()})
		return err
	}
	// Absolute n: do not walk a cache-relative halo (that is what made
	// first-paint claim older=0 at cache index 1). Page-up fetches more.
	sub := resolved
	if s.mux == nil || s.mux.absoluteN(name) == 0 {
		sub = muxwin.Subscribe(resolved, muxwin.KindsOf(events), muxwin.HaloProse)
	}
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
	need := muxwin.Need(sub, n, have)
	out := s.muxEventsAt(name, events, need)
	for _, ev := range out {
		s.muxWrite(ctx, conn, ch, "frame", muxEventBody(ev, "put", ""))
		if watch != nil {
			if watch.sent == nil {
				watch.sent = make(map[string]struct{})
			}
			watch.sent[ev.ID] = struct{}{}
		}
	}
	s.muxWrite(ctx, conn, ch, "meta", s.muxTranscriptMeta(sub, n, s.muxTruncated(name)))
	return nil
}

func (s *Server) muxEventsAt(name string, cache []muxwin.Event, idxs []int) []muxwin.Event {
	if len(idxs) == 0 {
		return nil
	}
	if s.mux != nil && s.mux.absoluteN(name) > 0 {
		lo, hi := idxs[0], idxs[len(idxs)-1]+1
		return s.statedbRange(name, lo, hi)
	}
	return muxwin.Slice(cache, idxs)
}

func (s *Server) writeMuxReplay(ctx context.Context, conn muxConn, name string) error {
	return s.writeMuxWindow(ctx, conn, nil, name, -muxwin.DefaultFollow, 0, true)
}

func (s *Server) writeMuxPageBefore(ctx context.Context, conn muxConn, sess *muxSession, name, before string, limit int) {
	ch := transcriptChannel(name)
	if s.mux != nil && s.mux.absoluteN(name) > 0 {
		s.writeMuxPageBeforeDB(ctx, conn, sess, name, before, limit)
		return
	}
	events := s.muxCoalesced(name, false)
	if s.mux != nil && s.mux.absoluteN(name) > 0 {
		s.writeMuxPageBeforeDB(ctx, conn, sess, name, before, limit)
		return
	}
	var watch *muxWatch
	var have map[int]struct{}
	if sess != nil {
		watch = sess.ensure(name)
		have = muxwin.HaveFromIDs(events, watch.sent)
	}
	page, err := muxwin.BeforeUnsent(events, before, limit, have)
	if err != nil && s.muxTruncated(name) && s.muxGrowOlder(name) {
		events = s.muxCoalesced(name, false)
		if watch != nil {
			have = muxwin.HaveFromIDs(events, watch.sent)
		}
		page, err = muxwin.BeforeUnsent(events, before, limit, have)
	}
	if err != nil {
		s.muxWrite(ctx, conn, ch, "error", map[string]any{"error": err.Error()})
		return
	}
	// Page is a fetch of older unsent events, not a new CQRS window.
	// The last open/window subscription keeps streaming as-is.
	out := muxwin.Slice(events, muxwin.Need(page, len(events), have))
	for len(out) == 0 && s.muxTruncated(name) {
		if !s.muxGrowOlder(name) {
			break
		}
		events = s.muxCoalesced(name, false)
		if watch != nil {
			have = muxwin.HaveFromIDs(events, watch.sent)
		}
		var growErr error
		page, growErr = muxwin.BeforeUnsent(events, before, limit, have)
		if growErr != nil {
			break
		}
		out = muxwin.Slice(events, muxwin.Need(page, len(events), have))
	}
	s.writeMuxPageEnvelope(ctx, conn, sess, name, page, out, len(events), s.muxTruncated(name))
}

func (s *Server) writeMuxPageBeforeDB(ctx context.Context, conn muxConn, sess *muxSession, name, before string, limit int) {
	if limit <= 0 {
		limit = 50
	}
	n := s.muxAbsoluteN(name, nil)
	idx := muxBeforeIndex(before)
	if idx < 1 {
		events := s.mux.eventsFor(name)
		idx = muxwinIndexOfID(events, before)
	}
	if idx < 1 {
		s.muxWrite(ctx, conn, transcriptChannel(name), "error", map[string]any{"error": "muxwin: unknown before id " + before})
		return
	}
	out := s.statedbBefore(name, idx, limit)
	lo := 1
	if len(out) > 0 {
		lo = out[0].Index
	} else if idx > 1 {
		lo = idx
	}
	page := muxwin.Resolved{Lo: lo, Hi: idx, Following: false}
	s.writeMuxPageEnvelope(ctx, conn, sess, name, page, out, n, lo > 1)
}

func muxBeforeIndex(id string) int {
	id = strings.TrimSpace(id)
	if !strings.HasPrefix(id, "e:") {
		return 0
	}
	n := 0
	for _, r := range id[2:] {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func muxwinIndexOfID(events []muxwin.Event, id string) int {
	for _, e := range events {
		if e.ID == id {
			return e.Index
		}
	}
	return 0
}

func (s *Server) writeMuxPageEnvelope(ctx context.Context, conn muxConn, sess *muxSession, name string, page muxwin.Resolved, out []muxwin.Event, n int, truncated bool) {
	ch := transcriptChannel(name)
	var watch *muxWatch
	if sess != nil {
		watch = sess.ensure(name)
	}
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
	if page.Lo > 1 {
		older = page.Lo
	} else if truncated {
		older = 2
	}
	if len(out) == 0 && !truncated {
		older = 0
	}
	following := watch != nil && watch.sub.Following
	body := map[string]any{
		"start": page.Lo, "older": older, "total": n,
		"lo": page.Lo, "hi": page.Hi, "n": n, "following": following,
		"lines": lines,
	}
	if truncated {
		body["truncated"] = true
	}
	s.muxWrite(ctx, conn, ch, "page", body)
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

func (s *Server) muxPageGrowCap() int {
	if s != nil && s.muxTailBytes > 0 {
		capBytes := s.muxTailBytes * 64
		if capBytes < 1<<20 {
			capBytes = 1 << 20
		}
		return capBytes
	}
	return muxPageGrowBytesMax
}

// muxGrowOlder doubles the byte tail and stitches newly visible older
// events onto the cache, keeping already-sent event IDs stable so the
// client's before cursor still resolves (🎯T494.1.4).
func (s *Server) muxGrowOlder(name string) bool {
	if s == nil || s.mux == nil {
		return false
	}
	curBytes, truncated := s.mux.tailState(name)
	if !truncated {
		return false
	}
	start := s.muxTailStartBytes()
	if curBytes < start {
		curBytes = start
	}
	next := curBytes * 2
	if next < start*2 {
		next = start * 2
	}
	capBytes := s.muxPageGrowCap()
	if next > capBytes {
		next = capBytes
	}
	if next <= curBytes {
		return false
	}
	prev := s.mux.eventsFor(name)
	prevLen := len(prev)
	lines, stillTrunc := s.muxJournalTail(name, next)
	full := muxwin.EventsFromLines(lines)
	grown := stitchMuxOlder(prev, full)
	s.mux.replaceCache(name, grown, next, stillTrunc)
	return len(grown) > prevLen || next > curBytes
}

func sameMuxEvent(a, b muxwin.Event) bool {
	if a.Type != b.Type {
		return false
	}
	if a.TS != "" && b.TS != "" && a.TS != b.TS {
		return false
	}
	return string(a.Body) == string(b.Body)
}

func nextGrownIDBase(prev []muxwin.Event) int {
	max := 0
	for _, e := range prev {
		var n int
		if _, err := fmt.Sscanf(e.ID, "e:o:%d", &n); err == nil && n > max {
			max = n
		}
	}
	return max + 1
}

func stitchMuxOlder(prev, full []muxwin.Event) []muxwin.Event {
	if len(prev) == 0 {
		return full
	}
	if len(full) == 0 {
		return prev
	}
	cut := -1
	anchor := prev[0]
	for i := range full {
		if sameMuxEvent(full[i], anchor) {
			cut = i
			break
		}
	}
	if cut < 0 {
		// Fold may have rewritten the old start (torn first line).
		// Match a suffix from EOF.
		n, m := len(prev), len(full)
		if m < n {
			return full
		}
		cut = m - n
		for i := 0; i < n; i++ {
			if !sameMuxEvent(prev[i], full[cut+i]) {
				return full
			}
		}
	}
	if cut == 0 {
		return prev
	}
	older := make([]muxwin.Event, cut)
	copy(older, full[:cut])
	base := nextGrownIDBase(prev)
	out := make([]muxwin.Event, 0, cut+len(prev))
	for i := range older {
		older[i].Index = i + 1
		older[i].ID = fmt.Sprintf("e:o:%d", base+i)
		out = append(out, older[i])
	}
	for i := range prev {
		ev := prev[i]
		ev.Index = cut + i + 1
		out = append(out, ev)
	}
	return out
}

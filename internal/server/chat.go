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
	"github.com/marcelocantos/jevons/internal/briefaddr"
	"github.com/marcelocantos/jevons/internal/chatlog"
	"github.com/marcelocantos/jevons/internal/cli"
	"github.com/marcelocantos/jevons/internal/fleet"
	"github.com/marcelocantos/jevons/internal/silentresponse"
	"github.com/marcelocantos/jevons/internal/targetfile"
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
//
// 🎯T259: concurrent handlers are serialised via historyGate so a large
// chatlog progressive hydrate cannot burn more than one core. Successful
// pages log at Debug (sampled Info every historyInfoEvery) to avoid
// hydrate_page INFO spam on multi-100k-line journals.
func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	s.historyGate.acquire()
	defer s.historyGate.release()
	if s.historySlowHook != nil {
		s.historySlowHook()
	}

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
	// 🎯T240: display path coalesces token streams and strips silent sealed
	// assistant bodies (same view as ReplayTailSealed). Raw journal unchanged.
	// start/total still describe the raw window for paging.
	display := chatlog.CoalesceStreamLines(lines)
	raw := make([]json.RawMessage, 0, len(display))
	for _, ln := range display {
		// 🎯T382: pre-fix journals hold provider echoes of owner turns;
		// paging one back would repaint the message a second time.
		if echoedOwnerTurnLine(ln) {
			continue
		}
		raw = append(raw, json.RawMessage(ln))
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"start": end - len(lines), "total": total, "lines": raw,
	})
	// 🎯T259: Debug per page; sample Info so large progressive hydrates do not
	// flood the decision/event log at production default.
	n := s.historyPageSeq.Add(1)
	if n == 1 || n%historyInfoEvery == 0 {
		slog.Info("history hydrate page",
			"end", end, "limit", limit, "lines", len(lines), "total", total, "seq", n)
	} else {
		slog.Debug("history hydrate page",
			"end", end, "limit", limit, "lines", len(lines), "total", total, "seq", n)
	}
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
// one if needed (🎯T223 / 🎯T242). Caller must hold s.mu.
//
// IDs are globally unique (UUID), not "s1"/"s2" restart counters. Short
// sequential ids reused after every daemon bounce caused CoalesceStreamLines
// to glue many real turns into one stream on history replay — and if any
// reincarnation started with [silent], the whole blob was stripped (owner
// saw replies flash live then vanish on seal/reload as consecutive users).
func (s *Server) ensureOverseerStreamIDLocked() string {
	if s.overseerStreamID == "" {
		s.overseerStreamSeq++ // kept for metrics / debug ordering only
		s.overseerStreamID = uuid.NewString()
	}
	return s.overseerStreamID
}

// clearOverseerStreamID drops the open stream label and 🎯T240 silent state
// after a terminal stop.
func (s *Server) clearOverseerStreamID() {
	s.mu.Lock()
	s.overseerStreamID = ""
	s.overseerStreamAcc = ""
	s.overseerStreamSilent = false
	s.overseerStreamHold = nil
	s.mu.Unlock()
}

// emptyEndTurnWire builds a body-less terminal assistant frame so the UI
// clears working without painting prose (🎯T238 / 🎯T240).
func emptyEndTurnWire(stopReason, streamID string) string {
	if stopReason == "" {
		stopReason = "end_turn"
	}
	b, err := json.Marshal(map[string]any{
		"type":      "assistant",
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		"message": map[string]any{
			"role":        "assistant",
			"content":     []any{},
			"stop_reason": stopReason,
		},
	})
	if err != nil {
		return ""
	}
	return stampStreamID(string(b), streamID)
}

// DeliverOverseerEvent is the live event path for the overseer: normalise
// to the chat wire shape, broadcast to /ws/chat listeners, then update
// turn/idle status. Extracted so tests can drive the same path without
// a live claudia.Agent.
//
// 🎯T240: assistant text is classified as a whole stream (accumulated
// deltas per stream_id). Once silentresponse marks the stream Silent,
// body fragments are neither journaled nor broadcast; only an empty
// end_turn is emitted on terminal so the UI clears working.
func (s *Server) DeliverOverseerEvent(ev claudia.Event) {
	// Any ACP traffic resets stuck-busy idle (🎯T204).
	s.NoteOverseerProgress()

	// 🎯T406: transport-level provider refusals on the overseer wire enter
	// (or leave alone) the fleet hard-block. Authored prose never fires —
	// ClassifyFrom(SourceAuthored) is ClassNone (🎯T455).
	if ev.Type == "assistant" && ev.Text != "" {
		if class := agenterr.ClassifyFrom(assistantTextSource(ev), ev.Text); class.IsFailure() {
			s.observeProviderFailure(class, ev.Text)
		}
	}

	// 🎯T513: a user turn whose 🎯T425 identity header names a fleet agent is
	// that agent's brief, misdelivered — the overseer chat is the wrong seat
	// for it. 🎯T452 refuses these on the send path; this is the mirror-image
	// check on arrival, the last point where the owner's chatlog can still be
	// kept clean. Dropped loudly, with both names: never broadcast, never
	// journaled. Bound to user-turn injects only — an assistant turn is the
	// overseer's own prose, and IdentityHeaderName already reads the opening
	// header block alone, so a report QUOTING a header still lands.
	if ev.Type == "user" {
		if text, _ := userTurnText(ev); text != "" {
			if err := briefaddr.Check(s.overseerAgentName(), text); err != nil {
				slog.Warn("chat: dropping wrong-seat brief from overseer wire (🎯T513)", "err", err)
				s.HandleAgentEvent(ev)
				return
			}
		}
	}
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

	// 🎯T240 whole-stream silent: accumulate assistant text, suppress body.
	if ev.Type == "assistant" && (ev.Text != "" || ev.IsTerminalStop()) {
		s.mu.Lock()
		if ev.Text != "" {
			s.overseerStreamAcc += ev.Text
			switch silentresponse.Classify(s.overseerStreamAcc) {
			case silentresponse.Silent:
				s.overseerStreamSilent = true
				s.overseerStreamHold = nil // drop any incomplete-prefix hold
			case silentresponse.Visible:
				// Flush any held prefix fragments now that we know visible.
				for _, held := range s.overseerStreamHold {
					s.mu.Unlock()
					s.BroadcastChat(held)
					s.mu.Lock()
				}
				s.overseerStreamHold = nil
			}
		}
		silent := s.overseerStreamSilent
		class := silentresponse.Classify(s.overseerStreamAcc)
		s.mu.Unlock()

		if silent {
			// 🎯T481: record the body; do not broadcast it.
			if ev.Text != "" {
				s.persistChatLine(losslessLine(ev))
			}
			if ev.IsTerminalStop() {
				if line := emptyEndTurnWire(ev.StopReason, streamID); line != "" {
					s.BroadcastChat(line)
				}
				s.clearOverseerStreamID()
			}
			s.HandleAgentEvent(ev)
			return
		}

		if class == silentresponse.Pending && ev.Text != "" && !ev.IsTerminalStop() {
			// Hold wire until prefix completes or stream proves visible.
			if line, ok := chatWireLine(ev); ok {
				if streamID != "" {
					line = stampStreamID(line, streamID)
				}
				s.mu.Lock()
				s.overseerStreamHold = append(s.overseerStreamHold, line)
				s.mu.Unlock()
			}
			s.HandleAgentEvent(ev)
			return
		}

		// Terminal while still Pending with empty/non-silent acc: flush hold
		// then normal wire (empty end_turn). Pending with incomplete "[" only
		// is rare; treat as visible on seal.
		if ev.IsTerminalStop() && class == silentresponse.Pending {
			s.mu.Lock()
			held := s.overseerStreamHold
			s.overseerStreamHold = nil
			acc := s.overseerStreamAcc
			s.mu.Unlock()
			for _, h := range held {
				s.BroadcastChat(h)
			}
			// 🎯T378: held fragments proved visible on seal — the owner can
			// read them, so this turn answered.
			if len(held) > 0 {
				s.noteOwnerVisibleText(acc)
			}
		}
	}

	if line, ok := chatWireLine(ev); ok {
		if streamID != "" {
			line = stampStreamID(line, streamID)
		}
		// If this full-text event is silent (T238 single-fragment path) and
		// we did not already return above, chatWireLine drops body; terminal
		// empty end_turn still ok.
		s.BroadcastChat(line)
		// 🎯T378: reaching here with assistant prose means the stream was not
		// silent — every silent path returned above — so this is text the
		// owner actually sees, which is the only thing that answers a
		// question. A seal alone never gets to make that claim.
		if ev.Type == "assistant" {
			s.noteOwnerVisibleText(ev.Text)
		}
	} else {
		// 🎯T481: mapping failed — still persist the raw event.
		s.persistChatLine(losslessLine(ev))
	}
	if ev.IsTerminalStop() {
		s.clearOverseerStreamID()
	}
	s.HandleAgentEvent(ev)
}

// overseerWorkingLevel reports whether an owner-visible overseer turn is
// currently in flight (🎯T272 + 🎯T291). Level-trigger sample for history_meta.
//
// Fleet note chews set waiting / may keep PromptInFlight, but they must NOT
// light owner working chrome. Only an in-flight owner turn (overseerOwnerTurn)
// counts — after the owner's reply seals, chrome stays clear while the
// overseer drains the fleet backlog.
func (s *Server) overseerWorkingLevel() bool {
	s.mu.RLock()
	ownerTurn := s.overseerOwnerTurn
	waiting := s.waiting
	streamOpen := s.overseerStreamID != ""
	proc := s.proc
	s.mu.RUnlock()
	if !ownerTurn {
		return false
	}
	if waiting || streamOpen {
		return true
	}
	if proc != nil && proc.Alive() && proc.PromptInFlight() {
		return true
	}
	return false
}

// SendToOverseer delivers text to the current overseer process.
//
// 🎯T62: never drop on busy — enqueue and drain.
// 🎯T291: fleet notes coalesce per worker; owner turns jump ahead of the
// fleet backlog and interrupt a fleet-only in-flight prompt so the owner's
// words are not deferred behind idle churn.
func (s *Server) SendToOverseer(text string) error {
	// The owner talking to Jevons is the strongest owner-present signal —
	// feed the budget dead-man's switch so it never stops a fleet the
	// owner is actively directing.
	if s.activityHook != nil {
		s.activityHook()
	}
	owner := isOwnerNotifyText(text)

	s.mu.Lock()
	if owner {
		// Owner turns never coalesce with each other; append then peel first
		// at drain (partition). Keep enqueue order among owners.
		s.notifyQueue = append(s.notifyQueue, text)
	} else {
		s.notifyQueue = coalesceNotifyEnqueue(s.notifyQueue, text)
	}
	depth := len(s.notifyQueue)
	// Fleet-only busy: interrupt so the owner can take the session (T291).
	// Do not interrupt an owner turn — cancel-and-send is the chat client's job.
	fleetBusy := s.waiting && !s.overseerOwnerTurn
	s.mu.Unlock()

	// 🎯T128.3: enqueue is Info so queue growth is visible at production default.
	slog.Info("notify_queue",
		"component", "notify_queue",
		"decision", "enqueue",
		"depth", depth,
		"owner", owner,
	)

	if owner && fleetBusy {
		s.interruptOverseerForOwner()
		// Terminal stop will drain with owner-first partition; still try now
		// in case the session is already free.
	}

	s.drainOverseerNotes()
	return nil
}

// interruptOverseerForOwner cancels a fleet-note chew so an owner turn can
// claim the ACP session (🎯T291). No-op when notifySender is stubbed (tests
// drive busy via the seam) or the process is missing.
func (s *Server) interruptOverseerForOwner() {
	if s.notifySender != nil {
		// Hermetic path: tests model busy via notifySender; no real process.
		// Callers re-drain on simulated terminal / idle.
		return
	}
	proc := s.CurrentProcess()
	if proc == nil || !proc.Alive() {
		return
	}
	if err := proc.Interrupt(); err != nil {
		slog.Info("notify_queue",
			"component", "notify_queue",
			"decision", "owner_interrupt",
			"err", err,
		)
		return
	}
	slog.Info("notify_queue",
		"component", "notify_queue",
		"decision", "owner_interrupt",
		"ok", true,
	)
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

// drainOverseerNotes attempts to deliver queued async notifications.
//
// 🎯T291:
//   - Owner turns drain first, one at a time (never mixed with fleet notes).
//   - Fleet notes drain as one batch (already coalesced per worker).
//   - Successful fleet drain with a pending owner interrupts so owner is not
//     stuck behind the fleet chew.
//   - overseerOwnerTurn is set only for owner batches (working chrome).
//
// Busy / unreachable: requeue at front (coalesced) for the next turn-complete
// (🎯T62 — nothing dropped). Drain-in-progress serializes concurrent callers.
func (s *Server) drainOverseerNotes() {
	s.mu.Lock()
	if s.notifyDraining || len(s.notifyQueue) == 0 {
		s.mu.Unlock()
		return
	}
	batch, rest, ownerBatch := takeNotifyDrainBatch(s.notifyQueue)
	s.notifyQueue = rest
	s.notifyDraining = true
	s.mu.Unlock()

	if len(batch) == 0 {
		s.mu.Lock()
		s.notifyDraining = false
		s.mu.Unlock()
		return
	}

	err := s.sendNotes(strings.Join(batch, "\n\n"))

	s.mu.Lock()
	s.notifyDraining = false
	if err != nil {
		// Overseer busy or down — put the batch back at the front so order
		// is preserved, re-coalesce with anything that arrived during send.
		s.notifyQueue = requeueNotifyFront(batch, s.notifyQueue)
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
			"owner_batch", ownerBatch,
		)
		return
	}
	// Successful prompt delivery: mark waiting so stuck-busy can see an
	// in-flight turn even when only notify/owner notes are on the wire.
	s.waiting = true
	s.overseerOwnerTurn = ownerBatch
	s.overseerLastProgress = time.Now()
	depth := len(s.notifyQueue)
	ownerPending := queueHasOwner(s.notifyQueue)
	s.mu.Unlock()
	if ownerBatch {
		// 🎯T355: the prompt left the queue into the overseer's session —
		// the server-ack half of send-landed.
		s.NoteOwnerDelivered()
	}
	slog.Info("notify_queue",
		"component", "notify_queue",
		"decision", "drain",
		"depth", depth,
		"drained", len(batch),
		"owner_batch", ownerBatch,
	)
	// Fleet batch just took the session but an owner is waiting — free it.
	if !ownerBatch && ownerPending {
		s.interruptOverseerForOwner()
	}
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
	agent, lerr := fleet.LaunchRecovering(reg, s.overseerName)
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
	// AsideKind delineates purpose=aside rows (side | capture | target) from
	// the create-time meta beside the aside workdir (🎯T365). The RHS paints
	// 🎯 chrome on target filings and 💡 on idea/capture asides; serving it
	// from the feed keeps the icon right across a hard reload. Empty for
	// non-asides and for pre-🎯T270 asides with no meta.
	AsideKind string `json:"aside_kind,omitempty"`
	// TargetID is the bullseye target this agent is engaged on (🎯T198).
	// Empty when not mission-bound. UI merges with /api/frontier by equality.
	TargetID string `json:"target_id,omitempty"`
	// Ledger is the ledger key that owns TargetID — derived from WorkDir, not
	// declared (🎯T389). The overlay merges on (ledger, target_id), because
	// ids are per-ledger: without this, claudia's 🎯T19 worker marks
	// orthograph's 🎯T19 row engaged. Empty only when WorkDir is empty, and an
	// empty key matches every ledger (unscoped, pre-T389 reading).
	Ledger   string `json:"ledger,omitempty"`
	Status   string `json:"status"`
	Phase    string `json:"phase,omitempty"`
	Step     string `json:"step,omitempty"`
	Progress string `json:"progress,omitempty"`
	// Provider / Model back the RHS company-icon + condensed model prefix
	// (🎯T287). Provider is the agent's stored backend (claude | grok | …).
	// Model is session truth for the badge (🎯T324): live observation,
	// else session log, else launch pin / provider default bound at
	// Launch for this SessionID. Empty only when the backend has no
	// known default and nothing has been observed yet.
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
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
	return listFleetAgentsNotifying(reg, nil, nil, nil)
}

// listFleetAgentsNotifying is the same feed with an optional notify hook for
// recovery events (server wires agents_changed). Used by hermetic tests with
// notify=nil. progress may be nil (no ACP snapshots); models may be nil (no
// session-log lookup — rows then carry only what the live hub saw).
func listFleetAgentsNotifying(reg *claudia.Registry, onRecovered func(names []string), progress *AgentProgressHub, models *fleetModelResolver) []agentInfo {
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
	// 🎯T311: agents that left the registry (kill/remove) must not leave a
	// sticky observation behind — the next agent to answer to that name would
	// inherit the dead one's model. Pruning here covers every kill path
	// without each one having to notify the hub.
	if progress != nil {
		registered := make(map[string]struct{}, len(defs))
		for _, d := range defs {
			registered[d.Name] = struct{}{}
		}
		progress.Prune(registered)
	}
	agents := make([]agentInfo, 0, len(defs))
	// 🎯T389: resolving a workdir to its ledger walks the filesystem, and this
	// feed is rebuilt on every poll. Fleets are workdir-clustered, so one memo
	// per rebuild collapses the walk to one per repo.
	ledgerOf := make(map[string]string, len(defs))
	for _, d := range defs {
		status := "stopped"
		if proc := reg.Get(d.Name); proc != nil && proc.Alive() {
			status = "running"
			// 🎯T412: a live process whose registry row claims a conversation
			// that is not on disk is a dead seat, not a running agent. The
			// mint window is safe: an ACP working snapshot in the progress
			// hub outranks this baseline (SetStatus never clobbers it).
			if fleet.SessionLost(&d) {
				status = fleet.StatusDeadUnmaterialized
			}
		}
		purpose := d.Purpose
		if purpose == "" {
			purpose = claudia.PurposeWork
		}
		// Seed status baseline when no richer ACP snapshot exists yet.
		// Epoch first (🎯T311): a restart on a fresh session drops the previous
		// run's observation before the baseline can carry it forward.
		if progress != nil {
			progress.SyncEpoch(d.Name, d.SessionID)
			progress.SetStatus(d.Name, status)
		}
		ledger, seen := ledgerOf[d.WorkDir]
		if !seen {
			ledger = targetfile.LedgerKey(d.WorkDir)
			ledgerOf[d.WorkDir] = ledger
		}
		info := agentInfo{
			Name:        d.Name,
			WorkDir:     d.WorkDir,
			Parent:      d.Parent,
			Purpose:     purpose,
			Description: d.Description,
			TargetID:    strings.TrimSpace(d.TargetID),
			Ledger:      ledger,
			Status:      status,
			Provider:    strings.TrimSpace(string(d.Provider)),
		}
		// 🎯T365: target filings and idea/capture asides share purpose=aside;
		// the create-time meta beside the workdir is what tells them apart.
		if purpose == claudia.PurposeAside {
			info.AsideKind = asideKindFromWorkDir(d.WorkDir)
		}
		if progress != nil {
			p := progress.Get(d.Name)
			if p.Summary != "" || p.Phase != "" || p.Step != "" {
				info.Phase = p.Phase
				info.Step = p.Step
				info.Progress = p.Summary
			}
			// What the agent is running, seen on the wire this session.
			info.Model = strings.TrimSpace(p.Model)
			// 🎯T348 belt: a hub poisoned before the modelFromEvent filter
			// existed (or by any future synthetic producer) must not serve
			// '<synthetic>' as a model — drop it so the log/pin chain engages.
			if info.Model == syntheticModel {
				progress.ClearModel(d.Name)
				info.Model = ""
			}
			// 🎯T323: drop sticky observations that belong to another company
			// (Claude-era fable under provider=grok after migrate). Clear the
			// hub so the next poll does not re-serve the foreign id.
			if info.Model != "" && !modelFitsProvider(info.Provider, info.Model) {
				progress.ClearModel(d.Name)
				info.Model = ""
			}
		}
		// Nothing observed live — either the provider names no model in its
		// frames (Grok, 🎯T293) or this daemon has not seen a turn yet, which
		// is every agent right after a restart. The harness's own session log
		// says what the process actually ran, so it seeds the badge at attach
		// instead of leaving a live agent blank (🎯T311).
		if info.Model == "" {
			info.Model = models.Model(d.Provider, d.WorkDir, d.SessionID)
		}
		// Launch pin / session binding (🎯T311 / 🎯T324). Intent for this
		// SessionID — fills the gap before the first observation, never
		// overrides one.
		if info.Model == "" {
			info.Model = strings.TrimSpace(d.Model)
		}
		// 🎯T323 residual belt: drop foreign-company residue if it still
		// reaches the feed. Product strategy is session-truth binding at
		// Launch/migrate (T324), not fail-closed sniff.
		if info.Model != "" && !modelFitsProvider(info.Provider, info.Model) {
			info.Model = ""
		}
		// 🎯T324: unbound → provider default so cold Grok agents report a
		// condensable id (badge version), not mark-only forever.
		if info.Model == "" {
			info.Model = cli.DefaultModelForProvider(d.Provider)
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
	models := s.fleetModels
	s.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	if reg == nil {
		_ = json.NewEncoder(w).Encode([]agentInfo{})
		return
	}
	agents := listFleetAgentsNotifying(reg, func(names []string) {
		// 🎯T85: push UI refresh + optional client-visible signal after recovery.
		s.NotifyAgentsChanged()
	}, s.agentProgress, models)
	_ = json.NewEncoder(w).Encode(agents)
}

// handleChatControlFrame consumes a client→server protocol frame arriving on
// /ws/chat and reports whether msg was one.
//
// Control frames are machine wire, never an owner turn. A frame that falls
// through to the owner-turn path below is echoed, journaled as a durable chat
// bubble, and delivered to the overseer — which is exactly how literal
// {"type":"ux_state","composer_blocked":false} ended up in the owner's main
// chat (🎯T362). It lives as its own method so that boundary is decidable
// without a live overseer process.
//
// conn may be nil for frames that never write back (ux_state); every frame
// that answers the client dereferences it.
func (s *Server) handleChatControlFrame(ctx context.Context, conn *websocket.Conn, ch chan string, msg string) bool {
	// Heartbeat ping from the resilient browser transport. Echo
	// a pong so the watchdog stays quiet; do NOT forward to
	// Claude (it would be parsed as a chat turn).
	if msg == `{"type":"ping"}` {
		// 🎯T355: a ticking client is the level evidence that the owner
		// can still drive the UI; a frozen main thread stops pinging.
		s.NoteOwnerUIHeartbeat()
		writeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		_ = conn.Write(writeCtx, websocket.MessageText, []byte(`{"type":"pong"}`))
		cancel()
		return true
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
				return true
			}
		}
		s.settleCancel()
		return true
	}

	// Control frames are JSON objects with a "type" field. Match on the
	// unmarshalled type — never HasPrefix on key order (Go map marshal and
	// probes may emit {"name":…,"type":"inspect_subscribe"} which must not
	// fall through into owner chat / overseer as a user turn) (🎯T209).
	if !strings.HasPrefix(msg, "{") {
		return false
	}
	var ctl struct {
		Type            string `json:"type"`
		Name            string `json:"name"`
		Turns           int    `json:"turns"`
		ComposerBlocked bool   `json:"composer_blocked"`
		Reason          string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(msg), &ctl); err != nil || ctl.Type == "" {
		return false
	}
	switch ctl.Type {
	case "ux_state":
		// 🎯T361: the client reports whether the owner can actually submit.
		// Heartbeats only prove the page is ticking — a live tab with a
		// refusing composer is exactly the UX degrade this level observes.
		s.NoteOwnerComposerBlocked(ctl.ComposerBlocked, ctl.Reason)
		return true
	case "rewind":
		// Roll conversation back N user turns; tell clients to trim.
		slog.Info("chat: rewind", "turns", ctl.Turns)
		if err := s.RewindOverseer(ctl.Turns); err != nil {
			slog.Error("chat: rewind failed", "err", err)
			return true
		}
		s.broadcastChatLive(fmt.Sprintf(`{"type":"rewound","turns":%d}`, ctl.Turns))
		return true
	case "inspect_subscribe", "inspect_unsubscribe", "subscribe", "unsubscribe":
		// 🎯T209: RHS agent inspect multiplex on /ws/chat.
		name := strings.TrimSpace(ctl.Name)
		if ctl.Type == "inspect_unsubscribe" || ctl.Type == "unsubscribe" || name == "" {
			s.setInspectSub(ch, "")
			slog.Info("chat: inspect unsubscribe")
			return true
		}
		s.setInspectSub(ch, name)
		slog.Info("chat: inspect subscribe", "name", name)
		if err := s.writeInspectReplay(ctx, conn, name); err != nil {
			slog.Warn("chat: inspect replay failed", "name", name, "err", err)
		}
		return true
	}
	// An unrecognised typed frame is NOT swallowed: the owner may legitimately
	// paste JSON into chat, and a silent drop would be a vanished turn. It
	// falls through to the owner-turn path; the client-side paint filter
	// (🎯T362) is what keeps a *leaked* frame out of the bubble stream.
	return false
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
			// 🎯T382: pre-fix journals hold provider echoes of owner turns;
			// replaying one repaints the message (marker and images) twice.
			if echoedOwnerTurnLine(line) {
				return nil
			}
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
			// 🎯T272: include current in-flight level so clients re-derive working
			// chrome after reload (level-trigger, not edge memory).
			meta, _ := json.Marshal(map[string]any{
				"type": "history_meta", "older": start, "total": total, "start": start,
				"conn_id": connID, "replay_frames": frames, "replay_bytes": bytes, "replay_ms": replayMS,
				// 🎯T355: this sample is also what the client will paint,
				// so it is the chrome level the server has published.
				"working":  s.publishedWorkingLevel(),
				"owner_ux": s.ownerUXLevel(),
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
		if s.handleChatControlFrame(ctx, conn, ch, msg) {
			continue
		}

		slog.Info("chat: received", "msg", msg)
		if _, err := s.sendToNamedAgentAs(s.overseerAgentName(), msg, sendOriginOwner); err != nil {
			// Failure already journaled/broadcast inside sendToOverseerAsOwner.
			_ = err
		}
	}
}

// settleCancel ends the cancelled turn on the server and tells clients so.
//
// Providers disagree about what an interrupted turn emits: Grok's ACP
// session reports a turn end, while a Claude Session is a TUI that simply
// stops — no terminal event ever arrives. Waiting for the provider
// therefore leaves the owner's thinking indicator spinning after Esc on
// any Claude agent, so the settle signal comes from the server (🎯T282).
// Clients already treat state=cancel_settled as "not working"
// (web/scripts/chat_events.js workingLevelFromSample).
func (s *Server) settleCancel() {
	s.mu.Lock()
	s.turnBuf = ""
	s.waiting = false
	s.overseerOwnerTurn = false // 🎯T291
	s.overseerStreamID = ""
	s.overseerStreamAcc = ""
	s.overseerStreamSilent = false
	s.overseerStreamHold = nil
	s.mu.Unlock()

	s.NoteOverseerProgress()
	// 🎯T355: a cancelled turn owes no reply — that is a named residual,
	// and the settle frame is the chrome level the clients now paint.
	s.NoteOwnerResidual("cancelled_by_owner")
	s.noteChromePublished(false)
	frame := map[string]any{"type": "status", "state": "cancel_settled", "text": "cancelled"}
	if payload, err := json.Marshal(frame); err == nil {
		s.BroadcastChat(string(payload))
	}
	s.Broadcast(frame)
	// Notes deferred while the turn held the session can go out now.
	s.drainOverseerNotes()
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
func (s *Server) persistChatLine(line string) {
	if s == nil || strings.TrimSpace(line) == "" {
		return
	}
	s.mu.Lock()
	clog := s.chatLog
	s.mu.Unlock()
	if clog == nil {
		return
	}
	if err := clog.Append(line); err != nil {
		slog.Error("chat: DURABILITY FAILURE — chat log append failed", "err", err)
		return
	}
	s.noteChatJournaled(line)
}

func (s *Server) BroadcastChat(line string) {
	s.persistChatLine(line)
	s.broadcastChatLive(stampConversationName(line, s.overseerAgentName()))
}

// broadcastChatLive fans a line out to connected clients WITHOUT
// journaling it. Only for frames whose durable effect is already in the
// journal (the rewound control frame after TruncateTurns — journaling it
// too would double-trim replayed views).
func (s *Server) broadcastChatLive(line string) {
	s.mu.Lock()
	listeners := append([]chan string(nil), s.chatListeners...)
	s.mu.Unlock()
	for _, ch := range listeners {
		select {
		case ch <- line:
		default:
		}
	}
	s.muxFanTranscript(s.overseerAgentName(), line)
}

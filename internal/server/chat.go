// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/marcelocantos/jevons/internal/seatstate"

	"github.com/coder/websocket"
	"github.com/google/uuid"
	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/agenterr"
	"github.com/marcelocantos/jevons/internal/briefaddr"
	"github.com/marcelocantos/jevons/internal/chatlog"
	"github.com/marcelocantos/jevons/internal/delivery"
	"github.com/marcelocantos/jevons/internal/discovery"
	"github.com/marcelocantos/jevons/internal/fleet"
	"github.com/marcelocantos/jevons/internal/fleetlog"
	"github.com/marcelocantos/jevons/internal/seatactivity"
	"github.com/marcelocantos/jevons/internal/seatstop"
	"github.com/marcelocantos/jevons/internal/silentresponse"
	"github.com/marcelocantos/jevons/internal/targetfile"
	"github.com/marcelocantos/jevons/internal/upgrade"
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
	s.proc = proc
	s.mu.Unlock()
	a := s.seats.Load()
	if a == nil {
		a = seatstate.New(seatstate.Args{})
		s.SetSeats(a)
	}
	a.ObserveProcess(s.overseerAgentName(), proc)
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

	// 🎯T567: "overseer is back" answers a down state this client saw —
	// a tick that found the process dead, or a down error we wrote it.
	// A reconnect that finds the overseer already alive attaches silently.
	sawDown := false
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if cur := s.CurrentProcess(); cur != nil && (s.seatState(s.overseerSeatName()).Alive == seatstate.Yes) {
				slog.Info("chat: overseer recovered; attaching live stream", "announce", sawDown)
				if sawDown {
					payload, _ := json.Marshal(map[string]string{
						"type": "status", "text": "overseer is back",
					})
					wctx, cancel := context.WithTimeout(ctx, 2*time.Second)
					_ = conn.Write(wctx, websocket.MessageText, payload)
					cancel()
				}
				return cur
			}
			sawDown = true
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
			sawDown = true
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
	reg, name := s.registry, s.overseerName
	s.mu.Unlock()
	seatstate.ObserveRegistrySeat(reg, name)
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
	if s.holdOverseerAdmission(ev) {
		return
	}
	s.deliverOverseerEventAdmitted(ev)
}

func (s *Server) deliverOverseerEventAdmitted(ev claudia.Event) (currentDurable bool) {
	// Current-event durability must never inherit a prior held-prefix write.
	// The aggregate is only for crediting text in those earlier held lines.
	if a := s.seats.Load(); a != nil {
		a.FromTurnEvent(s.overseerSeatName(), ev.IsTerminalStop(), time.Now())
	}
	// Any ACP traffic resets stuck-busy idle (🎯T204).
	s.NoteOverseerProgress()

	// 🎯T406: transport-level provider refusals on the overseer wire enter
	// (or leave alone) the fleet hard-block. Authored prose never fires —
	// ClassifyFrom(SourceAuthored) is ClassNone (🎯T455).
	// The mirror: authored assistant text is the provider accepting a turn,
	// which is what clears the hard-block. Without it only an owner chat send
	// cleared it, so a block entered on this wire outlived the provider's
	// recovery while the overseer kept answering its POs (2026-09-28, 🎯T885).
	if !s.overseerAdmissionEnabled() && ev.Type == "assistant" && ev.Text != "" {
		source := assistantTextSource(ev)
		if class := classifyAssistantEvent(ev); class.IsFailure() {
			s.observeProviderFailure(class, ev.Text)
		} else if source == agenterr.SourceAuthored {
			s.observeProviderOK()
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
		if text, prose := userTurnText(ev); prose && s.consumeJournaledFleetHealth(text) {
			// Admission already wrote this fleet-health note. The provider
			// echo is the same text, not a second episode.
			s.handleAdmissionSafeAgentEvent(ev, currentDurable)
			return currentDurable
		}
		if text, _ := userTurnText(ev); text != "" {
			if err := briefaddr.Check(s.overseerAgentName(), text); err != nil {
				slog.Warn("chat: dropping wrong-seat brief from overseer wire (🎯T513)", "err", err)
				s.handleAdmissionSafeAgentEvent(ev, currentDurable)
				return currentDurable
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
					s.broadcastAdmittedChat(held)
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
				if !s.overseerAdmissionEnabled() {
					s.persistChatLine(losslessLine(ev))
				}
			}
			if ev.IsTerminalStop() {
				if line := emptyEndTurnWire(ev.StopReason, streamID); line != "" {
					currentDurable = s.broadcastAdmittedChat(line)
				}
				s.clearOverseerStreamID()
				// 🎯T919: a silent turn still ends. Returning before the
				// phase reduce left whatever the turn last showed (a
				// tool, a pane preview) published until the next turn.
				s.applyOverseerEventPhase(s.admissionSafeEvent(ev, false))
				if s.ObserveAgentProgress(s.overseerAgentName(), s.admissionSafeEvent(ev, false)) {
					s.NotifyAgentsChanged()
				}
			}
			s.handleAdmissionSafeAgentEvent(ev, false)
			return false
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
			s.handleAdmissionSafeAgentEvent(ev, currentDurable)
			return currentDurable
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
				s.broadcastAdmittedChat(h)
			}
			// 🎯T378: held fragments proved visible on seal — the owner can
			// read them, so this turn answered.
			if len(held) > 0 && !s.overseerAdmissionEnabled() {
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
		currentDurable = s.broadcastAdmittedChat(line)
		// 🎯T378: reaching here with assistant prose means the stream was not
		// silent — every silent path returned above — so this is text the
		// owner actually sees, which is the only thing that answers a
		// question. A seal alone never gets to make that claim.
		if ev.Type == "assistant" && (currentDurable || !s.overseerAdmissionEnabled()) {
			s.noteOwnerVisibleText(ev.Text)
		}
	} else {
		// 🎯T481: mapping failed — still persist the raw event.
		if !s.overseerAdmissionEnabled() {
			s.persistChatLine(losslessLine(ev))
		}
	}
	if ev.IsTerminalStop() {
		s.clearOverseerStreamID()
	}
	if s.overseerAdmissionEnabled() && currentDurable && ev.Type == "assistant" && ev.Text != "" {
		source := assistantTextSource(ev)
		if class := classifyAssistantEvent(ev); class.IsFailure() {
			s.observeProviderFailure(class, ev.Text)
		} else if source == agenterr.SourceAuthored {
			s.observeProviderOK()
		}
	}
	// 🎯T555.1: interleave the phase reduce on the same stream, same clock,
	// after the bubble frame it describes so the message stays line-first.
	// 🎯T919: and before HandleAgentEvent, whose terminal-stop settle drains
	// the next queued batch and stamps it accepted; the finished turn's idle
	// landing after that stamp would paint the new turn as idle.
	s.applyOverseerEventPhase(s.admissionSafeEvent(ev, currentDurable))
	s.handleAdmissionSafeAgentEvent(ev, currentDurable)
	// The fleet row reads AgentProgressHub, which workers fill from the
	// MCP event hook. The overseer stream never went through that hook,
	// so GET /api/agents kept phase=idle through a live Grok turn while
	// the status bar said thinking.
	if s.ObserveAgentProgress(s.overseerAgentName(), s.admissionSafeEvent(ev, currentDurable)) {
		s.NotifyAgentsChanged()
	}
	return currentDurable
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
	if s.seatInFlight(s.overseerSeatName(), proc) {
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
	return s.SendToOverseerAs(text, "")
}

// ownerReofferIsDuplicateInFlight reports whether text is exactly the owner
// turn currently in flight to the overseer (🎯T1120). Scoped deliberately
// narrow: only a system re-offer (caller already checked msgID == "") of the
// SAME text as the live overseerOwnerTurnText, while that turn has not yet
// sealed. Once the turn completes, overseerOwnerTurnText is cleared and a
// later identical text — genuine repeat or late re-offer — is delivered
// normally; this guard only stops the race against an answer already under way.
func (s *Server) ownerReofferIsDuplicateInFlight(text string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.waiting && s.overseerOwnerTurn && s.overseerOwnerTurnText != "" && s.overseerOwnerTurnText == text
}

// SendToOverseerAs is SendToOverseer for an owner turn that has a message id
// (🎯T806): the id is registered in the same lock hold that enqueues the text.
func (s *Server) SendToOverseerAs(text, msgID string) error {
	_, err := s.sendToOverseerAs(text, msgID)
	return err
}

// sendToOverseerAs is SendToOverseerAs reporting the batch, if any, that this
// call's own drain handed to the overseer.
func (s *Server) sendToOverseerAs(text, msgID string) (*sentNotifyBatch, error) {
	owner := isOwnerNotifyText(text)
	// The owner talking to Jevons is the strongest owner-present signal —
	// feed the budget dead-man's switch so it never stops a fleet the
	// owner is actively directing. System notices are not owner contact:
	// a budget notice can arrive while the enforcer owns its lock, and
	// calling its heartbeat here would re-enter that lock (🎯T623.1).
	if owner && s.activityHook != nil {
		s.activityHook()
	}

	// 🎯T1120: a system re-offer of the owner's own words (msgID == "" —
	// 🎯T915's comment names this: "owner-health re-injections carry none")
	// must not mint a second provider turn while the first is still the
	// in-flight owner turn it is re-offering. A genuine fresh owner click
	// always carries a msgID, so this only ever catches a re-offer racing
	// the answer it was trying to rescue — the 2026-10-21 20:56 specimen
	// where a duplicate "Same status as my last summary" bubble followed a
	// correct answer by about 30s. This does not touch a deliberate repeat
	// question the owner sends after the turn has actually completed: by
	// then overseerOwnerTurnText is cleared and the text no longer matches.
	if owner && msgID == "" && s.ownerReofferIsDuplicateInFlight(text) {
		slog.Info("notify_queue",
			"component", "notify_queue",
			"decision", "dedup_reoffer_inflight",
		)
		return nil, nil
	}

	s.mu.Lock()
	released := false
	if owner && msgID != "" && s.ownerCancelHold {
		// 🎯T915: the owner's first send after their cancel is the next
		// turn — ahead of anything queued while the cancelled turn ran,
		// owner words included. Only a send with a message id is the
		// owner's; owner-health re-injections carry none.
		s.notifyQueue = append([]string{text}, s.notifyQueue...)
		s.observeOwnerQueueLocked()
		s.releaseOwnerCancelHoldLocked()
		released = true
		s.registerOwnerMessageIDLocked(text, msgID)
	} else if owner {
		// Owner turns never coalesce with each other; append then peel first
		// at drain (partition). Keep enqueue order among owners.
		s.notifyQueue = append(s.notifyQueue, text)
		s.observeOwnerQueueLocked()
		if msgID != "" {
			s.registerOwnerMessageIDLocked(text, msgID)
		}
	} else {
		s.notifyQueue = coalesceNotifyEnqueue(s.notifyQueue, text)
		s.observeOwnerQueueLocked()
	}
	depth := len(s.notifyQueue)
	// Fleet-only busy: interrupt so the owner can take the session (T291).
	// Do not interrupt an owner turn — cancel-and-send is the chat client's job.
	fleetBusy := s.waiting && !s.overseerOwnerTurn
	s.mu.Unlock()
	if owner {
		s.persistOwnerQueue() // 🎯T806: a bounce must not lose a displayed message
	}

	// 🎯T128.3: enqueue is Info so queue growth is visible at production default.
	slog.Info("notify_queue",
		"component", "notify_queue",
		"decision", "enqueue",
		"depth", depth,
		"owner", owner,
		"released_cancel_hold", released,
	)

	if owner && fleetBusy {
		// A seat that settles on interrupt is free now; otherwise its
		// terminal stop will drain with owner-first partition. Try now
		// either way in case the session is already free.
		s.interruptOverseerForOwner()
	}

	// Journal before the drain sends. The provider echo, if one arrives,
	// has to find the latch already set or the transcript stores the note twice.
	if !owner {
		s.journalFleetHealthNote(text)
	}

	sent := s.drainOverseerNotesSent()
	// A working fleet still returns nil when the seat is busy: the text is
	// queued and the caller hears delivered_unconfirmed. A non-working fleet
	// must not. The drain above leaves the text in the queue either way.
	if !s.notifyDrainAllowed() {
		return nil, delivery.ErrNotifyHeldByIntent
	}
	return sent, nil
}

// fleetHealthNotePrefix is the marker notifyFleetHealth puts on a notice.
// A coalesced repeat never reaches SendToOverseer, so it is not journaled.
const fleetHealthNotePrefix = "[Fleet health]"

func isFleetHealthNote(text string) bool {
	return strings.HasPrefix(strings.TrimSpace(text), fleetHealthNotePrefix)
}

// journalFleetHealthNote writes one admitted fleet-health episode into the
// overseer transcript. Queue acceptance is the episode boundary: the sweep
// coalesces later copies, and a busy overseer must not hide the alert until
// its current turn ends.
func (s *Server) journalFleetHealthNote(text string) {
	if s == nil || !isFleetHealthNote(text) {
		return
	}
	text = strings.TrimSpace(text)
	s.mu.Lock()
	if s.fleetHealthJournaled == nil {
		s.fleetHealthJournaled = map[string]int{}
	}
	s.fleetHealthJournaled[text]++
	s.mu.Unlock()
	line, err := json.Marshal(map[string]any{
		"type":      "agent_note",
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		"text":      text,
	})
	if err != nil || !s.persistChatLine(string(line)) {
		s.consumeJournaledFleetHealth(text)
	}
}

// consumeJournaledFleetHealth reports whether text was journaled at
// admission and not yet echoed. The caller skips the echo so the
// transcript keeps one copy.
func (s *Server) consumeJournaledFleetHealth(text string) bool {
	if s == nil {
		return false
	}
	text = strings.TrimSpace(text)
	s.mu.Lock()
	defer s.mu.Unlock()
	n := s.fleetHealthJournaled[text]
	if n == 0 {
		return false
	}
	if n == 1 {
		delete(s.fleetHealthJournaled, text)
	} else {
		s.fleetHealthJournaled[text] = n - 1
	}
	return true
}

// interruptOverseerForOwner cancels a fleet-note chew so an owner turn can
// claim the ACP session (🎯T291). No-op when notifySender is stubbed (tests
// drive busy via the seam, or ownerInterruptSeam) or the process is missing.
//
// It reports whether the chew was settled here. A live seat is never settled
// here: every provider runs on the omp agent core, whose sidecar answers an
// abort with turn_end "aborted", and a second prompt sent before that stop can
// be refused asynchronously (🎯T623). Only the test seam may settle directly.
// (The Claude TUI that stopped on Esc with no terminal event, 🎯T282/T915,
// is gone.)
func (s *Server) interruptOverseerForOwner() bool {
	settles := false
	if seam := s.ownerInterruptSeam; seam != nil {
		ok, err := seam()
		if err != nil {
			slog.Info("notify_queue",
				"component", "notify_queue",
				"decision", "owner_interrupt",
				"err", err,
			)
			return false
		}
		settles = ok
	} else {
		if s.notifySender != nil {
			// Hermetic path: tests model busy via notifySender; no real process.
			// Callers re-drain on simulated terminal / idle.
			return false
		}
		proc := s.CurrentProcess()
		if proc == nil || !(s.seatState(s.overseerSeatName()).Alive == seatstate.Yes) {
			return false
		}
		if err := upgrade.WithReadopt(context.Background(), s.overseerAgentName(), proc,
			func(a *claudia.Agent) error { return a.Interrupt() }); err != nil {
			slog.Info("notify_queue",
				"component", "notify_queue",
				"decision", "owner_interrupt",
				"err", err,
			)
			return false
		}
	}
	settled := settles && s.settleFleetChew()
	slog.Info("notify_queue",
		"component", "notify_queue",
		"decision", "owner_interrupt",
		"ok", true,
		"settled", settled,
	)
	return settled
}

// settleFleetChew ends an interrupted fleet-note turn on the server. It
// touches only a fleet chew: an owner turn that began in the meantime is the
// owner's, and settling it would let a second prompt in over it.
func (s *Server) settleFleetChew() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.waiting || s.overseerOwnerTurn {
		return false
	}
	s.turnBuf = ""
	s.waiting = false
	s.overseerStreamID = ""
	s.overseerStreamAcc = ""
	s.overseerStreamSilent = false
	s.overseerStreamHold = nil
	return true
}

// SetRequestMissionRecorder installs the host write that freezes the
// overseer's target onto a transport request before handoff.
func (s *Server) SetRequestMissionRecorder(fn func(ctx context.Context, seat, requestID, mission string) error) {
	s.mu.Lock()
	s.recordRequestMission = fn
	s.mu.Unlock()
}

// ownerHandoffRequestID is the host id minted when this owner turn was
// accepted. A fleet batch has none here. The id is peeked, not popped:
// a failed handoff must leave it for retry, and a later identical text
// must not take an earlier pending id.
func (s *Server) ownerHandoffRequestID(text string) string {
	if !isOwnerNotifyText(text) {
		return ""
	}
	return s.peekOwnerMessageID(text)
}

// fleetBatchRequestID is the host id for one composed fleet prompt.
// The notes are not one request until drain joins them, so the id is
// minted at that handoff. The same bytes keep the id when the handoff
// fails. A different composition is a different request.
func (s *Server) fleetBatchRequestID(text string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if text != "" && s.notifyFleetBatchText == text && s.notifyFleetBatchID != "" {
		return s.notifyFleetBatchID
	}
	id := newTrackedRequestID("fn")
	s.notifyFleetBatchText = text
	s.notifyFleetBatchID = id
	return id
}

func (s *Server) forgetFleetBatchRequestID(text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.notifyFleetBatchText == text {
		s.notifyFleetBatchText = ""
		s.notifyFleetBatchID = ""
	}
}

// untrackedOwnerRequestID is the host id for an owner note that reached
// the queue without one. The same wire keeps the id across a failed
// handoff. A later admission after success is a new id.
func (s *Server) untrackedOwnerRequestID(text string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.notifyUntrackedOwnerIDs == nil {
		s.notifyUntrackedOwnerIDs = map[string]string{}
	}
	if id := s.notifyUntrackedOwnerIDs[text]; id != "" {
		return id
	}
	id := newTrackedRequestID("om")
	s.notifyUntrackedOwnerIDs[text] = id
	return id
}

func (s *Server) forgetUntrackedOwnerRequestID(text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.notifyUntrackedOwnerIDs, text)
}

// freezeOverseerMission writes the overseer's registry target against
// requestID before any provider call. An empty target is not stored.
// A nil recorder leaves the mission unknown. A write error must not send.
func (s *Server) freezeOverseerMission(requestID string) error {
	s.mu.RLock()
	name := s.overseerName
	s.mu.RUnlock()
	return s.freezeSeatMission(name, requestID)
}

// freezeSeatMission writes one seat's registry target against requestID
// before any provider call. An empty target is not stored. A nil recorder
// leaves the mission unknown. A write error must not send.
func (s *Server) freezeSeatMission(seat, requestID string) error {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return nil
	}
	s.mu.RLock()
	record := s.recordRequestMission
	reg := s.registry
	s.mu.RUnlock()
	if record == nil {
		return nil
	}
	mission := ""
	seat = strings.TrimSpace(seat)
	if reg != nil {
		if d := reg.Def(seat); d != nil {
			mission = strings.TrimSpace(d.TargetID)
		}
	}
	return record(context.Background(), seat, requestID, mission)
}

// sendNotes delivers one coalesced note batch to the overseer. The
// notifySender seam lets tests stub delivery; nil uses the live process.
// An owner batch carries the id registered at acceptance. A fleet batch
// mints an fn- id for the composed prompt and reuses it when that same
// text is retried. An owner note with no registered id, including an
// owner-health re-injection, mints an om- id under the same retry rule.
func (s *Server) sendNotes(text string) (string, error) {
	if err := s.injectedBrokerFault(text); err != nil {
		return "", err
	}
	requestID := s.ownerHandoffRequestID(text)
	fleet := false
	untrackedOwner := false
	if requestID == "" && !isOwnerNotifyText(text) {
		requestID = s.fleetBatchRequestID(text)
		fleet = true
	}
	if requestID == "" && isOwnerNotifyText(text) {
		requestID = s.untrackedOwnerRequestID(text)
		untrackedOwner = true
	}
	if requestID != "" {
		if err := s.freezeOverseerMission(requestID); err != nil {
			return requestID, err
		}
	}
	var err error
	switch {
	case s.overseerSendSeam != nil:
		err = s.overseerSendSeam(text, requestID)
	case s.notifySender != nil:
		err = s.notifySender(text)
	default:
		proc := s.CurrentProcess()
		if proc == nil || !(s.seatState(s.overseerSeatName()).Alive == seatstate.Yes) {
			return requestID, agenterr.ErrOverseerNotRunning()
		}
		err = upgrade.WithReadopt(context.Background(), s.overseerAgentName(), proc,
			func(a *claudia.Agent) error {
				if requestID == "" {
					return a.Send(text)
				}
				return a.SendWithRequestID(text, requestID)
			})
	}
	if err == nil && fleet {
		s.forgetFleetBatchRequestID(text)
	}
	if err == nil && untrackedOwner {
		s.forgetUntrackedOwnerRequestID(text)
	}
	return requestID, err
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
	if class := agenterr.NotifyClass(err); class != "" {
		return class
	}
	return "other"
}

// SetAllowNotifyDrain installs the fleet-intent gate for the overseer notify
// queue. A nil callback allows every drain.
func (s *Server) SetAllowNotifyDrain(allow func() bool) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.allowNotifyDrain = allow
	s.mu.Unlock()
}

// notifyDrainAllowed is true when no gate is installed or the gate allows
// a turn. The callback runs without s.mu held.
func (s *Server) notifyDrainAllowed() bool {
	if s == nil {
		return true
	}
	s.mu.Lock()
	allow := s.allowNotifyDrain
	s.mu.Unlock()
	if allow == nil {
		return true
	}
	return allow()
}

// DrainOverseerNotes delivers any queued overseer note the fleet intent
// currently allows. A blocked fleet leaves the queue untouched.
func (s *Server) DrainOverseerNotes() {
	if s == nil {
		return
	}
	s.drainOverseerNotes()
}

// notifyDrainStopLocked reports whether this drain must leave the queue
// alone. The caller holds s.mu. decision is set when the stop should be logged.
func (s *Server) notifyDrainStopLocked() (stop bool, decision string) {
	// A successful socket write does not mean the provider has accepted the
	// prompt. OMP can reject a second prompt asynchronously, so do not offer
	// another batch while the first turn is still open (🎯T623).
	if s.notifyDraining || s.waiting || len(s.notifyQueue) == 0 {
		return true, ""
	}
	// 🎯T915: after the owner's cancel, nothing queued runs until the
	// owner's own next send (which releases the hold) or the window lapses.
	if s.ownerCancelHold {
		return true, "hold_after_owner_cancel"
	}
	return false, ""
}

func (s *Server) logNotifyDrainStop(decision string) {
	if decision == "" {
		return
	}
	s.mu.Lock()
	depth := len(s.notifyQueue)
	s.mu.Unlock()
	slog.Info("notify_queue",
		"component", "notify_queue",
		"decision", decision,
		"depth", depth,
	)
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
	s.drainOverseerNotesSent()
}

// drainOverseerNotesSent is drainOverseerNotes reporting the fleet batch it
// handed to the overseer, or nil when it handed none (stopped, refused, or an
// owner batch, whose receipt is read below).
func (s *Server) drainOverseerNotesSent() *sentNotifyBatch {
	s.mu.Lock()
	if stop, decision := s.notifyDrainStopLocked(); stop {
		s.mu.Unlock()
		s.logNotifyDrainStop(decision)
		return nil
	}
	s.mu.Unlock()

	// 🎯T1066: the worker drain's hand-off gate, applied to the overseer
	// queue. Checked after the empty/busy/cancel stops, and before sendNotes,
	// so a blocked fleet never mints a receipt. Re-check the stops after the
	// callback: it runs without s.mu.
	if !s.notifyDrainAllowed() {
		s.mu.Lock()
		depth := len(s.notifyQueue)
		s.mu.Unlock()
		slog.Info("notify_queue",
			"component", "notify_queue",
			"decision", "hold_fleet_intent",
			"depth", depth,
		)
		return nil
	}

	s.mu.Lock()
	if stop, decision := s.notifyDrainStopLocked(); stop {
		s.mu.Unlock()
		s.logNotifyDrainStop(decision)
		return nil
	}
	batch, rest, ownerBatch := takeNotifyDrainBatch(s.notifyQueue)
	s.notifyQueue = rest
	s.observeOwnerQueueLocked()
	s.notifyDraining = true
	s.notifyInFlight = batch
	s.mu.Unlock()

	if len(batch) == 0 {
		s.mu.Lock()
		s.notifyDraining = false
		s.mu.Unlock()
		return nil
	}

	// 🎯T1091: the owner message id this batch's cockpit row carries; the
	// delivered announcement below retires it from notifyOwnerIDs.
	ownerMsgID := ""
	if ownerBatch {
		ownerMsgID = s.peekOwnerMessageID(batch[0])
	}
	requestID, err := s.sendNotes(strings.Join(batch, "\n\n"))

	s.mu.Lock()
	s.notifyDraining = false
	s.notifyInFlight = nil
	if err != nil {
		// Overseer busy or down — put the batch back at the front so order
		// is preserved, re-coalesce with anything that arrived during send.
		s.notifyQueue = requeueNotifyFront(batch, s.notifyQueue)
		s.observeOwnerQueueLocked()
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
		// 🎯T806: busy is ordinary queueing behind a turn. Anything else on an
		// owner batch is a refusal the owner must be told about, and retried.
		if ownerBatch && notifyErrClass(err) != "busy" {
			s.announceOwnerUndelivered(batch[0], err)
			s.scheduleNotifyRetry()
		}
		return nil
	}
	// Successful prompt delivery: mark waiting so stuck-busy can see an
	// in-flight turn even when only notify/owner notes are on the wire.
	s.waiting = true
	s.overseerOwnerTurn = ownerBatch
	s.overseerOwnerTurnText = ""
	s.clearOwnerTurnPlacementLocked()
	armPreempt := func() {}
	if ownerBatch {
		s.overseerOwnerTurnText = batch[0]
		armPreempt = s.noteOwnerHandoffLocked(requestID, ownerMsgID) // 🎯T1091
	}
	s.noteOverseerProgressLocked()
	depth := len(s.notifyQueue)
	ownerPending := queueHasOwner(s.notifyQueue)
	s.mu.Unlock()
	armPreempt()
	if ownerBatch {
		// The write returned. Send-landed and the owner-visible delivered
		// frame are the receipt for this request. Retiring the id is
		// separate: the bytes already left, so a restart must not send
		// them again.
		s.noteOwnerRequest(requestID)
		acked := s.noteOwnerDeliveredFromReceipt(requestID)
		s.announceOwnerDelivered(batch[0], acked)
		logNotifyReceipt(requestID, true, len(batch), acked)
	}
	var sent *sentNotifyBatch
	if !ownerBatch && requestID != "" && s.receiptQueryConfigured() {
		sent = s.settleFleetBatchReceipt(requestID, batch)
	}
	// 🎯T555.1: the batch owns the session now — accepted + correspondent
	// before the first ACP update, so the bar does not wait for it.
	s.beginOverseerPhase(correspondentForBatch(batch, ownerBatch))
	slog.Info("notify_queue",
		"component", "notify_queue",
		"decision", "drain",
		"depth", depth,
		"drained", len(batch),
		"owner_batch", ownerBatch,
	)
	// Fleet batch just took the session but an owner is waiting — free it.
	// A seat that settles on interrupt is free at once, so the owner goes
	// now rather than on a terminal stop that seat will never emit.
	if !ownerBatch && ownerPending && s.interruptOverseerForOwner() {
		s.drainOverseerNotes()
	}
	return sent
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

// RewindOverseer refuses the retired CLI rewind operation before any mutation.
// Sidecar Stop detaches a connection; it does not unload or reset its session.
// A rotate-and-recap replacement cannot safely reuse that still-loaded seat.
func (s *Server) RewindOverseer(n int) error {
	if n < 1 {
		return fmt.Errorf("rewind: turns must be >= 1")
	}
	return fmt.Errorf("rewind is unavailable: legacy CLI rewind was removed; your conversation has not been changed")
}

// SetRegistry attaches the agent registry for the /api/agents endpoint.
func (s *Server) SetRegistry(reg *claudia.Registry) {
	s.mu.Lock()
	s.registry = reg
	s.mu.Unlock()
	a := s.seats.Load()
	if a == nil {
		a = seatstate.RegistryAuthority(reg)
		s.SetSeats(a)
	}
	a.ObserveRegistry(reg)
	s.projectAgents()
}

// NotifyAgentsChanged pushes a non-journaled live frame so the RHS fleet
// panel refreshes without waiting on poll (🎯T82). Safe from any goroutine.
func (s *Server) NotifyAgentsChanged() {
	s.projectAgents()
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
	Ledger string `json:"ledger,omitempty"`
	Status string `json:"status"`
	// Running is process Alive() — not registry presence or last-known Launch (🎯T545.5).
	Running  bool   `json:"running"`
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
	// StopReason / StoppedAt: why a not-running seat last stopped, from the
	// daemon's seat-stop ledger (🎯T662). MassStop is the fleet-wide alert
	// when three or more seats stopped within a minute with no daemon
	// restart; carried on every row so the RHS can show it once.
	StopReason string `json:"stop_reason,omitempty"`
	StoppedAt  string `json:"stopped_at,omitempty"`
	// Starting marks a not-running seat that something is bringing up: a
	// jevons_agent_start that has not returned, or a launch in flight
	// (🎯T970). It carries no stop reason: it has not stopped.
	Starting bool `json:"starting,omitempty"`
	// PlanWall is set when the seat's transcript ends on Cursor's
	// "Upgrade your plan to continue" turn. The process may still be
	// running; the row shows it the way it shows a stop reason.
	PlanWall string `json:"plan_wall,omitempty"`
	// Rehydrate says whether a not-running seat can come back (🎯T763):
	// "resumable", "repairable: …" (its def carries settings its provider
	// refuses, dropped on the next rehydrate) or "broken: …" (its last
	// rehydrate failed, with the error). `status: stopped` alone cannot
	// tell a revivable seat from one every send will refuse. Empty while
	// running.
	Rehydrate       string `json:"rehydrate,omitempty"`
	ReauthAvailable bool   `json:"reauth_available,omitempty"`
	// DroppedCaps is the last provider-native setting (a codex sandbox)
	// dropped from this seat's def because its provider refuses it, with
	// when and what (🎯T763). A drop is never silent; empty means none
	// since this daemon started.
	DroppedCaps string `json:"dropped_caps,omitempty"`
	MassStop    string `json:"mass_stop,omitempty"`
	// Wedged is set while a running seat's turn is believed in flight but
	// nothing has been heard from it and messages wait behind it (🎯T927).
	// phase stays "working"; this says the work is not happening.
	Wedged string `json:"wedged,omitempty"`
	// SpawnOrders: one line per open spawn order given to this seat
	// (🎯T762). An incomplete order names every seat it listed that was never
	// minted and why (refused: the start error; not_attempted: no start was
	// ever made), so a cold reader can tell a half-done order from a done one.
	SpawnOrders []string `json:"spawn_orders,omitempty"`
	// SpawnOrdersError is set when the order store cannot be read: malformed
	// state is a visible error on the row, never an empty spawn_orders.
	SpawnOrdersError string `json:"spawn_orders_error,omitempty"`
	// TranscriptActivity / TranscriptLastMove / TranscriptAgeSeconds answer
	// "has this running seat moved recently?" from this one response, with no
	// filesystem reach into ~/.local/state/claudia or ~/.claude/projects
	// (🎯T702). The reading is a stat of the same transcript the daemon
	// already watches for born-stuck detection (🎯T679.1 / 🎯T694), so Grok,
	// Claude and Cursor seats report the same field with the same meaning.
	//
	// TranscriptAgeSeconds is deliberately a pointer without omitempty: a seat
	// whose transcript cannot be located serves null, never 0 and never the
	// clock — an unreadable meter is unknown, not a value (🎯T677). Readers
	// branch on TranscriptActivity ("known" | "unknown") and never on a zero.
	TranscriptActivity       string   `json:"transcript_activity,omitempty"`
	TranscriptLastMove       string   `json:"transcript_last_move,omitempty"`
	TranscriptAgeSeconds     *float64 `json:"transcript_age_seconds"`
	TranscriptActivityReason string   `json:"transcript_activity_reason,omitempty"`
}

// feedRelaunch is one dead seat's relaunch in progress, per registry
// (🎯T936): a seat is relaunched once however many feed requests see it dead.
type feedRelaunch struct {
	reg  *claudia.Registry
	name string
}

var feedRelaunching sync.Map // feedRelaunch -> struct{}

// feedLaunch is the relaunch itself; tests replace it.
var feedLaunch = func(reg *claudia.Registry, name string) (*claudia.Agent, error) { return reg.Launch(name) }

// relaunchDeadSeat relaunches a dead AutoStart seat in the background and
// reports the outcome as the inline relaunch did. A seat whose lifecycle
// another path holds (the health sweep relaunching it too) is left to that
// path: stopping it here would stop the other path's fresh process.
func relaunchDeadSeat(reg *claudia.Registry, name, cause string, onRecovered func([]string), onDead func(name, cause, detail string)) {
	key := feedRelaunch{reg, name}
	if _, busy := feedRelaunching.LoadOrStore(key, struct{}{}); busy {
		return
	}
	go func() {
		defer feedRelaunching.Delete(key)
		if _, err := feedLaunch(reg, name); err != nil {
			if errors.Is(err, claudia.ErrLifecycleInProgress) {
				return
			}
			reg.Stop(name)
			seatstate.ObserveStopped(reg, name)
			if onDead != nil {
				onDead(name, cause, "found not alive by the fleet feed; re-launch failed: "+err.Error())
			}
			return
		}
		if onDead != nil {
			onDead(name, cause, "found not alive by the fleet feed; re-launched (AutoStart)")
		}
		if onRecovered != nil {
			onRecovered([]string{name})
		}
	}()
}

// SetDeadSeatNoter installs the 🎯T925 record the fleet feed makes of each
// dead seat it handles (mcpserver.NoteDeadSeat in production).
func (s *Server) SetDeadSeatNoter(fn func(name, cause, detail string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deadSeatNoter = fn
}

func (s *Server) noteDeadSeat(name, cause, detail string) {
	s.mu.RLock()
	fn := s.deadSeatNoter
	s.mu.RUnlock()
	if fn != nil {
		fn(name, cause, detail)
	}
}

// SetSeatStartingReader installs the 🎯T970 lookup for seats being brought up
// (mcpserver.AgentStarting in production).
func (s *Server) SetSeatStartingReader(fn func(name string) bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seatStartingReader = fn
}

// SetSeatStopReader installs the 🎯T662 seat-stop lookup the /api/agents
// rows are decorated with (mcpserver.SeatStopReason in production).
func (s *Server) SetSeatStopReader(fn func(name string) (reason string, at time.Time, ok bool)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seatStopReader = fn
}

// SetMassStopReader installs the 🎯T662 mass-stop line source
// (mcpserver.MassStopLine in production).
func (s *Server) SetMassStopReader(fn func() string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.massStopReader = fn
}

// SetSpawnOrderReader installs the 🎯T762 open-order lines per parent
// (mcpserver.SpawnOrderLines in production).
func (s *Server) SetSpawnOrderReader(fn func(parent string) ([]string, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.spawnOrderReader = fn
}

// SetWedgedReader installs the 🎯T927 wedged-turn lookup
// (mcpserver.WedgedSeat in production).
func (s *Server) SetWedgedReader(fn func(name string) (string, bool)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.wedgedReader = fn
}

// decorateSeatStops applies the 🎯T662 fields to the rows.
func (s *Server) decorateSeatStops(agents []agentInfo) []agentInfo {
	s.mu.RLock()
	stopReader := s.seatStopReader
	startingReader := s.seatStartingReader
	massReader := s.massStopReader
	wedgedReader := s.wedgedReader
	orderReader := s.spawnOrderReader
	s.mu.RUnlock()
	mass := ""
	if massReader != nil {
		mass = massReader()
	}
	for i := range agents {
		if startingReader != nil && !agents[i].Running && startingReader(agents[i].Name) {
			// 🎯T970: a row registered minutes before its process exists
			// has not exited; saying so is what the panel showed on
			// 2026-09-30 for four workers queued behind other launches.
			agents[i].Starting = true
		} else if stopReader != nil && !agents[i].Running {
			if reason, at, ok := stopReader(agents[i].Name); ok {
				agents[i].StopReason = reason
				agents[i].StoppedAt = at.UTC().Format(time.RFC3339)
			} else {
				// A restart drops the in-memory ledger, and a seat that was
				// already stopped at boot is never swept. Silence on that
				// row is the 2026-09-15 failure. Unknown is the recorded
				// answer when this process has no reason.
				agents[i].StopReason = seatstop.Unknown
			}
		}
		agents[i].MassStop = mass
		if wedgedReader != nil && agents[i].Running {
			if line, ok := wedgedReader(agents[i].Name); ok {
				agents[i].Wedged = line
			}
		}
		if orderReader != nil {
			lines, err := orderReader(agents[i].Name)
			agents[i].SpawnOrders = lines
			if err != nil {
				agents[i].SpawnOrdersError = err.Error()
			}
		}
	}
	return agents
}

// SetTranscriptRoots attaches the provider transcript stores the 🎯T702 seat
// activity meter reads. Without it, Grok seats report unknown rather than
// having the daemon guess at the live home (🎯T679.1).
func (s *Server) SetTranscriptRoots(roots discovery.Roots) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.transcriptRoots = roots
}

// decorateSeatActivity fills the 🎯T702 recency fields from the registry rows
// the feed was built from. One stat per seat, never a decode: these tapes are
// multi-megabyte and grow all night, and re-opening them per request is what
// starved the loops that watch for stalls in 🎯T705.
func (s *Server) decorateSeatActivity(reg *claudia.Registry, agents []agentInfo, now time.Time) []agentInfo {
	if reg == nil {
		return agents
	}
	for i := range agents {
		d := reg.Def(agents[i].Name)
		st := s.seatState(agents[i].Name)
		if d == nil || st.LastActivity.IsZero() || (st.SessionID != "" && st.SessionID != d.SessionID) {
			agents[i].TranscriptActivity = string(seatactivity.VerdictUnknown)
			agents[i].TranscriptActivityReason = "no activity observed for this session"
			continue
		}
		agents[i].TranscriptActivity = string(seatactivity.VerdictKnown)
		age := max(0, now.Sub(st.LastActivity).Seconds())
		agents[i].TranscriptAgeSeconds = &age
		agents[i].TranscriptLastMove = st.LastActivity.UTC().Format(time.RFC3339)
	}

	return agents
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
	return listFleetAgentsNotifying(reg, nil, nil, nil, nil)
}

// listFleetAgentsNotifying is the same feed with an optional notify hook for
// recovery events (server wires agents_changed). Used by hermetic tests with
// notify=nil. progress may be nil (no ACP snapshots); models may be nil (no
// session-log lookup — rows then carry only what the live hub saw).
func listFleetAgentsNotifying(reg *claudia.Registry, account *fleetlog.Account, onRecovered func(names []string), progress *AgentProgressHub, models *fleetModelResolver) []agentInfo {
	return listFleetAgentsNoting(reg, account, onRecovered, nil, progress, models)
}

// listFleetAgentsNoting is listFleetAgentsNotifying that also reports each
// dead seat it handles to onDead, with why its handle died when the harness
// knew (🎯T925). This path stops dead seats as the sweep does, and a stop it
// made with no record behind it painted "unknown" on a seat the broker took
// down.
func listFleetAgentsNoting(reg *claudia.Registry, account *fleetlog.Account, onRecovered func(names []string), onDead func(name, cause, detail string), progress *AgentProgressHub, models *fleetModelResolver) []agentInfo {
	if reg == nil {
		return []agentInfo{}
	}
	var recovered []string
	// Inline silent-death policy (same as mcpserver.deadRecoveryPlan) so the
	// HTTP feed path does not import mcpserver (cycle). Keep in sync.
	for _, d := range reg.List() {
		proc := reg.Get(d.Name)
		if proc == nil || seatstate.ReadRegistry(reg, d.Name).Alive != seatstate.No {
			continue
		}
		if reg.ResumeDenied(d.Name) != nil {
			continue
		}
		cause := fleet.ExitCause(proc)
		if d.AutoStart {
			// Off the request path (🎯T936): a relaunch waits on the broker,
			// and during a broker restart every seat is dead at once, so the
			// fleet list went unanswered for minutes exactly while the fleet
			// was recovering. The row answers as it is now; the owner hears
			// when the seat is back.
			relaunchDeadSeat(reg, d.Name, cause, onRecovered, onDead)
		} else if fleet.DeadSeatRemovable(d.Purpose) {
			// 🎯T544: a dead work seat leaves the tree instead of lingering as
			// a "stopped" row. Stop first so the dead handle is not left
			// dangling should the accounted removal refuse.
			reg.Stop(d.Name)
			seatstate.ObserveStopped(reg, d.Name)
			if _, err := account.Remove(reg, d.Name, fleetlog.Removal{
				Reason: fleetlog.ReasonDeadSeat,
				Detail: "work seat's process exited without a terminal report (🎯T544)",
			}); err != nil {
				slog.Warn("fleet feed: dead work seat removal failed; handle cleared",
					"name", d.Name, "err", err)
			}
			recovered = append(recovered, d.Name) // row gone → surface the diff
		} else {
			reg.Stop(d.Name)
			seatstate.ObserveStopped(reg, d.Name)
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
		st := seatstate.ReadRegistry(reg, d.Name)
		status := st.Status
		running := st.Alive == seatstate.Yes
		if st.Alive == seatstate.No {
			status = "stopped"
		}
		if status == "" {
			status = "phase_unknown"
			if running {
				status = "running"
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
			Running:     running,
			Provider:    strings.TrimSpace(string(d.Provider)),
		}
		if !running {
			info.Rehydrate = fleet.RehydrateHealth(d)
			info.ReauthAvailable = reauthablePlanFailure(d.Provider, d)
		}
		info.DroppedCaps = fleet.CapDrop(d.Name)
		// 🎯T365: target filings and idea/capture asides share purpose=aside;
		// the create-time meta beside the workdir is what tells them apart.
		if purpose == claudia.PurposeAside {
			info.AsideKind = asideKindFromWorkDir(d.WorkDir)
		}
		if progress != nil {
			p := progress.Get(d.Name)
			if p.Summary != "" || p.Phase != "" || p.Step != "" {
				info.Phase = st.Phase.String()
				// A blocked goal is task content, independent of seat turn state.
				if p.Phase == "blocked" {
					info.Phase = "blocked"
				}
				info.Step = p.Step
				info.Progress = p.Summary
			}
		}
		info.Model = st.Model

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
	changed := s.agentProgress.Observe(name, ev)
	if a := s.seats.Load(); a != nil {
		a.FromTurnEvent(name, ev.IsTerminalStop(), time.Now())
		if s.registry != nil {
			if d := s.registry.Def(name); d != nil {
				observeSeatModel(a, *d, s.agentProgress, nil)
			}
		}
	}
	if changed {
		s.muxFanSeatPhase(name)
	}
	return changed
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
	// Where a slow answer spent its time (🎯T936), logged past agentsSlow.
	start := time.Now()
	var phases []string
	mark := func(name string) {
		phases = append(phases, fmt.Sprintf("%s=%s", name, time.Since(start).Round(time.Millisecond)))
	}
	agents := listFleetAgentsNoting(reg, s.RemovalAccount(), func(names []string) {
		// 🎯T85: push UI refresh + optional client-visible signal after recovery.
		s.NotifyAgentsChanged()
	}, s.noteDeadSeat, s.agentProgress, models)
	mark("list")
	rows := s.decorateSeatStops(agents)
	mark("stops")
	rows = s.decoratePlanWalls(rows)
	mark("walls")
	rows = s.decoratePlanAuth(reg, rows)
	mark("auth")
	rows = s.decorateSeatActivity(reg, rows, time.Now())
	mark("activity")
	if took := time.Since(start); took > agentsSlow {
		slog.Warn("🎯T936 /api/agents slow", "took", took.Round(time.Millisecond), "phases", strings.Join(phases, " "), "rows", len(rows))
	}
	_ = json.NewEncoder(w).Encode(rows)
}

// agentsSlow is when /api/agents logs where its time went (🎯T936).
const agentsSlow = 2 * time.Second

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
		s.settleOwnerCancel() // 🎯T915
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
		// Refuse the withdrawn operation visibly, without telling clients
		// to trim history or treating the control frame as an owner turn.
		if err := s.RewindOverseer(ctl.Turns); err != nil {
			payload, _ := json.Marshal(map[string]string{"type": "error", "error": err.Error()})
			writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			if err := conn.Write(writeCtx, websocket.MessageText, payload); err != nil {
				slog.Warn("chat: rewind refusal delivery failed", "err", err)
			}
		}
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

	if proc == nil || !(s.seatState(s.overseerSeatName()).Alive == seatstate.Yes) {
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
	s.overseerOwnerTurnText = ""
	s.clearOwnerTurnPlacementLocked()
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
	s.setOverseerPhase(PhaseSample{Phase: PhaseIdle}) // 🎯T555.1 after the settle frame
	s.Broadcast(frame)
	// Notes deferred while the turn held the session can go out now —
	// unless the owner's cancel holds them for the owner's next send (🎯T915).
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
func (s *Server) persistChatLine(line string) bool {
	return s.persistChatLineWithPolicy(line, false)
}

func (s *Server) persistChatLineWithPolicy(line string, requireDurable bool) bool {
	if s == nil || strings.TrimSpace(line) == "" {
		return false
	}
	// 🎯T555.5 / T355: type=status is recovery/level chrome, not a turn.
	// Journaling it replays “overseer is back” as assistant prose.
	if isEphemeralChatStatusLine(line) {
		return false
	}
	name := s.overseerAgentName()
	// 🎯T548.2: a configured SQLite store is canonical even when empty
	// or temporarily unreadable. JSONL is import-once history.
	if s.stateStore() != nil {
		if s.mux == nil {
			s.mux = newMuxHub()
		}
		durable := s.muxFanTranscript(name, line)
		// A failed SQLite read also looks like N=0 to legacy callers. Do
		// not switch stores: that JSONL append would mark SendJournaled
		// true for a turn canonical replay will never recover.
		// 🎯T593: SQLite is the durable store now, so it is what
		// send-landed must be observed against. Before this, durability
		// was only ever recorded on the JSONL append — a path 🎯T548.2
		// stopped taking — so sendJournaled could never become true,
		// every owner turn aged into the not_durable_in_chatlog gap, and
		// owner-health re-injected it. The owner saw one question reach
		// the overseer again and again while the cockpit sat at idle.
		if durable {
			s.noteChatJournaled(line)
		}
		// 🎯T592: the turn-gap alarm re-arms only on what the store
		// actually recorded — a turn whose append failed is the defect,
		// never a reset.
		s.observeChatTurnGap(line, durable)
		return durable
	}
	durable := s.persistChatJSONL(line)
	s.observeChatTurnGap(line, durable)
	if durable || !requireDurable {
		s.muxFanTranscript(name, line)
	}
	return durable
}

// isEphemeralChatStatusLine reports type=status recovery/level chrome
// (🎯T555.5). Those frames are live-only; they are not turns.
func isEphemeralChatStatusLine(line string) bool {
	var m struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(line)), &m); err != nil {
		return false
	}
	return m.Type == "status"
}

// persistChatJSONL appends one line to the JSONL chat log and reports
// whether the append actually landed (🎯T592: durability is the append's
// result, not the attempt).
func (s *Server) persistChatJSONL(line string) bool {
	if s == nil || strings.TrimSpace(line) == "" {
		return false
	}
	if isEphemeralChatStatusLine(line) {
		return false
	}
	s.mu.Lock()
	clog := s.chatLog
	s.mu.Unlock()
	if clog == nil {
		return false
	}
	if err := clog.Append(line); err != nil {
		slog.Error("chat: DURABILITY FAILURE — chat log append failed", "err", err)
		return false
	}
	s.noteChatJournaled(line)
	return true
}

func (s *Server) BroadcastChat(line string) bool {
	durable := s.persistChatLine(line)
	s.broadcastChatLive(stampConversationName(line, s.overseerAgentName()))
	return durable
}

// Only admission-controlled assistant output requires a durable journal write
// before WS publication. Unrelated owner echoes, send_error and notices keep
// their prior broadcast semantics, including on a journal failure.
func (s *Server) broadcastAdmittedChat(line string) bool {
	if !s.overseerAdmissionEnabled() {
		return s.BroadcastChat(line)
	}
	durable := s.persistChatLineWithPolicy(line, true)
	if s.admissionJournalHook != nil {
		s.admissionJournalHook(line, durable)
	}
	if durable || isEphemeralChatStatusLine(line) {
		s.broadcastChatLive(stampConversationName(line, s.overseerAgentName()))
	} else {
		s.admissionDegraded("journal write failed", "")
	}
	return durable
}

// Text can escape to the remote voice/websocket path through HandleAgentEvent,
// independent of /ws/chat and /ws/mux. In the admission path only a durable
// admitted publication may forward authored text; terminal settlement remains
// body-less on journal failure. Legacy nil-admission behavior is untouched.
func (s *Server) admissionSafeEvent(ev claudia.Event, durable bool) claudia.Event {
	if s.overseerAdmissionEnabled() && !durable {
		// Text and Raw may carry model-authored preview/tool fragments even
		// when Type is progress. Preserve Type/StopReason/ProgressType for
		// phase settlement, but never forward unjournaled candidate bytes.
		ev.Text = ""
		ev.Raw = nil
		ev.ToolTitle = ""
		ev.ToolCallID = ""
	}
	return ev
}

func (s *Server) handleAdmissionSafeAgentEvent(ev claudia.Event, durable bool) {
	s.HandleAgentEvent(s.admissionSafeEvent(ev, durable))
}

func (s *Server) overseerAdmissionEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ownerAdmission != nil
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
}

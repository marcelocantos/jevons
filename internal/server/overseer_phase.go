// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/marcelocantos/claudia"
)

// Overseer turn-state phases (🎯T555.1). A closed enum derived from signals
// Claudia / jevonsd actually publish — never from "is a bubble open". Design:
// docs/design/overseer-turn-state.md.
const (
	PhaseIdle       = "idle"
	PhaseAccepted   = "accepted"
	PhaseThinking   = "thinking"
	PhaseTool       = "tool"
	PhaseStreaming  = "streaming"
	PhasePermission = "permission"
	PhaseError      = "error"
	PhaseStuck      = "stuck"
)

// Claudia ProgressType values the mapper understands (claudia 🎯T50).
const (
	progressTypeToolUse        = "tool_use"
	progressTypeThought        = "thought"
	progressTypePlan           = "plan"
	progressTypePromptAccepted = "prompt_accepted"
	progressTypePermission     = "permission"
	// 🎯T899 / claudia 🎯T138: a message queued or steered behind the turn
	// was taken, or its escalation fired. Neither is a phase of the turn.
	progressTypeDeliveryAbsorbed  = "delivery_absorbed"
	progressTypeDeliveryEscalated = "delivery_escalated"
)

// CorrespondentFleet names a fleet note that carries no agent name.
const CorrespondentFleet = "fleet"

// PhaseSample is one reduce of the overseer turn-state stream: the latest
// phase-bearing event wins. Correspondent is the in-flight notify batch,
// stamped by jevons (ACP does not know jevons-po); empty for the owner.
type PhaseSample struct {
	Phase         string   `json:"phase"`
	Step          string   `json:"step,omitempty"`
	Tokens        int      `json:"tokens,omitempty"`
	Correspondent []string `json:"correspondent,omitempty"`
}

// Working is the derived session-busy boolean (vanilla / 🎯T355 compat):
// every phase except idle and error, with stuck staying true so
// chrome_false_idle still fires.
func (p PhaseSample) Working() bool {
	return p.Phase != "" && p.Phase != PhaseIdle && p.Phase != PhaseError
}

// phaseFromEvent is the one mapper shared by the chat stream and
// AgentProgressHub. ok=false means the event carries no phase signal
// (the reduce keeps its previous sample).
func phaseFromEvent(ev claudia.Event) (PhaseSample, bool) {
	tokens := ev.Usage.OutputTokens
	switch {
	case ev.Type == "progress" && ev.ProgressType != "":
		switch ev.ProgressType {
		case progressTypeThought:
			return PhaseSample{Phase: PhaseThinking, Tokens: tokens}, true
		case progressTypePlan:
			// Plan refines thinking; chrome may ignore the body.
			return PhaseSample{Phase: PhaseThinking, Tokens: tokens}, true
		case progressTypePromptAccepted:
			return PhaseSample{Phase: PhaseAccepted, Tokens: tokens}, true
		case progressTypePermission:
			return PhaseSample{Phase: PhasePermission, Tokens: tokens}, true
		case progressTypeDeliveryAbsorbed, progressTypeDeliveryEscalated:
			return PhaseSample{}, false
		case claudia.ProgressTUIPreview:
			// 🎯T919: a Claude TUI pane preview is provisional assistant
			// text scraped off the pane, not a tool call.
			return PhaseSample{Phase: PhaseStreaming, Tokens: tokens}, true
		case claudia.ProgressTUIPreviewFault, claudia.ProgressPromptSuperseded:
			// A scrape-invariant report and a steered-over prompt's result
			// are bookkeeping; the turn is whatever it already was.
			return PhaseSample{}, false
		}
		if toolStatusTerminal(ev.ToolStatus) || (ev.ToolStatus == "" && toolCallTerminal(ev.Raw)) {
			// A finished tool is not a new phase: the stream stays wherever
			// the next chunk puts it. Keep the current sample.
			return PhaseSample{}, false
		}
		s := PhaseSample{Phase: PhaseTool, Tokens: tokens}
		// Prefer Claudia T50.3 ToolTitle; fall back to Raw only when empty.
		// "MCP: tool" stays bare tool — never invent a name (🎯T64.2).
		title := strings.TrimSpace(ev.ToolTitle)
		if title != "" && !genericToolTitle(title) {
			s.Step = oneLineProgress(title, 40)
			return s, true
		}
		if title == "" {
			call := parseToolCall(ev.Raw)
			if name := call.DisplayName(); name != "" && !genericToolTitle(name) {
				s.Step = oneLineProgress(name, 40)
			}
		}
		return s, true

	case ev.Type == "assistant" && ev.IsError:
		return PhaseSample{Phase: PhaseError}, true

	case ev.IsTerminalStop():
		return PhaseSample{Phase: PhaseIdle}, true

	case ev.Type == "assistant":
		return PhaseSample{Phase: PhaseStreaming, Tokens: tokens}, true

	case ev.Type == "user":
		// The provider echoing the prompt back: in flight, nothing yet.
		return PhaseSample{Phase: PhaseAccepted}, true

	default:
		return PhaseSample{}, false
	}
}

func toolStatusTerminal(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "completed", "failed", "cancelled", "canceled":
		return true
	}
	return false
}

// toolCallTerminal reports whether a tool_call_update carries a finished
// status (completed / failed / cancelled).
func toolCallTerminal(raw []byte) bool {
	if len(raw) == 0 {
		return false
	}
	var u struct {
		Update struct {
			Status string `json:"status"`
		} `json:"update"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(raw, &u); err != nil {
		return false
	}
	st := u.Update.Status
	if st == "" {
		st = u.Status
	}
	return toolStatusTerminal(st)
}

// hubPhase projects the closed enum onto AgentProgressHub's coarser
// working | idle vocabulary (RHS fleet rows).
func hubPhase(phase string) string {
	switch phase {
	case PhaseIdle, PhaseError, "":
		return "idle"
	}
	return "working"
}

// correspondentForBatch stamps who a drained notify batch is for: nothing
// for the owner; [Agent <name> responded] names in drain order (deduped);
// CorrespondentFleet for any nameless note.
func correspondentForBatch(batch []string, ownerBatch bool) []string {
	if ownerBatch {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for _, note := range batch {
		name, ok := notifyAgentRespondedName(note)
		if !ok || name == "" {
			name = CorrespondentFleet
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

// phaseWireLine is the interleaved progress frame on /ws/chat. It carries no
// message body, so neither client paints it as a bubble. Frames are live
// only — never journaled: the reload snapshot is history_meta.phase (🎯T272),
// and a log full of chrome ticks would be a second source of truth.
func phaseWireLine(p PhaseSample) string {
	frame := map[string]any{
		"type":      "progress",
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		"phase":     p.Phase,
		"working":   p.Working(),
	}
	if p.Step != "" {
		frame["step"] = p.Step
	}
	if p.Tokens > 0 {
		frame["tokens"] = p.Tokens
	}
	if len(p.Correspondent) > 0 {
		frame["correspondent"] = p.Correspondent
	}
	b, err := json.Marshal(frame)
	if err != nil {
		return ""
	}
	return string(b)
}

// OverseerPhase returns the current reduce (history_meta snapshot for hard
// reload, 🎯T272). It is the tail of the stream, not a second source of truth.
func (s *Server) OverseerPhase() PhaseSample {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p := s.overseerPhase
	if p.Phase == "" {
		p.Phase = PhaseIdle
	}
	return p
}

// setOverseerPhase applies one phase-bearing sample to the reduce and
// interleaves the frame onto the chat stream. Non-idle samples inherit the
// in-flight correspondent; idle clears it. Unchanged samples are not
// re-broadcast.
func (s *Server) setOverseerPhase(next PhaseSample) {
	s.reduceOverseerPhase(next, false)
}

// applyOverseerEventPhase folds one overseer event into the phase reduce.
//
// 🎯T919: a turn that has ended stays ended until the next one begins.
// Claudia publishes from more than one goroutine — the Claude TUI pane
// poller runs on its own 100ms clock beside the JSONL tail, and an ACP
// tool_call_update can trail the prompt result — so a progress event can
// land behind the terminal stop. Mapped as mid-turn work it moved the
// reduce off idle, and nothing that follows a finished turn ever sets
// idle again: the mux level said "tool" until shutdown. A progress event
// that is not itself a turn opener therefore cannot move the reduce off
// idle; the drain (beginOverseerPhase), the provider's prompt echo,
// prompt_accepted, a permission request, and assistant output still do.
func (s *Server) applyOverseerEventPhase(ev claudia.Event) {
	p, ok := phaseFromEvent(ev)
	if !ok {
		return
	}
	s.reduceOverseerPhase(p, ev.Type == "progress" && !progressOpensTurn(ev))
}

// progressOpensTurn reports whether a progress event can start a turn from
// rest: the provider taking a prompt, or a turn blocked on the owner.
func progressOpensTurn(ev claudia.Event) bool {
	switch ev.ProgressType {
	case progressTypePromptAccepted, progressTypePermission:
		return true
	}
	return false
}

// reduceOverseerPhase is setOverseerPhase with an optional rest guard:
// midTurnOnly drops the sample when the reduce is at rest (idle). The
// guard and the write share one lock hold, so a trailing event racing the
// terminal stop cannot pass the check and then overwrite the idle it saw.
func (s *Server) reduceOverseerPhase(next PhaseSample, midTurnOnly bool) {
	s.mu.Lock()
	if midTurnOnly && (s.overseerPhase.Phase == "" || s.overseerPhase.Phase == PhaseIdle) {
		s.mu.Unlock()
		return
	}
	if next.Phase == PhaseIdle {
		next.Correspondent = nil
		s.overseerCorrespondent = nil
	} else {
		next.Correspondent = s.overseerCorrespondent
	}
	if next.Step == "" && next.Phase == s.overseerPhase.Phase {
		next.Step = s.overseerPhase.Step
	}
	prev := s.overseerPhase
	s.overseerPhase = next
	s.mu.Unlock()
	if samePhaseSample(prev, next) {
		return
	}
	if line := phaseWireLine(next); line != "" {
		s.broadcastChatLive(stampConversationName(line, s.overseerAgentName()))
	}
	s.muxFanOverseerLevel() // React /ws/mux reduces the same sample (🎯T555.2)
}

// beginOverseerPhase stamps a freshly drained batch: accepted plus its
// correspondent, before the first ACP update arrives.
func (s *Server) beginOverseerPhase(correspondent []string) {
	s.mu.Lock()
	s.overseerCorrespondent = correspondent
	s.overseerPhase = PhaseSample{} // force a broadcast even if already accepted
	s.mu.Unlock()
	s.setOverseerPhase(PhaseSample{Phase: PhaseAccepted})
}

// settleOverseerAfterUnstick clears the in-flight turn and returns the
// strip to idle. markOverseerStuck leaves the phase on stuck, and a
// stuck strip stays busy, so a follow-up the owner already typed sits
// in the queue until someone cuts in.
func (s *Server) settleOverseerAfterUnstick() {
	s.mu.Lock()
	s.waiting = false
	s.overseerOwnerTurn = false // 🎯T291
	s.turnBuf = ""
	s.noteOverseerProgressLocked()
	s.mu.Unlock()
	s.setOverseerPhase(PhaseSample{Phase: PhaseIdle})
}

// markOverseerStuck is the jevons-minted stuck frame: in flight past the
// watchdog with no new ACP progress.
func (s *Server) markOverseerStuck() {
	s.mu.Lock()
	s.overseerOutageOpen = true // 🎯T567: a stuck frame opens an outage
	s.mu.Unlock()
	s.setOverseerPhase(PhaseSample{Phase: PhaseStuck})
}

func samePhaseSample(a, b PhaseSample) bool {
	if a.Phase != b.Phase || a.Step != b.Step || a.Tokens != b.Tokens || len(a.Correspondent) != len(b.Correspondent) {
		return false
	}
	for i := range a.Correspondent {
		if a.Correspondent[i] != b.Correspondent[i] {
			return false
		}
	}
	return true
}

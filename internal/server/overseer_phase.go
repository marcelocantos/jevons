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

// Claudia ProgressType values the mapper understands. tool_use is what
// claudia emits today; thought / plan / prompt_accepted / permission are the
// 🎯T555.3 / claudia 🎯T50 uplift and are unreachable until it lands.
const (
	progressTypeToolUse        = "tool_use"
	progressTypeThought        = "thought"
	progressTypePlan           = "plan"
	progressTypePromptAccepted = "prompt_accepted"
	progressTypePermission     = "permission"
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
	switch {
	case ev.Type == "progress" && ev.ProgressType != "":
		switch ev.ProgressType {
		case progressTypeThought:
			return PhaseSample{Phase: PhaseThinking}, true
		case progressTypePlan:
			// Plan refines thinking; chrome may ignore the body.
			return PhaseSample{Phase: PhaseThinking}, true
		case progressTypePromptAccepted:
			return PhaseSample{Phase: PhaseAccepted}, true
		case progressTypePermission:
			return PhaseSample{Phase: PhasePermission}, true
		}
		call := parseToolCall(ev.Raw)
		if call.SessionUpdate == "tool_call_update" && toolCallTerminal(ev.Raw) {
			// A finished tool is not a new phase: the stream stays wherever
			// the next chunk puts it. Keep the current sample.
			return PhaseSample{}, false
		}
		s := PhaseSample{Phase: PhaseTool}
		// A real title is the phase copy (🎯T71); "MCP: tool" stays bare
		// tool — never invent a name the provider never wrote (🎯T64.2).
		if name := call.DisplayName(); name != "" && !genericToolTitle(name) {
			s.Step = oneLineProgress(name, 40)
		}
		return s, true

	case ev.Type == "assistant" && ev.IsError:
		return PhaseSample{Phase: PhaseError}, true

	case ev.IsTerminalStop():
		return PhaseSample{Phase: PhaseIdle}, true

	case ev.Type == "assistant":
		s := PhaseSample{Phase: PhaseStreaming}
		s.Tokens = ev.Usage.OutputTokens
		return s, true

	case ev.Type == "user":
		// The provider echoing the prompt back: in flight, nothing yet.
		return PhaseSample{Phase: PhaseAccepted}, true

	default:
		return PhaseSample{}, false
	}
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
	st := strings.ToLower(u.Update.Status)
	if st == "" {
		st = strings.ToLower(u.Status)
	}
	switch st {
	case "completed", "failed", "cancelled", "canceled":
		return true
	}
	return false
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
	s.mu.Lock()
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

// markOverseerStuck is the jevons-minted stuck frame: in flight past the
// watchdog with no new ACP progress.
func (s *Server) markOverseerStuck() {
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

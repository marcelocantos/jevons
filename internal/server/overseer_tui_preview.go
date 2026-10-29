// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/marcelocantos/claudia"
)

// overseerTUIPreviewState tracks open Claude TUI preview blocks for the
// overseer wire (🎯T584). Live-only — never journaled or coalesced.
type overseerTUIPreviewState struct {
	blocks    map[string]string
	order     []string
	faultSeen map[string]struct{}
}

func (st *overseerTUIPreviewState) reset() {
	st.blocks = nil
	st.order = nil
	st.faultSeen = nil
}

func (st *overseerTUIPreviewState) applyPreview(ev claudia.Event) {
	mid := strings.TrimSpace(ev.MessageID)
	if mid == "" {
		mid = "tui-preview"
	}
	if st.blocks == nil {
		st.blocks = make(map[string]string)
	}
	prev := st.blocks[mid]
	var md string
	switch ev.PreviewUpdate {
	case claudia.PreviewUpdateAppend:
		md = prev + ev.Text
	default:
		md = ev.Text
	}
	if prev == "" && strings.TrimSpace(md) != "" {
		st.order = append(st.order, mid)
	}
	st.blocks[mid] = md
}

func (st *overseerTUIPreviewState) sealOldest() {
	if len(st.order) == 0 {
		return
	}
	mid := st.order[0]
	st.order = st.order[1:]
	if st.blocks != nil {
		delete(st.blocks, mid)
	}
}

func (st *overseerTUIPreviewState) fencedSuffix() string {
	var b strings.Builder
	for _, mid := range st.order {
		md := strings.TrimSpace(st.blocks[mid])
		if md == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("```text\n")
		b.WriteString(md)
		b.WriteString("\n```")
	}
	return b.String()
}

func (st *overseerTUIPreviewState) noteFault(ev claudia.Event) bool {
	key := strings.TrimSpace(ev.MessageID)
	if key == "" {
		key = strings.TrimSpace(ev.Text)
	}
	if key == "" {
		return false
	}
	if st.faultSeen == nil {
		st.faultSeen = make(map[string]struct{})
	}
	if _, seen := st.faultSeen[key]; seen {
		return false
	}
	st.faultSeen[key] = struct{}{}
	return true
}

const (
	wireTypeProvisionalAssistant = "provisional_assistant"
	wireTypeTUIPreviewFault      = "tui_preview_fault"
)

func provisionalAssistantWire(streamID, messageID, text string) string {
	frame := map[string]any{
		"type":       wireTypeProvisionalAssistant,
		"timestamp":  time.Now().UTC().Format(time.RFC3339Nano),
		"stream_id":  streamID,
		"message_id": messageID,
		"text":       text,
	}
	b, err := json.Marshal(frame)
	if err != nil {
		return ""
	}
	return string(b)
}

func tuiPreviewFaultWire(text string) string {
	frame := map[string]any{
		"type":      wireTypeTUIPreviewFault,
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		"text":      text,
	}
	b, err := json.Marshal(frame)
	if err != nil {
		return ""
	}
	return string(b)
}

func isOverseerTUIPreviewEvent(ev claudia.Event) bool {
	if ev.Type != "progress" || ev.ProgressType == "" {
		return false
	}
	return ev.ProgressType == claudia.ProgressTUIPreview ||
		ev.ProgressType == claudia.ProgressTUIPreviewFault
}

// isEphemeralChatWireLine reports chat-wire frames that are live-only on the
// owner stream (never JSONL / statedb / mux coalesce).
func isEphemeralChatWireLine(line string) bool {
	var m struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(line)), &m); err != nil {
		return false
	}
	switch m.Type {
	case "status", wireTypeProvisionalAssistant, wireTypeTUIPreviewFault:
		return true
	default:
		return false
	}
}

func (s *Server) overseerClearTUIPreview() {
	s.mu.Lock()
	had := len(s.overseerTUIPreview.order) > 0
	s.overseerTUIPreview.reset()
	streamID := s.overseerStreamID
	s.mu.Unlock()
	if !had || streamID == "" {
		return
	}
	if line := provisionalAssistantWire(streamID, "", ""); line != "" {
		s.broadcastOverseerProvisionalWire(line)
	}
}

func (s *Server) overseerSealTUIPreviewForAssistant() {
	s.mu.Lock()
	hadOpen := len(s.overseerTUIPreview.order) > 0
	s.overseerTUIPreview.sealOldest()
	streamID := s.overseerStreamID
	suffix := s.overseerTUIPreview.fencedSuffix()
	s.mu.Unlock()
	if !hadOpen || streamID == "" {
		return
	}
	if line := provisionalAssistantWire(streamID, "", suffix); line != "" {
		s.broadcastOverseerProvisionalWire(line)
	}
}

func (s *Server) deliverOverseerTUIPreviewEvent(ev claudia.Event) {
	switch ev.ProgressType {
	case claudia.ProgressTUIPreviewFault:
		s.mu.Lock()
		ok := s.overseerTUIPreview.noteFault(ev)
		s.mu.Unlock()
		if !ok || strings.TrimSpace(ev.Text) == "" {
			return
		}
		if line := tuiPreviewFaultWire(ev.Text); line != "" {
			s.broadcastOverseerProvisionalWire(line)
		}
		return
	case claudia.ProgressTUIPreview:
		s.mu.Lock()
		s.overseerTUIPreview.applyPreview(ev)
		streamID := s.ensureOverseerStreamIDLocked()
		suffix := s.overseerTUIPreview.fencedSuffix()
		mid := strings.TrimSpace(ev.MessageID)
		s.mu.Unlock()
		if line := provisionalAssistantWire(streamID, mid, suffix); line != "" {
			s.broadcastOverseerProvisionalWire(line)
		}
	}
}

func (s *Server) broadcastOverseerProvisionalWire(line string) {
	if strings.TrimSpace(line) == "" {
		return
	}
	line = stampConversationName(line, s.overseerAgentName())
	s.broadcastChatLive(line)
	s.muxFanProvisionalWire(line)
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/marcelocantos/claudia"
)

// AgentProgress is a glanceable per-agent activity snapshot for the RHS
// fleet panel (🎯T118). Derived semi-automatically from ACP progress /
// tool steps and turn lifecycle — not from owner/overseer polling.
type AgentProgress struct {
	Phase   string // working | idle | blocked (empty when unknown)
	Step    string // last tool/step title
	Summary string // preformatted single-line secondary text
	// Model is the last model id an assistant turn reported (🎯T287).
	// Sticky: frames that name no model never forget the previous one, so
	// the RHS company-icon + condensed model prefix survives idle frames.
	// Empty when the provider never names one (Grok ACP).
	Model   string
	Updated time.Time
}

// AgentProgressHub tracks the latest progress line per agent name.
// Safe for concurrent Observe from agent event sinks.
type AgentProgressHub struct {
	mu   sync.Mutex
	by   map[string]AgentProgress
	// now is injectable for tests; nil → time.Now.
	now func() time.Time
}

// NewAgentProgressHub returns an empty hub.
func NewAgentProgressHub() *AgentProgressHub {
	return &AgentProgressHub{by: make(map[string]AgentProgress)}
}

func (h *AgentProgressHub) clock() time.Time {
	if h != nil && h.now != nil {
		return h.now()
	}
	return time.Now()
}

// Get returns the snapshot for name, or zero if unknown.
func (h *AgentProgressHub) Get(name string) AgentProgress {
	if h == nil {
		return AgentProgress{}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.by[name]
}

// Observe updates progress from a claudia agent event.
// Returns true when the glanceable summary changed (callers may push
// agents_changed so the RHS refreshes without poll).
func (h *AgentProgressHub) Observe(name string, ev claudia.Event) bool {
	if h == nil || name == "" {
		return false
	}
	model := modelFromEvent(ev)
	next, ok := progressFromEvent(ev)
	if !ok && model == "" {
		return false
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.by == nil {
		h.by = make(map[string]AgentProgress)
	}
	prev := h.by[name]

	// Model-only frame (no progress signal): learn the model, keep chrome.
	if !ok {
		if prev.Model == model {
			return false
		}
		prev.Model = model
		prev.Updated = h.clock()
		h.by[name] = prev
		return true
	}

	next.Updated = h.clock()
	next.Model = model
	if next.Model == "" {
		next.Model = prev.Model
	}
	// Preserve last step across mid-turn assistant frames that only set phase.
	if next.Step == "" && prev.Step != "" && next.Phase == "working" {
		next.Step = prev.Step
		next.Summary = composeProgressSummary(next.Phase, next.Step)
	}
	if prev.Summary == next.Summary && prev.Phase == next.Phase &&
		prev.Step == next.Step && prev.Model == next.Model {
		return false
	}
	h.by[name] = next
	return true
}

// modelFromEvent extracts the model id an assistant frame reports (🎯T287).
// Claude Code JSONL carries it at message.model; other shapes may put a bare
// top-level "model". Empty when the frame names none — the caller keeps the
// last known model rather than forgetting it.
func modelFromEvent(ev claudia.Event) string {
	if len(ev.Raw) == 0 {
		return ""
	}
	var line struct {
		Model   string `json:"model"`
		Message struct {
			Model string `json:"model"`
		} `json:"message"`
	}
	if err := json.Unmarshal(ev.Raw, &line); err != nil {
		return ""
	}
	if m := strings.TrimSpace(line.Message.Model); m != "" {
		return m
	}
	return strings.TrimSpace(line.Model)
}

// SetStatus seeds a baseline when the process map knows running/stopped
// but no ACP event has arrived yet. Does not clobber a richer snapshot.
func (h *AgentProgressHub) SetStatus(name, status string) {
	if h == nil || name == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.by == nil {
		h.by = make(map[string]AgentProgress)
	}
	prev, ok := h.by[name]
	if ok && prev.Summary != "" && prev.Phase != "" && prev.Phase != "idle" {
		// Keep ACP-derived working/step; only flip to stopped when process dies.
		if status == "stopped" && prev.Phase == "working" {
			prev.Phase = "idle"
			prev.Summary = composeProgressSummary("idle", prev.Step)
			prev.Updated = h.clock()
			h.by[name] = prev
		}
		return
	}
	phase, summary := statusBaseline(status)
	if summary == "" {
		return
	}
	h.by[name] = AgentProgress{
		Phase:   phase,
		Summary: summary,
		Model:   prev.Model, // 🎯T287: liveness baseline never forgets the model
		Updated: h.clock(),
	}
}

// statusBaseline maps process liveness to glanceable chrome (🎯T211).
// Process status=running alone is not busy work: phase stays idle and the
// summary is "idle" (not "running"), so RHS never presents bare running as
// an action line. Busy rows come only from ACP Observe (phase=working + step).
func statusBaseline(status string) (phase, summary string) {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "running":
		return "idle", "idle"
	case "stopped":
		return "idle", "stopped"
	default:
		return "", ""
	}
}

// progressFromEvent maps a claudia event to a progress snapshot.
// ok=false means the event carries no useful progress signal.
func progressFromEvent(ev claudia.Event) (AgentProgress, bool) {
	switch {
	case ev.Type == "progress" && ev.ProgressType != "":
		// Same filter as chat_wire: only initiating tool_call rows.
		title, _ := toolCallDetail(ev.Raw)
		if title == "" {
			return AgentProgress{}, false
		}
		step := oneLineProgress(title, 40)
		return AgentProgress{
			Phase:   "working",
			Step:    step,
			Summary: composeProgressSummary("working", step),
		}, true

	case ev.IsTerminalStop():
		return AgentProgress{
			Phase:   "idle",
			Summary: "idle",
		}, true

	case ev.Type == "assistant":
		// Mid-turn stream / tool_use pause without terminal stop → working.
		return AgentProgress{
			Phase:   "working",
			Summary: "working",
		}, true

	case ev.Type == "user":
		return AgentProgress{
			Phase:   "working",
			Summary: "working",
		}, true

	default:
		return AgentProgress{}, false
	}
}

func composeProgressSummary(phase, step string) string {
	phase = strings.TrimSpace(phase)
	step = strings.TrimSpace(step)
	switch {
	case phase != "" && step != "":
		return oneLineProgress(phase+" · "+step, 48)
	case step != "":
		return oneLineProgress(step, 48)
	case phase != "":
		return oneLineProgress(phase, 48)
	default:
		return ""
	}
}

func oneLineProgress(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if max <= 0 {
		max = 48
	}
	// Use rune count so the ellipsis (multi-byte) does not blow the cap.
	r := []rune(s)
	if len(r) > max {
		return string(r[:max-1]) + "…"
	}
	return s
}

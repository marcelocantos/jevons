// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/fleet"
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
	Model string
	// Session is the registry session id this observation belongs to
	// (🎯T311). Stamped by SyncEpoch; a different session means a different
	// run (kill+respawn, migration), so the sticky model is dropped rather
	// than inherited by whatever now answers to the same name.
	Session string
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

// Forget drops every observation for name (🎯T311). Called when an agent
// leaves the fleet: the sticky model is a fact about the process that just
// died, and a later agent registered under the same name must not inherit it.
func (h *AgentProgressHub) Forget(name string) {
	if h == nil || name == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.by, name)
}

// ClearModel drops only the sticky model for name (🎯T323). Used when the
// learned id belongs to a different company than the agent's live provider
// (Claude-era "fable" under provider=grok after migrate): keep phase/step,
// forget the foreign model so /api/agents never re-serves it.
func (h *AgentProgressHub) ClearModel(name string) {
	if h == nil || name == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	prev, ok := h.by[name]
	if !ok || prev.Model == "" {
		return
	}
	prev.Model = ""
	prev.Updated = h.clock()
	h.by[name] = prev
}

// Prune forgets every agent not in registered — the kill/remove path seen
// from the listing side, so no call site has to remember to notify the hub
// (🎯T311).
func (h *AgentProgressHub) Prune(registered map[string]struct{}) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for name := range h.by {
		if _, ok := registered[name]; !ok {
			delete(h.by, name)
		}
	}
}

// SyncEpoch ties name's snapshot to the session the registry currently has
// on its row (🎯T311). The first sighting stamps it; a later sighting under
// a different session id means the agent was restarted on a fresh
// conversation — possibly under a new model pin — so the stale observation
// is dropped instead of masking the pin.
func (h *AgentProgressHub) SyncEpoch(name, sessionID string) {
	if h == nil || name == "" || sessionID == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	prev, ok := h.by[name]
	if !ok {
		return
	}
	switch prev.Session {
	case sessionID:
	case "":
		prev.Session = sessionID
		h.by[name] = prev
	default:
		h.by[name] = AgentProgress{Session: sessionID, Updated: h.clock()}
	}
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
	next.Session = prev.Session
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
//
// "<synthetic>" frames (API error notices, cancellations — clustered around
// daemon restarts) name no real model and must not poison the sticky
// observation (🎯T348): the log parser has filtered them since 🎯T311, but the
// live wire did not, so a restart window could pin '<synthetic>' into the hub
// and paint a bare mark with a '<synthetic>' tooltip until the next real turn.
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
	m := strings.TrimSpace(line.Message.Model)
	if m == "" {
		m = strings.TrimSpace(line.Model)
	}
	if m == syntheticModel {
		return ""
	}
	return m
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
		Session: prev.Session,
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
	case fleet.StatusDeadUnmaterialized:
		// 🎯T412: the seat's process is up but its claimed conversation does
		// not exist on disk — glanceable chrome says dead, never idle.
		return "idle", "dead (no conversation)"
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
		if goalStatusBlocked(ev) {
			return AgentProgress{Phase: "blocked", Summary: "blocked"}, true
		}
		return AgentProgress{
			Phase:   "idle",
			Summary: "idle",
		}, true

	case ev.Type == "assistant":
		if goalStatusBlocked(ev) {
			return AgentProgress{Phase: "blocked", Summary: "blocked"}, true
		}
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

func goalStatusBlocked(ev claudia.Event) bool {
	if strings.Contains(ev.Text, "GOAL_STATUS: blocked") {
		return true
	}
	if len(ev.Raw) > 0 && strings.Contains(string(ev.Raw), "GOAL_STATUS: blocked") {
		return true
	}
	return false
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

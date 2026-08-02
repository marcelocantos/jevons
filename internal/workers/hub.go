// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package workers

import (
	"encoding/json"
	"sync"
)

// HubEvent is one SSE/dashboard payload for worker lifecycle or output.
type HubEvent struct {
	Type     string `json:"type"` // worker_started | worker_progress | worker_completed | worker_failed | worker_denied
	WorkerID string `json:"worker_id"`
	Status   string `json:"status,omitempty"`
	Task     string `json:"task,omitempty"`
	Line     string `json:"line,omitempty"`
	Outcome  string `json:"outcome,omitempty"`
	Model    string `json:"model,omitempty"`
	// PolicyDecision is included when known (🎯T8.3 surfaces on T8.2 stream).
	PolicyDecision string `json:"policy_decision,omitempty"`
	PolicyLevel    int    `json:"policy_level,omitempty"`
}

// JSON serialises the event for SSE data lines.
func (e HubEvent) JSON() string {
	b, err := json.Marshal(e)
	if err != nil {
		return `{"type":"error","line":"marshal failed"}`
	}
	return string(b)
}

// Hub is a fan-out broadcast of worker lifecycle events (SSE event hub).
type Hub struct {
	mu   sync.Mutex
	subs map[chan HubEvent]struct{}
}

// NewHub creates an empty event hub.
func NewHub() *Hub {
	return &Hub{subs: make(map[chan HubEvent]struct{})}
}

// Subscribe registers a buffered listener. Caller must Unsubscribe.
func (h *Hub) Subscribe() chan HubEvent {
	ch := make(chan HubEvent, 32)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

// Unsubscribe removes a listener and closes its channel.
func (h *Hub) Unsubscribe(ch chan HubEvent) {
	h.mu.Lock()
	if _, ok := h.subs[ch]; ok {
		delete(h.subs, ch)
		close(ch)
	}
	h.mu.Unlock()
}

// Publish sends e to all subscribers (non-blocking; drops if full).
func (h *Hub) Publish(e HubEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- e:
		default:
			// Slow consumer: drop rather than block workers.
		}
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/marcelocantos/jevons/internal/workers"
)

// SetWorkersTracker attaches the 🎯T8.2 observability tracker (SQLite + SSE hub).
func (s *Server) SetWorkersTracker(t *workers.Tracker) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.workers = t
}

// registerWorkerRoutes adds GET /api/workers and SSE /api/workers/events.
func (s *Server) registerWorkerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/workers", s.handleListWorkers)
	mux.HandleFunc("GET /api/workers/{id}/events", s.handleWorkerEvents)
	mux.HandleFunc("GET /api/workers/events", s.handleWorkersSSE)
}

func (s *Server) handleListWorkers(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	tr := s.workers
	s.mu.RUnlock()
	if tr == nil || tr.Store == nil {
		writeJSON(w, []any{})
		return
	}
	status := r.URL.Query().Get("status")
	list, err := tr.Store.List(status, 100)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if list == nil {
		list = []workers.Worker{}
	}
	writeJSON(w, list)
}

func (s *Server) handleWorkerEvents(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	tr := s.workers
	s.mu.RUnlock()
	if tr == nil || tr.Store == nil {
		writeJSON(w, []any{})
		return
	}
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing worker id", http.StatusBadRequest)
		return
	}
	evs, err := tr.Store.Events(id, 0, 500)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if evs == nil {
		evs = []workers.Event{}
	}
	writeJSON(w, evs)
}

// handleWorkersSSE streams worker lifecycle events as text/event-stream.
func (s *Server) handleWorkersSSE(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	tr := s.workers
	s.mu.RUnlock()
	if tr == nil || tr.Hub == nil {
		http.Error(w, "workers hub unavailable", http.StatusServiceUnavailable)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "event: ready\ndata: {}\n\n")
	flusher.Flush()

	ch := tr.Hub.Subscribe()
	defer tr.Hub.Unsubscribe(ch)

	// Keepalive ticker so proxies do not idle-close the stream.
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-tick.C:
			fmt.Fprintf(w, ": keepalive\n\n")
			flusher.Flush()
		case ev, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, ev.JSON())
			flusher.Flush()
		}
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Debug("writeJSON", "err", err)
	}
}

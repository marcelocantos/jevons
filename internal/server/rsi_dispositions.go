// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/marcelocantos/jevons/internal/rsi"
)

// RSI coach owner visibility (🎯T354): read-only product API over the durable
// disposition store (🎯T333), so the owner sees what the coach judged and what
// the overseer did with it — not only MCP chat dumps in the overseer session.

// DefaultRSIJudgmentLimit caps the bare list; the store is small but the API
// must not stream an unbounded history into the RHS panel.
const DefaultRSIJudgmentLimit = 200

type rsiJudgmentsResponse struct {
	Judgments []rsi.DispositionEntry `json:"judgments"`
	Count     int                    `json:"count"`
	Total     int                    `json:"total"`
	Pending   int                    `json:"pending"`
	Path      string                 `json:"path,omitempty"`
	Filter    string                 `json:"filter,omitempty"`
}

// handleRSIDispositions GET /api/rsi/dispositions — judgment + disposition list.
// Empty store is an honest empty list, never an error.
func (s *Server) handleRSIDispositions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "GET only")
		return
	}
	s.mu.RLock()
	dir := s.stateDir
	s.mu.RUnlock()
	if dir == "" {
		writeJSONError(w, http.StatusInternalServerError, "rsi dispositions: stateDir not configured")
		return
	}
	store, err := rsi.OpenDispositionStore(dir)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	entries, err := store.Entries()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	filter, err := normalizeRSIDispositionFilter(r.URL.Query().Get("disposition"))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	limit := DefaultRSIJudgmentLimit
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			writeJSONError(w, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		limit = n
	}

	out := make([]rsi.DispositionEntry, 0, len(entries))
	pending := 0
	for _, e := range entries {
		if rsiDispositionOf(e) == rsi.DispositionPending {
			pending++
		}
		if filter != "" && rsiDispositionOf(e) != filter {
			continue
		}
		// Normalize on the wire so clients never have to guess "" == pending.
		e.Disposition = rsiDispositionOf(e)
		out = append(out, e)
	}
	if len(out) > limit {
		out = out[:limit]
	}
	writeJSON(w, rsiJudgmentsResponse{
		Judgments: out,
		Count:     len(out),
		Total:     len(entries),
		Pending:   pending,
		Path:      store.Path(),
		Filter:    filter,
	})
}

// rsiDispositionOf treats a blank disposition as pending (store default).
func rsiDispositionOf(e rsi.DispositionEntry) string {
	d := strings.TrimSpace(e.Disposition)
	if d == "" {
		return rsi.DispositionPending
	}
	return d
}

// normalizeRSIDispositionFilter validates the optional ?disposition= filter.
func normalizeRSIDispositionFilter(raw string) (string, error) {
	f := strings.ToLower(strings.TrimSpace(raw))
	switch f {
	case "", "all":
		return "", nil
	case rsi.DispositionPending, rsi.DispositionFile, rsi.DispositionPark,
		rsi.DispositionIgnoreWithReason, rsi.DispositionActOther:
		return f, nil
	}
	return "", fmt.Errorf("disposition must be one of pending|file|park|ignore_with_reason|act_other|all, got %q", raw)
}

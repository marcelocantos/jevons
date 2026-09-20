// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/marcelocantos/jevons/internal/gate"
)

// handleGetGate GET /api/gates/{id} — supervisor lookup of a cited GATE
// record (🎯T697). The daemon reads the store; the caller does not need
// filesystem access to ~/.jevons/gates or permission to run bin/gate.
//
// An id with no record (or an id that is not a legal handle) returns 404
// with error=not-found, which is distinguishable from a pass.
func (s *Server) handleGetGate(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if !gate.ValidRecordID(id) {
		writeJSONStatus(w, http.StatusNotFound, gate.NewNotFoundView(id))
		return
	}
	store, err := gate.OpenStore("")
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	rec, ok := store.Lookup(id)
	if !ok || rec == nil {
		writeJSONStatus(w, http.StatusNotFound, gate.NewNotFoundView(id))
		return
	}
	writeJSON(w, gate.ViewOf(rec))
}

func writeJSONStatus(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Debug("writeJSONStatus", "err", err)
	}
}

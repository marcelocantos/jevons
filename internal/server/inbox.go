// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/marcelocantos/jevons/internal/notice"
)

// Fleet ops inbox HTTP surface (🎯T254.4): the durable structured
// terminal-outcome notices that jevons_inbox_list serves over MCP, for the
// cockpit and for any reader that is not an agent. Worker finish reports
// otherwise reach the owner's side only as faint activity-strip notes that
// scroll away; these records do not.

type inboxListResponse struct {
	Parent  string          `json:"parent,omitempty"` // empty = fleet-wide
	Notices []notice.Notice `json:"notices"`
	Count   int             `json:"count"`
}

// handleListInbox GET /api/inbox?parent=&outcome=&limit= — a parent PO's
// inbox, or with no parent the overseer's fleet-wide view. No limit returns
// every notice.
func (s *Server) handleListInbox(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	dir := s.stateDir
	s.mu.RUnlock()
	if dir == "" {
		writeJSONError(w, http.StatusInternalServerError, "inbox: stateDir not configured")
		return
	}
	qs := r.URL.Query()
	q := notice.Query{Parent: strings.TrimSpace(qs.Get("parent"))}
	if raw := strings.TrimSpace(qs.Get("outcome")); raw != "" {
		o, ok := notice.ParseOutcome(raw)
		if !ok {
			writeJSONError(w, http.StatusBadRequest, "unknown outcome (want done|blocked|needs-design|other)")
			return
		}
		q.Outcome = o
	}
	if raw := strings.TrimSpace(qs.Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			writeJSONError(w, http.StatusBadRequest, "limit must be a non-negative integer")
			return
		}
		q.Limit = n
	}
	list, err := notice.Select(dir, q)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if list == nil {
		list = []notice.Notice{}
	}
	writeJSON(w, inboxListResponse{Parent: q.Parent, Notices: list, Count: len(list)})
}

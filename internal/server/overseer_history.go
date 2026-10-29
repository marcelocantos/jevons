// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"strings"

	"github.com/marcelocantos/jevons/internal/chatlog"
)

// Owner chat history readers (🎯T613 / 🎯T548.2):
//
//   - Canonical: SQLite statedb for the overseer agent once it has rows.
//     /api/history, /ws/chat replay-on-connect, and inspect hydrate (when
//     statedb is configured) read here.
//
//   - Archive-only JSONL (import-once, no new turns after statedb import):
//     internal/server/statedb.go importJSONL — one-time fold into statedb;
//     internal/server/convomux.go muxJournalTail — byte tail when statedb n=0;
//     internal/mcpserver/open_intent.go loadOpenIntentDialogue — fallback when
//     no product database file exists;
//     internal/server/chat.go sendHistory(proc.JSONLPath()) — provider session
//     file when no chatlog.Log is configured (not the owner chatlog archive).
//
// Do not add new overseer history consumers against chatlog/jevons.jsonl
// without routing through overseerCanonicalHistory.

// overseerCanonicalHistory reports whether owner-chat history must be read
// from statedb. A configured store is canonical even when empty; JSONL is
// consulted only until the first successful import (🎯T548.2).
func (s *Server) overseerCanonicalHistory() bool {
	if s == nil || s.stateStore() == nil {
		return false
	}
	name := s.overseerAgentName()
	if s.statedbN(name) > 0 {
		return true
	}
	s.mu.RLock()
	clog := s.chatLog
	s.mu.RUnlock()
	if clog != nil {
		s.importJSONL(name, clog.Path())
	}
	return s.statedbN(name) > 0
}

// overseerHistoryReadRange pages owner history for GET /api/history. Indices
// are 0-based [start, end) over coalesced statedb events, matching the
// chatlog.ReadRange contract but with total = coalesced N (not raw JSONL lines).
func (s *Server) overseerHistoryReadRange(start, end int) ([]string, int, error) {
	name := s.overseerAgentName()
	total := s.statedbN(name)
	if total == 0 {
		return nil, 0, nil
	}
	if end <= 0 || end > total {
		end = total
	}
	if start < 0 {
		start = 0
	}
	if start > end {
		start = end
	}
	rows := s.statedbRange(name, start+1, end+1)
	lines := make([]string, 0, len(rows))
	for _, ev := range rows {
		ln := strings.TrimSpace(string(ev.Body))
		if ln == "" {
			continue
		}
		lines = append(lines, ln)
	}
	return lines, total, nil
}

// overseerReplayTailSealed streams the recent overseer journal for /ws/chat
// connect. start/total describe coalesced event indices (0-based start of the
// replayed window; total = statedb N), analogous to chatlog.ReplayTailSealed.
func (s *Server) overseerReplayTailSealed(maxUserTurns int, fn func(line string) error) (start, total int, err error) {
	name := s.overseerAgentName()
	total = s.statedbN(name)
	if total == 0 {
		return 0, 0, nil
	}
	lo := s.statedbTailStart(name, maxUserTurns)
	if lo < 1 {
		lo = 1
	}
	rows := s.statedbRange(name, lo, total+1)
	raw := make([]string, 0, len(rows))
	for _, ev := range rows {
		ln := strings.TrimSpace(string(ev.Body))
		if ln == "" {
			continue
		}
		raw = append(raw, ln)
	}
	sealed := chatlog.CoalesceStreamLines(raw)
	for _, ln := range sealed {
		if err := fn(ln); err != nil {
			return lo - 1, total, err
		}
	}
	return lo - 1, total, nil
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// The standing brief is injected once per seat session, but fleetBriefed is
// process memory: every daemon restart re-injected ~43 KB into each seat's
// next send, although that session already carried a copy (2026-10-27: twice
// into jevons-po in one afternoon). This file records which session id each
// seat was briefed in, so a restart does not repeat it. A new session id
// (fresh context) is briefed again.

const fleetBriefedFile = "briefed.json"

func (s *Server) fleetBriefedPath() string {
	if s == nil || strings.TrimSpace(s.stateDir) == "" {
		return ""
	}
	return filepath.Join(s.stateDir, "fleet", fleetBriefedFile)
}

// loadFleetBriefedSessions reads name → session id. A missing file is empty.
func loadFleetBriefedSessions(path string) (map[string]string, error) {
	out := map[string]string{}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return out, nil
}

func (s *Server) seatSessionID(name string) string {
	if s == nil || s.registry == nil {
		return ""
	}
	if d := s.registry.Def(name); d != nil {
		return strings.TrimSpace(d.SessionID)
	}
	return ""
}

// briefedInCurrentSession is true when the durable record says this seat's
// current session already received the standing brief.
func (s *Server) briefedInCurrentSession(name string) bool {
	path := s.fleetBriefedPath()
	sid := s.seatSessionID(name)
	if path == "" || sid == "" {
		return false
	}
	s.fleetBriefedFileMu.Lock()
	defer s.fleetBriefedFileMu.Unlock()
	m, err := loadFleetBriefedSessions(path)
	if err != nil {
		slog.Error("fleet brief record unreadable; briefing again", "path", path, "err", err)
		return false
	}
	return m[name] == sid
}

// recordBriefedSession notes that name's current session received the brief.
// A malformed record is left for inspection rather than overwritten.
func (s *Server) recordBriefedSession(name string) {
	path := s.fleetBriefedPath()
	sid := s.seatSessionID(name)
	if path == "" || sid == "" {
		return
	}
	s.fleetBriefedFileMu.Lock()
	defer s.fleetBriefedFileMu.Unlock()
	m, err := loadFleetBriefedSessions(path)
	if err != nil {
		slog.Error("fleet brief record unreadable; not recording", "path", path, "err", err)
		return
	}
	if m[name] == sid {
		return
	}
	m[name] = sid
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		slog.Error("fleet brief record marshal", "err", err)
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		slog.Error("fleet brief record dir", "path", path, "err", err)
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		slog.Error("fleet brief record write", "path", path, "err", err)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		slog.Error("fleet brief record rename", "path", path, "err", err)
	}
}

// ensureFleetBrief is the one inject-once path for agent_send and spawn
// prompts: memory first, then the durable per-session record.
func (s *Server) ensureFleetBrief(name, text, roleBody string) (string, bool) {
	durable := s.briefedInCurrentSession(name)
	s.mu.Lock()
	if s.fleetBriefed == nil {
		s.fleetBriefed = map[string]bool{}
	}
	if durable {
		s.fleetBriefed[name] = true
	}
	out, injected := EnsureFleetBriefWithRole(s.fleetBriefed, name, text, roleBody)
	s.mu.Unlock()
	if injected {
		s.recordBriefedSession(name)
	}
	return out, injected
}

// fleetBriefPresent answers the idle-nudge BriefPresent hook from the same
// memory-then-durable record, so a post-restart nudge does not resend a full
// brief the session already holds.
func (s *Server) fleetBriefPresent(name string) bool {
	s.mu.Lock()
	inMemory := s.fleetBriefed != nil && s.fleetBriefed[name]
	s.mu.Unlock()
	if inMemory {
		return true
	}
	if !s.briefedInCurrentSession(name) {
		return false
	}
	s.markFleetBriefedMemory(name)
	return true
}

// markFleetBriefed records a full-brief delivery in memory and durably.
func (s *Server) markFleetBriefed(name string) {
	s.markFleetBriefedMemory(name)
	s.recordBriefedSession(name)
}

func (s *Server) markFleetBriefedMemory(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fleetBriefed == nil {
		s.fleetBriefed = map[string]bool{}
	}
	s.fleetBriefed[name] = true
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// 🎯T1168 — once-per-hold fleet notices must survive a daemon restart.
//
// 🎯T582 / 🎯T706 keyed suppression by hold id in memory only. Each development
// bounce re-fired every held-backlog notice to the overseer. The record lives
// beside the durable sendq under state_dir.

const heldReapedAnnouncedFile = "held-reaped-noticed.json"

type heldReapedAnnouncedRecord struct {
	Agent string    `json:"agent"`
	At    time.Time `json:"at"`
}

// heldReapedStateDir is where hold-notice memory is persisted. Prefer the
// daemon state dir; fall back to the parent of <state>/sendq when only the
// queue wire is set (hermetic servers).
func (s *Server) heldReapedStateDir() string {
	if s == nil {
		return ""
	}
	if dir := strings.TrimSpace(s.deliveryStateDir()); dir != "" {
		return dir
	}
	if q := s.sendQueue(); q != nil {
		if dir := strings.TrimSpace(q.Dir()); dir != "" {
			return filepath.Dir(dir)
		}
	}
	return ""
}

func (s *Server) loadHeldReapedAnnounced() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.heldReapedAnnouncedLoad {
		s.mu.Unlock()
		return
	}
	s.heldReapedAnnouncedLoad = true
	if s.heldReapedAnnounced == nil {
		s.heldReapedAnnounced = map[string]struct{}{}
	}
	s.mu.Unlock()
	dir := s.heldReapedStateDir()
	if dir == "" {
		return
	}
	raw, err := os.ReadFile(filepath.Join(dir, heldReapedAnnouncedFile))
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Error("held reaped notice: read", "err", err)
		}
		return
	}
	var onDisk map[string]heldReapedAnnouncedRecord
	if err := json.Unmarshal(raw, &onDisk); err != nil {
		slog.Error("held reaped notice: malformed state; memory is in-process only", "err", err)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for hold, rec := range onDisk {
		hold = strings.TrimSpace(hold)
		if hold == "" {
			continue
		}
		s.heldReapedAnnounced[hold] = struct{}{}
		if rec.Agent != "" {
			if s.heldReapedNoticed == nil {
				s.heldReapedNoticed = map[string]string{}
			}
			s.heldReapedNoticed[strings.TrimSpace(rec.Agent)] = hold
		}
	}
}

func (s *Server) persistHeldReapedAnnounced(hold, agent string) {
	dir := s.heldReapedStateDir()
	if dir == "" || strings.TrimSpace(hold) == "" {
		return
	}
	path := filepath.Join(dir, heldReapedAnnouncedFile)
	var onDisk map[string]heldReapedAnnouncedRecord
	raw, err := os.ReadFile(path)
	if err == nil {
		_ = json.Unmarshal(raw, &onDisk)
	}
	if onDisk == nil {
		onDisk = map[string]heldReapedAnnouncedRecord{}
	}
	onDisk[hold] = heldReapedAnnouncedRecord{
		Agent: strings.TrimSpace(agent),
		At:    time.Now().UTC(),
	}
	out, err := json.Marshal(onDisk)
	if err != nil {
		slog.Error("held reaped notice: marshal", "err", err)
		return
	}
	tmp := path + ".tmp"
	if err = os.WriteFile(tmp, out, 0o644); err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		slog.Error("held reaped notice: persist", "err", err)
	}
}

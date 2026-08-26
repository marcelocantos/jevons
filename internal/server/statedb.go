// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"bufio"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/muxwin"
	"github.com/marcelocantos/jevons/internal/statedb"
)

// SetStateDB attaches the product SQLite store (🎯T548).
func (s *Server) SetStateDB(db *statedb.Store) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.stateDB = db
	s.mu.Unlock()
}

func (s *Server) stateStore() *statedb.Store {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.stateDB
}

func muxEventToRow(ev muxwin.Event) statedb.Event {
	return statedb.Event{
		Index: ev.Index,
		ID:    ev.ID,
		TS:    ev.TS,
		Type:  ev.Type,
		Kind:  int(ev.Kind),
		Body:  string(ev.Body),
	}
}

func rowToMuxEvent(ev statedb.Event) muxwin.Event {
	return muxwin.Event{
		ID:    ev.ID,
		Index: ev.Index,
		Kind:  muxwin.Kind(ev.Kind),
		Type:  ev.Type,
		TS:    ev.TS,
		Body:  json.RawMessage(ev.Body),
	}
}

func rowsToMuxEvents(rows []statedb.Event) []muxwin.Event {
	out := make([]muxwin.Event, len(rows))
	for i, r := range rows {
		out[i] = rowToMuxEvent(r)
	}
	return out
}

func (s *Server) statedbUpsertFolds(name string, folds []muxwin.LiveFold) {
	db := s.stateStore()
	if db == nil || len(folds) == 0 {
		return
	}
	rows := make([]statedb.Event, 0, len(folds))
	for _, f := range folds {
		if f.Event.Index < 1 {
			continue
		}
		rows = append(rows, muxEventToRow(f.Event))
	}
	if err := db.Upsert(name, rows); err != nil {
		slog.Error("statedb: upsert transcript failed", "agent", name, "err", err)
	}
}

func (s *Server) statedbTailStart(name string, userTurns int) int {
	db := s.stateStore()
	if db == nil || userTurns <= 0 {
		return 0
	}
	lo, err := db.TailStart(name, userTurns)
	if err != nil {
		slog.Error("statedb: tail-start failed", "agent", name, "err", err)
		return 0
	}
	return lo
}

func (s *Server) statedbN(name string) int {
	db := s.stateStore()
	if db == nil {
		return 0
	}
	n, err := db.N(name)
	if err != nil {
		slog.Error("statedb: n failed", "agent", name, "err", err)
		return 0
	}
	return n
}

func (s *Server) statedbRange(name string, lo, hi int) []muxwin.Event {
	db := s.stateStore()
	if db == nil {
		return nil
	}
	rows, err := db.Range(name, lo, hi)
	if err != nil {
		slog.Error("statedb: range failed", "agent", name, "err", err)
		return nil
	}
	return rowsToMuxEvents(rows)
}

func (s *Server) statedbBefore(name string, before, limit int) []muxwin.Event {
	db := s.stateStore()
	if db == nil {
		return nil
	}
	rows, err := db.Before(name, before, limit)
	if err != nil {
		slog.Error("statedb: before failed", "agent", name, "err", err)
		return nil
	}
	return rowsToMuxEvents(rows)
}

// ImportTranscripts folds JSONL journals into statedb once per agent
// (🎯T548.2). A second boot with rows already present does not re-fold.
func (s *Server) ImportTranscripts() {
	if s == nil {
		return
	}
	db := s.stateStore()
	if db == nil {
		return
	}
	s.mu.RLock()
	clog := s.chatLog
	overseer := s.overseerAgentName()
	s.mu.RUnlock()
	if clog != nil {
		s.importJSONL(overseer, clog.Path())
	}
	dir := filepath.Join(s.stateDir, agentChatLogDirName)
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, ent := range ents {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".jsonl") {
			continue
		}
		name := strings.TrimSuffix(ent.Name(), ".jsonl")
		if name == "" || name == overseer {
			continue
		}
		s.importJSONL(name, filepath.Join(dir, ent.Name()))
	}
}

func (s *Server) importJSONL(agent, path string) {
	db := s.stateStore()
	if db == nil || strings.TrimSpace(agent) == "" || strings.TrimSpace(path) == "" {
		return
	}
	ok, err := db.ShouldImport(agent)
	if err != nil {
		slog.Error("statedb: should-import failed", "agent", agent, "err", err)
		return
	}
	if !ok {
		return
	}
	lines, err := readImportLines(path)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Error("statedb: read jsonl failed", "agent", agent, "path", path, "err", err)
		}
		return
	}
	evs := muxwin.EventsFromLines(lines)
	rows := make([]statedb.Event, len(evs))
	for i, ev := range evs {
		rows[i] = muxEventToRow(ev)
	}
	if err := db.ReplaceAll(agent, rows); err != nil {
		slog.Error("statedb: import replace failed", "agent", agent, "err", err)
		return
	}
	if err := db.SetWatermark(agent, path, statedb.JSONLSize(path), len(rows)); err != nil {
		slog.Error("statedb: import watermark failed", "agent", agent, "err", err)
	}
	slog.Info("statedb: imported transcript", "agent", agent, "n", len(rows), "path", path)
}

func readImportLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16<<20)
	var lines []string
	for sc.Scan() {
		ln := strings.TrimSpace(sc.Text())
		if ln == "" || echoedOwnerTurnLine(ln) {
			continue
		}
		lines = append(lines, ln)
	}
	return lines, sc.Err()
}

func (s *Server) projectAgents() {
	db := s.stateStore()
	if db == nil {
		return
	}
	defs := s.RegistryAgents()
	s.mu.RLock()
	reg := s.registry
	s.mu.RUnlock()
	live := make([]statedb.Agent, 0, len(defs))
	for _, d := range defs {
		status := "stopped"
		if reg != nil {
			if p := reg.Get(d.Name); p != nil && p.Alive() {
				status = "running"
			}
		}
		live = append(live, agentRow(d, status))
	}
	if err := db.ReplaceAgents(live); err != nil {
		slog.Error("statedb: project agents failed", "err", err)
	}
}

func agentRow(d claudia.AgentDef, status string) statedb.Agent {
	return statedb.Agent{
		Name:      d.Name,
		Parent:    d.Parent,
		Purpose:   d.Purpose,
		TargetID:  d.TargetID,
		SessionID: d.SessionID,
		Provider:  string(d.Provider),
		Model:     d.Model,
		WorkDir:   d.WorkDir,
		Status:    status,
	}
}

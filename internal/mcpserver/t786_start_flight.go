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
	"sync"
	"time"

	"github.com/marcelocantos/claudia"
)

// 🎯T786 — a jevons_agent_start detached flight that outlives the caller must
// not survive a daemon bounce as a briefless registry row holding a provider
// cap slot. The ledger records open flights; shutdown marks them interrupted;
// the successor daemon reaps any seat that never finished briefing.

const startFlightsFile = "start-flights.json"

type startFlightRecord struct {
	StartedAt   time.Time `json:"started_at"`
	Interrupted bool      `json:"interrupted,omitempty"`
}

type startFlightLedger struct {
	mu      sync.Mutex
	path    string
	records map[string]startFlightRecord
	loadErr error
}

func newStartFlightLedger(path string) *startFlightLedger {
	l := &startFlightLedger{
		path:    strings.TrimSpace(path),
		records: map[string]startFlightRecord{},
	}
	if l.path != "" {
		l.loadErr = l.load()
	}
	return l
}

func (l *startFlightLedger) load() error {
	data, err := os.ReadFile(l.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("start-flights: read %s: %w", l.path, err)
	}
	var records map[string]startFlightRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return fmt.Errorf("start-flights: malformed %s: %w", l.path, err)
	}
	if records == nil {
		records = map[string]startFlightRecord{}
	}
	l.records = records
	return nil
}

func (l *startFlightLedger) persistLocked() error {
	if l.path == "" {
		return nil
	}
	if l.loadErr != nil {
		return l.loadErr
	}
	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return fmt.Errorf("start-flights: dir: %w", err)
	}
	data, err := json.MarshalIndent(l.records, "", "  ")
	if err != nil {
		return fmt.Errorf("start-flights: encode: %w", err)
	}
	tmp := l.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("start-flights: write: %w", err)
	}
	if err := os.Rename(tmp, l.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("start-flights: commit: %w", err)
	}
	return nil
}

func (s *Server) startFlightsLedger() *startFlightLedger {
	if s == nil {
		return newStartFlightLedger("")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.startFlightStore == nil {
		s.startFlightStore = newStartFlightLedger("")
	}
	return s.startFlightStore
}

func (s *Server) noteStartFlightOpen(name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	l := s.startFlightsLedger()
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.loadErr != nil {
		return
	}
	l.records[name] = startFlightRecord{StartedAt: time.Now().UTC()}
	if err := l.persistLocked(); err != nil {
		slog.Error("start-flight: persist open", "agent", name, "err", err)
	}
}

func (s *Server) noteStartFlightDone(name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	l := s.startFlightsLedger()
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.loadErr != nil {
		return
	}
	if _, ok := l.records[name]; !ok {
		return
	}
	delete(l.records, name)
	if err := l.persistLocked(); err != nil {
		slog.Error("start-flight: persist done", "agent", name, "err", err)
	}
}

// SettleStartFlightsOnShutdown marks every open detached start as interrupted
// so the successor daemon can reap briefless rows (🎯T786).
func (s *Server) SettleStartFlightsOnShutdown() {
	l := s.startFlightsLedger()
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.loadErr != nil || len(l.records) == 0 {
		return
	}
	for name, rec := range l.records {
		rec.Interrupted = true
		l.records[name] = rec
	}
	if err := l.persistLocked(); err != nil {
		slog.Error("start-flight: persist shutdown", "err", err)
		return
	}
	slog.Info("start-flight: marked interrupted detached starts",
		"component", compAgentLifecycle, "count", len(l.records))
}

// ReapInterruptedStartFlights removes registry rows left by a bounce-interrupted
// jevons_agent_start. Called once at boot before the fleet is running.
func (s *Server) ReapInterruptedStartFlights() {
	if s == nil || s.registry == nil {
		return
	}
	l := s.startFlightsLedger()
	l.mu.Lock()
	if l.loadErr != nil {
		l.mu.Unlock()
		return
	}
	pending := make(map[string]startFlightRecord, len(l.records))
	for name, rec := range l.records {
		if rec.Interrupted {
			pending[name] = rec
		}
	}
	l.mu.Unlock()
	if len(pending) == 0 {
		return
	}
	var reaped []string
	for name := range pending {
		def := s.registry.Def(name)
		if def == nil {
			s.clearInterruptedStartFlight(name)
			continue
		}
		if def.Materialized && s.seatAlive(name) {
			s.clearInterruptedStartFlight(name)
			continue
		}
		cause := errors.New("daemon bounce interrupted jevons_agent_start before the opening brief landed (🎯T786)")
		if s.releaseUnbriefedSeat(name, false, cause) {
			reaped = append(reaped, name)
		}
		s.clearInterruptedStartFlight(name)
	}
	if len(reaped) > 0 {
		slog.Info("start-flight: reaped bounce-interrupted mints",
			"component", compAgentLifecycle, "agents", reaped)
	}
}

// reapBornStuckSeat removes a born-stuck row after its parent was notified,
// so a cap slot is not held when the overseer is down (🎯T786).
func (s *Server) reapBornStuckSeat(d claudia.AgentDef) {
	if s == nil || s.registry == nil || strings.TrimSpace(d.Name) == "" {
		return
	}
	cause := errors.New("born-stuck seat reaped by daemon after parent notice (🎯T786)")
	if !s.releaseUnbriefedSeat(d.Name, true, cause) {
		slog.Warn("born-stuck: daemon reap did not remove seat", "agent", d.Name)
	}
}

func (s *Server) clearInterruptedStartFlight(name string) {
	l := s.startFlightsLedger()
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.loadErr != nil {
		return
	}
	delete(l.records, name)
	if err := l.persistLocked(); err != nil {
		slog.Error("start-flight: persist clear", "agent", name, "err", err)
	}
}

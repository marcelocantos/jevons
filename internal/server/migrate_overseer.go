// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/handover"
)

// Overseer provider migration (🎯T285).
//
// Fleet agents migrate through jevons_agent_migrate, which is registry
// work. The overseer cannot: its process is attached to owner chat by
// this server, so rotating the registry row alone would leave a running
// agent nobody is talking to — the conversation goes quiet with nothing
// visibly broken. The rotate half is shared with the fleet path; the
// attach and seed halves live here, mirroring RewindOverseer.

// OverseerMigrator is the registry half of a provider switch, implemented
// by *fleet.Claudia. Nil leaves overseer migration unavailable rather than
// half-wired.
type OverseerMigrator interface {
	PrepareMigration(name string, to claudia.Provider, force bool) (handover.Pending, error)
	PendingHandover(name string) (handover.Pending, bool, error)
	MarkHandoverDelivered(name string) error
}

// SetOverseerMigrator wires the provider-migration capability.
func (s *Server) SetOverseerMigrator(m OverseerMigrator) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.overseerMigrator = m
}

// MigrateOverseer switches the overseer to another backend and hands its
// successor the predecessor's transcript.
//
// The owner-visible chatlog is untouched: it is provider-agnostic and
// replays to clients exactly as before. What changes is the agent behind
// it, which starts a new conversation knowing only where to read.
func (s *Server) MigrateOverseer(to claudia.Provider, force bool) (handover.Pending, error) {
	target := claudia.Provider(strings.TrimSpace(string(to)))
	if target == "" {
		return handover.Pending{}, fmt.Errorf("migrate overseer: target provider is required")
	}
	s.mu.RLock()
	reg := s.registry
	mig := s.overseerMigrator
	name := s.overseerName
	s.mu.RUnlock()
	if reg == nil {
		return handover.Pending{}, fmt.Errorf("migrate overseer: no registry")
	}
	if mig == nil {
		return handover.Pending{}, fmt.Errorf("migrate overseer: migration is not wired")
	}

	// Rotate + persist the transcript pointer (shared with the fleet path).
	pending, err := mig.PrepareMigration(name, target, force)
	if err != nil {
		return handover.Pending{}, err
	}

	// From here the row is already rotated, so failures must leave a
	// legible state rather than pretend nothing happened: the handover is
	// on disk, and cockpit converge (🎯T204) keeps retrying the launch.
	agent, lerr := reg.Launch(name)
	if lerr != nil {
		s.SetOverseerDownReason("provider migration relaunch failed: " + lerr.Error())
		s.NotifyAgentsChanged()
		return pending, fmt.Errorf("migrate overseer: relaunch on %s failed: %w (handover stays pending)",
			target, lerr)
	}
	s.AttachOverseer(agent)
	s.SetOverseerDownReason("")
	s.NotifyAgentsChanged()

	s.ResumePendingHandover()
	slog.Info("overseer migrated", "detail", pending.Describe())
	return pending, nil
}

// ResumePendingHandover delivers the overseer's pending handover, if one is
// waiting. It is a no-op on every ordinary attach, and the whole recovery
// story on the ones that matter: a migration whose relaunch failed, or a
// daemon that died between rotating the row and seeding the successor.
// Without it the record would sit on disk forever and the successor would
// run on with no idea it had a predecessor — the silent history loss this
// path exists to prevent.
//
// Delivery goes through sendNotes rather than SendToOverseer because the
// mark must be honest: SendToOverseer only enqueues, and the queue is
// in-memory, so marking on enqueue would lose the handover on the next
// crash. A bare Send is right *here* (unlike the fleet path, which needs
// Deliver) because AttachOverseer has already subscribed the chat stream to
// this process, so the ACP response has a consumer.
func (s *Server) ResumePendingHandover() {
	s.mu.RLock()
	mig := s.overseerMigrator
	name := s.overseerName
	s.mu.RUnlock()
	if mig == nil || name == "" {
		return
	}
	pending, ok, err := mig.PendingHandover(name)
	if err != nil {
		slog.Error("overseer handover lookup failed", "agent", name, "err", err)
		return
	}
	seed := pending.Seed()
	if !ok || !pending.Usable() || seed == "" {
		return
	}

	// Single-flight: the record stays undelivered until the send returns,
	// and both migration and cockpit converge can reach this at once.
	s.mu.Lock()
	if s.handoverSeeding {
		s.mu.Unlock()
		return
	}
	s.handoverSeeding = true
	s.mu.Unlock()

	// Asynchronous because the caller is either an owner HTTP request or the
	// converge tick, and neither should block on the overseer's send path.
	go func() {
		err := s.sendNotes(seed)
		s.mu.Lock()
		s.handoverSeeding = false
		if err == nil {
			// Mirror a successful drain: a turn is now in flight, so
			// stuck-busy detection must be able to see it.
			s.waiting = true
			s.overseerLastProgress = time.Now()
		}
		s.mu.Unlock()
		if err != nil {
			// Left pending on purpose — the next attach or relaunch retries.
			slog.Warn("overseer handover not delivered; it stays pending",
				"agent", name, "err_class", notifyErrClass(err), "err", err)
			return
		}
		if err := mig.MarkHandoverDelivered(name); err != nil {
			slog.Error("overseer handover delivered but not marked; it may be seeded twice",
				"agent", name, "err", err)
		}
		slog.Info("overseer handover delivered", "detail", pending.Describe())
	}()
}

// handleOverseerMigrate is POST /api/overseer/migrate — owner-driven, not
// agent-driven: the overseer cannot sensibly rotate the session it is
// answering from, so this is deliberately an HTTP call the owner makes
// (the MCP tool refuses overseer rows).
func (s *Server) handleOverseerMigrate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Provider string `json:"provider"`
		Force    bool   `json:"force"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && r.ContentLength > 0 {
		http.Error(w, `{"error":"invalid JSON body"}`, http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(body.Provider) == "" {
		body.Provider = r.URL.Query().Get("provider")
	}
	if strings.TrimSpace(body.Provider) == "" {
		http.Error(w, `{"error":"provider is required"}`, http.StatusBadRequest)
		return
	}

	pending, err := s.MigrateOverseer(claudia.Provider(body.Provider), body.Force)
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":   err.Error(),
			"pending": pending.TranscriptPath,
		})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":         true,
		"from":       pending.From,
		"to":         pending.To,
		"transcript": pending.TranscriptPath,
		"detail":     pending.Describe(),
	})
}

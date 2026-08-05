// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/agenterr"
)

// createAsideRequest is the JSON body for POST /api/asides (🎯T136 / 🎯T263).
// Owner create path (aside:/capture:/target:) registers a purpose=aside
// fleet participant so the RHS tree shows 💡 nodes without top chip chrome.
// Freeform aside: may also pass Text so the process starts and the opening
// prompt is delivered in the same request (not registry-only + empty transcript).
type createAsideRequest struct {
	// ID is the stable agent/attention id (e.g. att-… from the client model).
	ID string `json:"id"`
	// Title is the owner-facing 💡 label (description).
	Title string `json:"title"`
	// Parent is fleet lineage (default: overseer root).
	Parent string `json:"parent,omitempty"`
	// Text is the owner's opening prompt for freeform asides (🎯T263).
	// When non-empty after trim: register (or update), then start/rehydrate
	// and deliver Text fire-and-forget. Empty → register-only (T136 residual
	// for capture:/target: dual-write that does not need a live process yet).
	Text string `json:"text,omitempty"`
}

// createAsideResponse is returned on success.
type createAsideResponse struct {
	Name        string `json:"name"`
	Purpose     string `json:"purpose"`
	Parent      string `json:"parent"`
	Description string `json:"description"`
	WorkDir     string `json:"workdir"`
	Status      string `json:"status"`
	Created     bool   `json:"created"` // true when newly registered
	// Deliver is set when Text was provided and send succeeded:
	// "sent" | "rehydrated_sent" (same vocabulary as POST /api/agents/{name}/send).
	Deliver string `json:"deliver,omitempty"`
}

// ensureAsideAgent registers (or updates description for) a purpose=aside
// agent without launching a process (AutoStart=false). Tree + inspect use
// registry identity. 🎯T263: freeform opening text is delivered by
// handleCreateAside via sendToNamedAgent after this register step.
func (s *Server) ensureAsideAgent(id, title, parent string) (createAsideResponse, error) {
	id = strings.TrimSpace(id)
	title = strings.TrimSpace(title)
	parent = strings.TrimSpace(parent)
	if id == "" {
		return createAsideResponse{}, fmt.Errorf("id is required")
	}
	// Refuse overseer name collision.
	s.mu.RLock()
	overseer := s.overseerName
	if overseer == "" {
		overseer = defaultOverseerName
	}
	reg := s.registry
	stateDir := s.stateDir
	s.mu.RUnlock()

	if id == overseer {
		return createAsideResponse{}, fmt.Errorf("id cannot be the overseer name")
	}
	if parent == "" {
		parent = overseer
	}
	if parent == id {
		return createAsideResponse{}, fmt.Errorf("parent cannot equal id")
	}
	if reg == nil {
		return createAsideResponse{}, fmt.Errorf("no agent registry")
	}
	if title == "" {
		title = "aside"
	}

	workdir := filepath.Join(stateDir, "asides", id)
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		return createAsideResponse{}, fmt.Errorf("aside workdir: %w", err)
	}

	created := false
	if def := reg.Def(id); def != nil {
		// Update description / purpose / parent backfill; do not re-mint session.
		dirty := false
		if def.Purpose != claudia.PurposeAside {
			def.Purpose = claudia.PurposeAside
			dirty = true
		}
		if def.Description != title {
			def.Description = title
			dirty = true
		}
		if def.Parent == "" && parent != "" {
			def.Parent = parent
			dirty = true
		}
		if def.WorkDir == "" {
			def.WorkDir = workdir
			dirty = true
		}
		if dirty {
			if err := reg.Register(*def); err != nil {
				return createAsideResponse{}, err
			}
		}
		status := "stopped"
		if proc := reg.Get(id); proc != nil && proc.Alive() {
			status = "running"
		}
		return createAsideResponse{
			Name:        id,
			Purpose:     claudia.PurposeAside,
			Parent:      def.Parent,
			Description: def.Description,
			WorkDir:     def.WorkDir,
			Status:      status,
			Created:     false,
		}, nil
	}

	def := claudia.AgentDef{
		Name:        id,
		WorkDir:     workdir,
		SessionID:   uuid.New().String(),
		Provider:    s.resolvedDefaultProvider(),
		AutoStart:   false, // register-only; no process on create
		Parent:      parent,
		Purpose:     claudia.PurposeAside,
		Description: title,
	}
	if err := reg.Register(def); err != nil {
		return createAsideResponse{}, err
	}
	created = true
	slog.Info("aside registered",
		"component", "aside",
		"name", id,
		"parent", parent,
		"title", title,
	)
	return createAsideResponse{
		Name:        id,
		Purpose:     claudia.PurposeAside,
		Parent:      parent,
		Description: title,
		WorkDir:     workdir,
		Status:      "stopped",
		Created:     created,
	}, nil
}

// handleCreateAside POST /api/asides — dual-write purpose=aside into the
// fleet registry so create paths no longer leave only localStorage chips (🎯T136).
// 🎯T263: when req.Text is non-empty, start/rehydrate and deliver the opening
// prompt in the same turn. Start/deliver failure is a loud HTTP error (agent
// may remain registered stopped) — never silent empty transcript + stopped.
func (s *Server) handleCreateAside(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req createAsideRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	out, err := s.ensureAsideAgent(req.ID, req.Title, req.Parent)
	if err != nil {
		// No registry / bad id → 4xx; other → 500.
		msg := err.Error()
		code := http.StatusBadRequest
		if strings.Contains(msg, "no agent registry") {
			code = http.StatusServiceUnavailable
		}
		http.Error(w, msg, code)
		return
	}

	opening := strings.TrimSpace(req.Text)
	if opening != "" {
		// Same deliver path as sidebar / frontier play (rehydrate + Send).
		deliverStatus, derr := s.sendToNamedAgent(out.Name, opening)
		if derr != nil {
			// Loud fail: registry row may exist, but owner must see the error
			// (not a stopped empty transcript with no indication).
			s.NotifyAgentsChanged()
			class, ownerMsg := agenterr.ClassifyAndFormat(derr)
			if !class.IsFailure() {
				ownerMsg = derr.Error()
			}
			slog.Warn("aside_create_deliver_failed",
				"component", "aside",
				"name", out.Name,
				"failure_class", class.String(),
				"err", derr.Error(),
			)
			code := http.StatusBadGateway
			msg := derr.Error()
			if strings.Contains(msg, "not registered") || strings.Contains(msg, "not available") {
				code = http.StatusNotFound
			} else if agenterr.IsPromptBusy(derr) {
				code = http.StatusConflict
			}
			// Prefix so UI can distinguish create-register-ok vs deliver fail.
			writeJSONErrorClass(w, code, "aside created but start/deliver failed: "+ownerMsg, class)
			return
		}
		out.Status = "running"
		out.Deliver = deliverStatus
		// Seed glanceable working phase until ACP progress arrives (RHS chrome).
		_ = s.ObserveAgentProgress(out.Name, claudia.Event{Type: "user"})
		slog.Info("aside_create_delivered",
			"component", "aside",
			"name", out.Name,
			"deliver", deliverStatus,
			"created", out.Created,
		)
	}

	s.NotifyAgentsChanged()
	w.Header().Set("Content-Type", "application/json")
	if out.Created {
		w.WriteHeader(http.StatusCreated)
	}
	_ = json.NewEncoder(w).Encode(out)
}

// dismissAsideAgent stops and deregisters a purpose=aside fleet agent by id
// (🎯T152 reverse of ensureAsideAgent / POST /api/asides). Refuses overseer
// and non-aside purposes so filing auto-close cannot kill workers.
func (s *Server) dismissAsideAgent(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("id is required")
	}
	s.mu.RLock()
	overseer := s.overseerName
	if overseer == "" {
		overseer = defaultOverseerName
	}
	reg := s.registry
	s.mu.RUnlock()

	if id == overseer {
		return fmt.Errorf("cannot dismiss the overseer")
	}
	if reg == nil {
		return fmt.Errorf("no agent registry")
	}
	def := reg.Def(id)
	if def == nil {
		// Idempotent: already gone is success for the close path.
		return nil
	}
	if def.Purpose != claudia.PurposeAside {
		return fmt.Errorf("agent %q purpose %q is not aside; refuse dismiss", id, def.Purpose)
	}
	if err := reg.Remove(id); err != nil {
		return fmt.Errorf("remove aside %q: %w", id, err)
	}
	slog.Info("aside dismissed",
		"component", "aside",
		"name", id,
	)
	return nil
}

// handleDeleteAside DELETE /api/asides/{id} — stop/deregister a purpose=aside
// fleet agent so RHS 💡 chrome leaves with the filing close path (🎯T152).
func (s *Server) handleDeleteAside(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		http.Error(w, "id is required", http.StatusBadRequest)
		return
	}
	if err := s.dismissAsideAgent(id); err != nil {
		msg := err.Error()
		code := http.StatusBadRequest
		switch {
		case strings.Contains(msg, "no agent registry"):
			code = http.StatusServiceUnavailable
		case strings.Contains(msg, "not aside"):
			code = http.StatusConflict
		case strings.Contains(msg, "overseer"):
			code = http.StatusForbidden
		}
		http.Error(w, msg, code)
		return
	}
	s.NotifyAgentsChanged()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"name":     id,
		"dismissed": true,
	})
}

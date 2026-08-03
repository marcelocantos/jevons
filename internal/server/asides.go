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
)

// createAsideRequest is the JSON body for POST /api/asides (🎯T136).
// Owner create path (aside:/capture:/target:) registers a purpose=aside
// fleet participant so the RHS tree shows 💡 nodes without top chip chrome.
type createAsideRequest struct {
	// ID is the stable agent/attention id (e.g. att-… from the client model).
	ID string `json:"id"`
	// Title is the owner-facing 💡 label (description).
	Title string `json:"title"`
	// Parent is fleet lineage (default: overseer root).
	Parent string `json:"parent,omitempty"`
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
}

// ensureAsideAgent registers (or updates description for) a purpose=aside
// agent without launching a process (AutoStart=false). Tree + inspect use
// registry identity; process is disposable later if Direct/Deliver needs it.
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
	s.NotifyAgentsChanged()
	w.Header().Set("Content-Type", "application/json")
	if out.Created {
		w.WriteHeader(http.StatusCreated)
	}
	_ = json.NewEncoder(w).Encode(out)
}

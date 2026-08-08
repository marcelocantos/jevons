// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/marcelocantos/jevons/internal/secauditor"
	"github.com/marcelocantos/jevons/internal/writconf"
)

// Security wiring (🎯T335): confined exec + auditor status on the daily path.

// SetSecurityAuditor attaches the standing security interest.
func (s *Server) SetSecurityAuditor(a *secauditor.Interest) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.secAuditor = a
	s.mu.Unlock()
}

// SetWritExecutor attaches the writ confinement executor (CLI or pure).
func (s *Server) SetWritExecutor(ex writconf.Executor, bin string, available bool) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.writExec = ex
	s.writBin = bin
	s.writAvailable = available
	s.mu.Unlock()
}

type confinedExecRequest struct {
	Argv      []string `json:"argv"`
	Cwd       string   `json:"cwd,omitempty"`
	Agent     string   `json:"agent,omitempty"`
	NetHosts  []string `json:"net_hosts,omitempty"`
	IncludeFS bool     `json:"include_fs,omitempty"`
	// Pure forces the hermetic PureExecutor (tests / no writ).
	Pure bool `json:"pure,omitempty"`
}

// handleSecurityStatus GET /api/security/status — T194 daily-path probe.
func (s *Server) handleSecurityStatus(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	a := s.secAuditor
	bin := s.writBin
	avail := s.writAvailable
	s.mu.RUnlock()

	st := secauditor.Status{
		Auditor:       secauditor.DefaultName,
		Ready:         a != nil,
		Confinement:   "writ",
		WritAvailable: avail,
		WritBin:       bin,
	}
	if a != nil {
		st.RecentAlerts = len(a.Recent())
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(st)
}

// handleConfinedExec POST /api/security/confined-exec — fleet high-risk exec
// under writ manifest → seatbelt/egress (🎯T335 vertical).
func (s *Server) handleConfinedExec(w http.ResponseWriter, r *http.Request) {
	var req confinedExecRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if len(req.Argv) == 0 {
		http.Error(w, "argv required", http.StatusBadRequest)
		return
	}

	cwd := strings.TrimSpace(req.Cwd)
	if cwd == "" {
		cwd = s.stateDir
	}
	m, err := writconf.FleetManifest(writconf.FleetManifestArgs{
		WorkDir:   cwd,
		NetHosts:  req.NetHosts,
		IncludeFS: req.IncludeFS,
	})
	if err != nil {
		http.Error(w, "manifest: "+err.Error(), http.StatusBadRequest)
		return
	}

	s.mu.RLock()
	ex := s.writExec
	auditor := s.secAuditor
	s.mu.RUnlock()

	if req.Pure || ex == nil {
		ex = writconf.PureExecutor{}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	res, err := ex.Exec(ctx, &writconf.ExecArgs{
		Manifest: m,
		Argv:     req.Argv,
		WorkDir:  cwd,
		Agent:    req.Agent,
		Timeout:  25 * time.Second,
	})
	if res != nil && res.Denied && res.Event != nil && auditor != nil {
		auditor.Observe(*res.Event)
	}
	// Bypass interest: pure path when writ was expected but unavailable.
	if !req.Pure && s != nil {
		s.mu.RLock()
		avail := s.writAvailable
		s.mu.RUnlock()
		if !avail && auditor != nil && res != nil && !res.Denied {
			// Running without sandbox after product path requested confinement.
			auditor.Observe(writconf.ClassifyBypass(req.Agent, "confined-exec fell back outside writ"))
		}
	}

	out := map[string]any{
		"ok":       err == nil && res != nil && !res.Denied && res.ExitCode == 0,
		"manifest": m,
	}
	if res != nil {
		out["exit_code"] = res.ExitCode
		out["stdout"] = res.Stdout
		out["stderr"] = res.Stderr
		out["denied"] = res.Denied
		out["used_writ"] = res.UsedWrit
		if res.Event != nil {
			out["event"] = res.Event
		}
	}
	if err != nil {
		out["error"] = err.Error()
	}
	w.Header().Set("Content-Type", "application/json")
	if res != nil && res.Denied {
		w.WriteHeader(http.StatusForbidden)
	} else if err != nil {
		w.WriteHeader(http.StatusBadGateway)
	}
	_ = json.NewEncoder(w).Encode(out)
}

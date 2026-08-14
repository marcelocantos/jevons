// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/targetfile"
)

// 🎯T198: frontier engagement — explicit AgentDef.TargetID, never name parse.
// Owner play → stop ends engagement by killing work agents bound to that target.

// NormalizeTargetID strips optional 🎯 prefix and whitespace. Empty in → empty out.
func NormalizeTargetID(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	// Strip leading "🎯" (U+1F3AF) if present.
	s = strings.TrimPrefix(s, "🎯")
	s = strings.TrimSpace(s)
	return s
}

// AgentsEngagedOnTarget returns registered agent names whose TargetID matches
// targetID (after NormalizeTargetID) **within one ledger**. Name is never
// parsed for T-ids. Overseer is skipped. purpose=aside is included only if it
// has a TargetID (unusual; residual multi-agent attribution).
//
// 🎯T389: scopeWorkdir is any path inside the repo whose ledger allocated the
// id — the ledger's own directory, the frontier cwd, the asking agent's
// workdir. Ids are per-ledger, so an unscoped answer names other repos'
// workers; empty scopeWorkdir keeps the pre-T389 unscoped reading. Both sides
// go through targetfile.LedgerKey so one canonicalization decides the match.
func AgentsEngagedOnTarget(reg *claudia.Registry, targetID, scopeWorkdir string) []string {
	want := NormalizeTargetID(targetID)
	if reg == nil || want == "" {
		return nil
	}
	wantLedger := targetfile.LedgerKey(scopeWorkdir)
	var names []string
	for _, d := range reg.List() {
		if NormalizeTargetID(d.TargetID) != want {
			continue
		}
		if !targetfile.SameLedger(wantLedger, targetfile.LedgerKey(d.WorkDir)) {
			continue
		}
		// Never kill/stop the overseer via engagement stop.
		if d.Purpose == claudia.PurposeOverseer || d.Name == "jevons" {
			continue
		}
		names = append(names, d.Name)
	}
	sort.Strings(names)
	return names
}

// stopEngagement removes engaged agents for targetID in scopeWorkdir's ledger
// (and their descendant subtrees). Owner-UI authority: no lineage check
// (product control surface). Returns names that were still registered and
// removed.
func stopEngagement(reg *claudia.Registry, targetID, scopeWorkdir string) ([]string, error) {
	if reg == nil {
		return nil, fmt.Errorf("agent registry not available")
	}
	want := NormalizeTargetID(targetID)
	if want == "" {
		return nil, fmt.Errorf("target_id is required")
	}
	names := AgentsEngagedOnTarget(reg, want, scopeWorkdir)
	if len(names) == 0 {
		return nil, nil
	}
	// Kill only roots among the matching set first: if both parent and child
	// match, killing parent removes the child. Re-check Def before Remove.
	var removed []string
	for _, name := range names {
		if reg.Def(name) == nil {
			continue
		}
		// Descendants first (DFS reverse of Descendants list).
		desc := reg.Descendants(name)
		for i := len(desc) - 1; i >= 0; i-- {
			if err := reg.Remove(desc[i]); err != nil {
				return removed, fmt.Errorf("kill descendant %q of %q: %w", desc[i], name, err)
			}
			removed = append(removed, desc[i])
		}
		if err := reg.Remove(name); err != nil {
			return removed, fmt.Errorf("kill %q: %w", name, err)
		}
		removed = append(removed, name)
	}
	return removed, nil
}

// engagementStopRequest is POST /api/agents/engagement/stop JSON body.
type engagementStopRequest struct {
	TargetID string `json:"target_id"`
	// Cwd names the repo the id belongs to (🎯T389). The frontier table is
	// bound to one ledger at a time (🎯T253), so the default — the daemon's
	// current frontier cwd — is the ledger the owner is looking at when the
	// stop button is theirs to press. Sent explicitly by any caller that
	// knows better.
	Cwd string `json:"cwd,omitempty"`
}

// engagementStopResponse is the success body.
type engagementStopResponse struct {
	TargetID string   `json:"target_id"`
	Stopped  []string `json:"stopped"`
	Status   string   `json:"status"` // ok | none
}

// handleEngagementStop POST /api/agents/engagement/stop
// Kills fleet work agents engaged on target_id (registry TargetID equality).
func (s *Server) handleEngagementStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req engagementStopRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	tid := NormalizeTargetID(req.TargetID)
	if tid == "" {
		writeJSONError(w, http.StatusBadRequest, "target_id is required")
		return
	}

	s.mu.RLock()
	reg := s.registry
	s.mu.RUnlock()
	if reg == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "agent registry not available")
		return
	}

	scope := s.frontierCwdOr(req.Cwd)
	stopped, err := stopEngagement(reg, tid, scope)
	if err != nil {
		slog.Warn("engagement_stop",
			"component", "engagement",
			"target_id", tid,
			"cwd", scope,
			"err", err.Error(),
		)
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(stopped) > 0 {
		s.NotifyAgentsChanged()
	}
	status := "ok"
	if len(stopped) == 0 {
		status = "none"
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(engagementStopResponse{
		TargetID: tid,
		Stopped:  stopped,
		Status:   status,
	})
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
)

// PlanAuth is one subscription plan's login health as Claudia's broker
// reports it (🎯T924): ok, missing, expired or rejected.
type PlanAuth struct {
	Provider string    `json:"provider"`
	State    string    `json:"state"`
	Detail   string    `json:"detail,omitempty"`
	Since    time.Time `json:"since,omitzero"`
}

const (
	planAuthOK = "ok"
	// planAuthStatusTimeout bounds the CLI round-trip to the broker.
	planAuthStatusTimeout = 10 * time.Second
	// planAuthStatusTTL lets several cockpit tabs share one broker read.
	planAuthStatusTTL = 10 * time.Second
)

type planAuthCache struct {
	mu    sync.Mutex
	at    time.Time
	plans []PlanAuth
	err   error
}

// runClaudiaAuthStatus asks the broker for every plan's login health. It
// never starts a login; only the owner's Reauth does.
func runClaudiaAuthStatus(ctx context.Context) ([]PlanAuth, error) {
	bin := strings.TrimSpace(os.Getenv("CLAUDIA_BIN"))
	if bin == "" {
		bin = "claudia"
	}
	out, err := exec.CommandContext(ctx, bin, "broker", "auth-status", "--json").Output()
	if err != nil {
		msg := err.Error()
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			msg = strings.TrimSpace(string(ee.Stderr))
		}
		return nil, fmt.Errorf("Claudia plan login status: %s", msg)
	}
	var body struct {
		Plans []PlanAuth `json:"plans"`
	}
	if err := json.Unmarshal(out, &body); err != nil {
		return nil, fmt.Errorf("Claudia plan login status: %w", err)
	}
	return body.Plans, nil
}

// planAuthStatus is the broker's report, shared for planAuthStatusTTL.
func (s *Server) planAuthStatus(ctx context.Context) ([]PlanAuth, error) {
	read := s.authStatus
	if read == nil {
		if testing.Testing() {
			// A test never asks the owner's real broker about real logins.
			return nil, errors.New("Claudia plan login status: no broker under test")
		}
		read = runClaudiaAuthStatus
	}
	c := &s.planAuth
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.at.IsZero() && time.Since(c.at) < planAuthStatusTTL {
		return c.plans, c.err
	}
	ctx, cancel := context.WithTimeout(ctx, planAuthStatusTimeout)
	defer cancel()
	c.plans, c.err = read(ctx)
	c.at = time.Now()
	return c.plans, c.err
}

// forgetPlanAuthStatus drops the cached report, so the bar sees a repaired
// login on its next read.
func (s *Server) forgetPlanAuthStatus() {
	s.planAuth.mu.Lock()
	s.planAuth.at = time.Time{}
	s.planAuth.mu.Unlock()
}

// planLoginUnhealthy reports whether the broker says this plan's login needs
// the owner. An unreadable status is not a verdict either way.
func (s *Server) planLoginUnhealthy(ctx context.Context, provider claudia.Provider) bool {
	plans, err := s.planAuthStatus(ctx)
	if err != nil {
		return false
	}
	for _, p := range plans {
		if claudia.Provider(p.Provider) == provider {
			return p.State != planAuthOK
		}
	}
	return false
}

// handlePlanAuthStatus serves every plan's login health for the plan bar.
func (s *Server) handlePlanAuthStatus(w http.ResponseWriter, r *http.Request) {
	plans, err := s.planAuthStatus(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, err.Error())
		return
	}
	if plans == nil {
		plans = []PlanAuth{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"plans": plans})
}

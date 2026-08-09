// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/marcelocantos/jevons/internal/selftest"
)

// registerSelfTestRoutes mounts 🎯T110 self-test kick paths.
func (s *Server) registerSelfTestRoutes(mux router) {
	mux.HandleFunc("POST /api/self_test/run", s.handleSelfTestRun)
	mux.HandleFunc("GET /api/self_test/packs", s.handleSelfTestPacks)
}

func (s *Server) handleSelfTestPacks(w http.ResponseWriter, r *http.Request) {
	type packMeta struct {
		ID    string   `json:"id"`
		Class string   `json:"class"`
		Sites []string `json:"sites"`
	}
	var out []packMeta
	for _, p := range selftest.ListPacks() {
		sites := make([]string, 0, len(p.AllowedSites()))
		for _, site := range p.AllowedSites() {
			sites = append(sites, string(site))
		}
		out = append(out, packMeta{ID: p.ID(), Class: p.Class(), Sites: sites})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func (s *Server) handleSelfTestRun(w http.ResponseWriter, r *http.Request) {
	if rejectCrossSite(w, r) {
		return
	}
	var req selftest.RunRequest
	if r.Body != nil {
		defer r.Body.Close()
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err.Error() != "EOF" {
			http.Error(w, `{"error":"invalid json body"}`, http.StatusBadRequest)
			return
		}
	}
	if req.Site == "" {
		req.Site = selftest.SiteLive
	}
	result, err := selftest.Run(r.Context(), s.SelfTestEnv(), req)
	if err != nil {
		http.Error(w, `{"error":`+jsonString(err.Error())+`}`, http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

// SelfTestEnv builds an in-process Env so packs do not need loopback HTTP.
// Shared by POST /api/self_test/run and MCP self_test.run (🎯T110).
func (s *Server) SelfTestEnv() *selftest.Env {
	return &selftest.Env{
		OverseerName: s.overseerName,
		HealthGET: func(ctx context.Context) (int, map[string]any, error) {
			return 200, map[string]any{
				"status":  "ok",
				"version": s.version,
			}, nil
		},
		AgentsGET: func(ctx context.Context) ([]selftest.AgentRow, error) {
			s.mu.RLock()
			reg := s.registry
			s.mu.RUnlock()
			agents := listFleetAgents(reg)
			out := make([]selftest.AgentRow, 0, len(agents))
			for _, a := range agents {
				out = append(out, selftest.AgentRow{
					Name:   a.Name,
					Parent: a.Parent,
					Status: a.Status,
				})
			}
			return out, nil
		},
	}
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

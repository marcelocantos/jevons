// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package selftest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// AgentRow is the lineage slice of GET /api/agents needed by packs.
type AgentRow struct {
	Name   string `json:"name"`
	Parent string `json:"parent,omitempty"`
	Status string `json:"status,omitempty"`
}

// Env is the execution environment for a pack run.
// Prefer in-process hooks (HealthGET / AgentsGET) when the runner
// lives inside jevonsd; BaseURL is the loopback/external fallback.
type Env struct {
	Site         Site
	BaseURL      string // e.g. http://127.0.0.1:13705 — no trailing slash
	Client       *http.Client
	OverseerName string // default "jevons"

	// HealthGET returns HTTP status, JSON body fields, error.
	// Nil falls back to GET BaseURL/health.
	HealthGET func(ctx context.Context) (statusCode int, body map[string]any, err error)

	// AgentsGET returns fleet rows. Nil falls back to GET BaseURL/api/agents.
	AgentsGET func(ctx context.Context) ([]AgentRow, error)
}

func (e *Env) client() *http.Client {
	if e != nil && e.Client != nil {
		return e.Client
	}
	return &http.Client{Timeout: 5 * time.Second}
}

func (e *Env) overseer() string {
	if e != nil && e.OverseerName != "" {
		return e.OverseerName
	}
	return "jevons"
}

func (e *Env) base() string {
	if e == nil {
		return ""
	}
	return strings.TrimRight(e.BaseURL, "/")
}

// FetchHealth uses HealthGET or BaseURL/health.
func (e *Env) FetchHealth(ctx context.Context) (statusCode int, body map[string]any, err error) {
	if e != nil && e.HealthGET != nil {
		return e.HealthGET(ctx)
	}
	base := e.base()
	if base == "" {
		return 0, nil, fmt.Errorf("no HealthGET and empty BaseURL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/health", nil)
	if err != nil {
		return 0, nil, err
	}
	resp, err := e.client().Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	var m map[string]any
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &m)
	}
	if m == nil {
		m = map[string]any{}
	}
	return resp.StatusCode, m, nil
}

// FetchAgents uses AgentsGET or BaseURL/api/agents.
func (e *Env) FetchAgents(ctx context.Context) ([]AgentRow, error) {
	if e != nil && e.AgentsGET != nil {
		return e.AgentsGET(ctx)
	}
	base := e.base()
	if base == "" {
		return nil, fmt.Errorf("no AgentsGET and empty BaseURL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/agents", nil)
	if err != nil {
		return nil, err
	}
	resp, err := e.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET /api/agents status %d", resp.StatusCode)
	}
	var rows []AgentRow
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []AgentRow{}
	}
	return rows, nil
}

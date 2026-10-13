// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/planusage"
)

// fakeSwitcher records what POST /api/agents/switch-model asked of the
// fleet (claudia 🎯T195).
type fakeSwitcher struct {
	mu       sync.Mutex
	switches []string // "name→provider/model force"
	err      error
}

func (m *fakeSwitcher) SwitchModel(name string, to claudia.Provider, model string, force bool) (claudia.SeatSwitch, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec := name + "→" + string(to) + "/" + model
	if force {
		rec += " force"
	}
	m.switches = append(m.switches, rec)
	if m.err != nil {
		return claudia.SeatSwitch{}, m.err
	}
	return claudia.SeatSwitch{Live: true, Changed: true, From: "xai-oauth", To: to, Model: model}, nil
}

func t285Snapshot() planusage.Snapshot {
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	week := now.Add(3*24*time.Hour + 12*time.Hour)
	lim := planusage.DefaultWeeklyWindowSeconds
	pct := func(v float64) *float64 { return &v }
	weekly := func(provider string, rem, used float64) planusage.Backend {
		return planusage.Backend{
			Provider: provider, Status: planusage.StatusAvailable,
			Windows: []planusage.Window{{
				Name: planusage.WindowWeekly, RemainingPercent: pct(rem),
				UsedPercent: pct(used), ResetsAt: &week, LimitWindowSeconds: &lim,
			}},
		}
	}
	return planusage.Snapshot{At: now, Backends: []planusage.Backend{
		weekly("grok", 20, 80),   // hot → red "!", not choosable
		weekly("claude", 50, 50), // on pace → eligible dest
	}}
}

// TestT285_2MigrateOptionsPayload: GET /api/migrate/options serves the
// server-computed weekly band per provider (same Go classification as the
// sweep), a reason string, the best-first model list, and lists a
// provider with running seats even when the snapshot has no row for it.
func TestT285_2MigrateOptionsPayload(t *testing.T) {
	s := New("test", t.TempDir())
	reg, err := claudia.NewRegistry(t.TempDir() + "/agents.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jv-codex-seat", Provider: "openai-codex", Model: "gpt-5-codex", SessionID: "s-codex",
		WorkDir: t.TempDir(), Purpose: claudia.PurposeWork,
	}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jv-grok-seat", Provider: "xai-oauth", Model: "grok-4.5", SessionID: "s-grok",
		WorkDir: t.TempDir(), Purpose: claudia.PurposeWork,
	}); err != nil {
		t.Fatal(err)
	}
	s.SetRegistry(reg)
	s.SetPlanUsageSource(func() any { return t285Snapshot() })

	// Band assertions pin the snapshot's own clock; the HTTP handler is
	// exercised separately below for the wire shape.
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	providers := s.migrateOptions(now)
	byProv := map[string]migrateProviderOption{}
	for _, p := range providers {
		byProv[p.Provider] = p
	}
	if len(byProv) != len(providers) || len(byProv) != 3 {
		t.Fatalf("runtime aliases produced duplicate or missing plan rows: %+v", providers)
	}

	grok := byProv["grok"]
	if grok.Band != planusage.BandHot || grok.Eligible {
		t.Fatalf("grok band=%s eligible=%v", grok.Band, grok.Eligible)
	}
	if !strings.Contains(grok.Reason, "hot") {
		t.Fatalf("grok reason = %q", grok.Reason)
	}
	if len(grok.Models) == 0 || grok.Models[0] != "grok-4.6" {
		t.Fatalf("grok models = %v (want best first grok-4.6)", grok.Models)
	}

	cl := byProv["claude"]
	if cl.Band != planusage.BandOK || !cl.Eligible {
		t.Fatalf("claude band=%s eligible=%v", cl.Band, cl.Eligible)
	}
	if len(cl.Models) == 0 || cl.Models[0] != "claude-fable-5" {
		t.Fatalf("claude models = %v (want best first claude-fable-5)", cl.Models)
	}

	// codex has a running seat but no snapshot row: listed, unpublished,
	// with the observed running model offered.
	cx, ok := byProv["codex"]
	if !ok {
		t.Fatalf("codex (running seat, unpublished) missing from %v", providers)
	}
	if cx.Band != planusage.BandUnpublished || cx.Eligible {
		t.Fatalf("codex band=%s eligible=%v", cx.Band, cx.Eligible)
	}
	found := false
	for _, m := range cx.Models {
		if m == "gpt-5-codex" {
			found = true
		}
	}
	if !found {
		t.Fatalf("codex models = %v (want observed gpt-5-codex merged)", cx.Models)
	}

	// Wire shape: the endpoint serves {"providers":[…]} as JSON.
	rec := httptest.NewRecorder()
	s.handleMigrateOptions(rec, httptest.NewRequest("GET", "/api/migrate/options", nil))
	var got struct {
		Providers []migrateProviderOption `json:"providers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v (%s)", err, rec.Body.String())
	}
	if len(got.Providers) != len(providers) {
		t.Fatalf("wire providers = %d, computed = %d", len(got.Providers), len(providers))
	}
}

func TestCursorAPIExhaustedBlocksModelPick(t *testing.T) {
	now := time.Date(2026, 9, 24, 2, 0, 0, 0, time.UTC)
	reset := now.Add(21 * 24 * time.Hour)
	lim := int64(30 * 24 * 3600)
	rem, used := 50.0, 50.0
	zero, full := 0.0, 100.0
	s := New("test", t.TempDir())
	s.SetPlanUsageSource(func() any {
		return planusage.Snapshot{At: now, Backends: []planusage.Backend{{
			Provider: "cursor", Status: planusage.StatusAvailable, FetchedAt: now,
			Windows: []planusage.Window{
				{Name: planusage.WindowMonthly, RemainingPercent: &rem, UsedPercent: &used, ResetsAt: &reset, LimitWindowSeconds: &lim},
				{Name: "api", Model: "API", RemainingPercent: &zero, UsedPercent: &full, ResetsAt: &reset, LimitWindowSeconds: &lim},
			},
		}}}
	})
	var cursor migrateProviderOption
	for _, p := range s.migrateOptions(now) {
		if p.Provider == "cursor" {
			cursor = p
		}
	}
	if cursor.ModelPick == nil || *cursor.ModelPick {
		t.Fatalf("model_pick = %v", cursor.ModelPick)
	}
	if cursor.Eligible {
		t.Fatal("exhausted API bucket must not become a migration destination")
	}
	if !strings.Contains(cursor.Reason, "included API usage exhausted") {
		t.Fatalf("reason = %q", cursor.Reason)
	}
}

// TestSwitchModelRoute: one route switches every seat in place, the
// overseer included (an empty name), passing provider, model and force
// through; a refusal comes back as a conflict.
func TestSwitchModelRoute(t *testing.T) {
	s := New("test", t.TempDir())
	s.SetOverseerName("jevons")
	sw := &fakeSwitcher{}
	s.SetModelSwitcher(sw)
	post := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		s.handleSwitchModel(rec, httptest.NewRequest("POST", "/api/agents/switch-model", strings.NewReader(body)))
		return rec
	}

	rec := post(`{"name":"jv-worker","provider":"claude","model":"claude-opus-5"}`)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"live":true`) {
		t.Fatalf("worker switch: code=%d body=%s", rec.Code, rec.Body.String())
	}
	// The overseer switches in place too: no relaunch, so chat stays attached.
	rec = post(`{"provider":"claude"}`)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"name":"jevons"`) {
		t.Fatalf("overseer switch: code=%d body=%s", rec.Code, rec.Body.String())
	}
	// A model alone is a switch on the seat's own provider.
	rec = post(`{"name":"jv-worker","model":"grok-4","force":true}`)
	if rec.Code != 200 {
		t.Fatalf("model-only switch: code=%d body=%s", rec.Code, rec.Body.String())
	}
	want := []string{"jv-worker→claude/claude-opus-5", "jevons→claude/", "jv-worker→/grok-4 force"}
	if strings.Join(sw.switches, "|") != strings.Join(want, "|") {
		t.Fatalf("switches = %v, want %v", sw.switches, want)
	}

	if rec = post(`{"name":"jv-worker"}`); rec.Code != 400 {
		t.Fatalf("nothing to switch to: code=%d body=%s", rec.Code, rec.Body.String())
	}
	sw.err = errors.New("switch jv-worker: a turn is in flight; wait for it, or force to interrupt it")
	if rec = post(`{"name":"jv-worker","provider":"claude"}`); rec.Code != 409 || !strings.Contains(rec.Body.String(), "turn is in flight") {
		t.Fatalf("refusal: code=%d body=%s", rec.Code, rec.Body.String())
	}
}

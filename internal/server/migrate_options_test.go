// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/handover"
	"github.com/marcelocantos/jevons/internal/planusage"
	"github.com/marcelocantos/jevons/internal/thread"
)

// fakeFleetMigrator records what POST /api/agents/migrate asked of the
// fleet half (🎯T285.2).
type fakeFleetMigrator struct {
	mu       sync.Mutex
	prepared []string // "name→provider"
	launched []string // "name(model)"
	pinned   []string // "name=model"
}

func (m *fakeFleetMigrator) PrepareMigration(name string, to claudia.Provider, force bool) (handover.Pending, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prepared = append(m.prepared, name+"→"+string(to))
	return handover.Pending{Agent: name, From: "grok", To: string(to),
		TranscriptPath: "/tmp/t.jsonl"}, nil
}

func (m *fakeFleetMigrator) CompleteThinBrief(p handover.Pending) (handover.Pending, error) {
	return p, nil
}

func (m *fakeFleetMigrator) SeedSuccessor(name string) (handover.Pending, bool, error) {
	return handover.Pending{Agent: name, BriefSource: "distill"}, true, nil
}

func (m *fakeFleetMigrator) Launch(t *thread.Thread) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.launched = append(m.launched, t.ID+"("+t.Model+")")
	return nil
}

func (m *fakeFleetMigrator) PinModel(name, model string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pinned = append(m.pinned, name+"="+model)
	return nil
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
		Name: "jv-codex-seat", Provider: "codex", Model: "gpt-5-codex", SessionID: "s-codex",
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

	grok := byProv["grok"]
	if grok.Band != planusage.BandHot || grok.Eligible {
		t.Fatalf("grok band=%s eligible=%v", grok.Band, grok.Eligible)
	}
	if !strings.Contains(grok.Reason, "hot") {
		t.Fatalf("grok reason = %q", grok.Reason)
	}
	if len(grok.Models) == 0 || grok.Models[0] != "grok-4.5" {
		t.Fatalf("grok models = %v (want best first grok-4.5)", grok.Models)
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

// TestT285_2AgentMigrateRoutes: the thin HTTP wrapper refuses the overseer
// (that seat re-attaches chat through /api/overseer/migrate), pins on a
// same-provider model choice, and migrates cross-provider with the chosen
// model riding the successor's launch as its pin.
func TestT285_2AgentMigrateRoutes(t *testing.T) {
	s := New("test", t.TempDir())
	s.SetOverseerName("jevons")
	reg, err := claudia.NewRegistry(t.TempDir() + "/agents.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, def := range []claudia.AgentDef{
		{Name: "jevons", Provider: "grok", Purpose: claudia.PurposeOverseer, WorkDir: t.TempDir(), SessionID: "s-jevons"},
		{Name: "jv-worker", Provider: "grok", Purpose: claudia.PurposeWork, WorkDir: t.TempDir(), SessionID: "s-worker"},
	} {
		if err := reg.Register(def); err != nil {
			t.Fatal(err)
		}
	}
	s.SetRegistry(reg)
	mig := &fakeFleetMigrator{}
	s.SetFleetMigrator(mig)

	post := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/agents/migrate", strings.NewReader(body))
		s.handleAgentMigrateHTTP(rec, req)
		return rec
	}

	// Overseer refused → the other route.
	rec := post(`{"name":"jevons","provider":"claude"}`)
	if rec.Code != 409 || !strings.Contains(rec.Body.String(), "overseer/migrate") {
		t.Fatalf("overseer: code=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(mig.prepared) != 0 {
		t.Fatalf("overseer reached the fleet migrator: %v", mig.prepared)
	}

	// Same provider + model → pin, no rotation.
	rec = post(`{"name":"jv-worker","provider":"grok","model":"grok-4"}`)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"kind":"pin"`) {
		t.Fatalf("pin: code=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(mig.pinned) != 1 || mig.pinned[0] != "jv-worker=grok-4" {
		t.Fatalf("pinned = %v", mig.pinned)
	}
	if len(mig.prepared) != 0 {
		t.Fatalf("pin rotated: %v", mig.prepared)
	}

	// Cross-provider + model → migrate; the model rides the launch pin.
	rec = post(`{"name":"jv-worker","provider":"claude","model":"claude-opus-5"}`)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"kind":"migrate"`) {
		t.Fatalf("migrate: code=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(mig.prepared) != 1 || mig.prepared[0] != "jv-worker→claude" {
		t.Fatalf("prepared = %v", mig.prepared)
	}
	if len(mig.launched) != 1 || mig.launched[0] != "jv-worker(claude-opus-5)" {
		t.Fatalf("launched = %v", mig.launched)
	}

	// Same provider, no model → nothing to do.
	rec = post(`{"name":"jv-worker","provider":"grok"}`)
	if rec.Code != 409 || !strings.Contains(rec.Body.String(), "already on") {
		t.Fatalf("same-provider no-model: code=%d body=%s", rec.Code, rec.Body.String())
	}
}

// TestT285_2OverseerMigrateModelPin: the overseer menu choice carries a
// model; the rotate records it on the rotated row before relaunch, so the
// successor comes up on the chosen model rather than the provider default.
func TestT285_2OverseerMigrateModelPin(t *testing.T) {
	s := New("test", t.TempDir())
	s.SetOverseerName("jevons")
	reg, err := claudia.NewRegistry(t.TempDir() + "/agents.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jevons", Provider: "grok", Purpose: claudia.PurposeOverseer,
		WorkDir: t.TempDir(), SessionID: "s-jevons",
	}); err != nil {
		t.Fatal(err)
	}
	s.SetRegistry(reg)
	s.SetOverseerMigrator(&fakeOverseerMigrator{})

	// The relaunch of a fake def fails in a unit test (no real provider);
	// the pin must already be on the row by then — that ordering is the
	// point of the test.
	_, _ = s.MigrateOverseerModel(claudia.ProviderClaude, "claude-opus-5", true)
	def := reg.Def("jevons")
	if def == nil || def.Model != "claude-opus-5" {
		t.Fatalf("overseer model pin not recorded: %+v", def)
	}
}

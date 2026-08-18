// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/handover"
	"github.com/marcelocantos/jevons/internal/planusage"
	"github.com/marcelocantos/jevons/internal/thread"
)

// Fleet-tree provider menu + "!" band marks (🎯T285.2).
//
// GET /api/migrate/options is the single fetch behind both halves of the
// owner's manual migrate UI: the per-provider weekly band (computed HERE,
// by the same planusage.WeeklyBandOf the sweep uses — the client never
// re-derives it) and the model ids the menu offers per provider, best
// first. POST /api/agents/migrate is the thin HTTP wrapper the menu uses
// for every non-overseer seat; the overseer keeps its own
// POST /api/overseer/migrate (chat re-attach lives there).

// FleetMigrator is the fleet half of a non-overseer provider switch —
// the same capability jevons_agent_migrate uses, plus the same-provider
// model pin. Implemented by *fleet.Claudia; nil leaves the HTTP wrapper
// unavailable rather than half-wired.
type FleetMigrator interface {
	PrepareMigration(name string, to claudia.Provider, force bool) (handover.Pending, error)
	CompleteThinBrief(p handover.Pending) (handover.Pending, error)
	SeedSuccessor(name string) (handover.Pending, bool, error)
	Launch(t *thread.Thread) error
	PinModel(name, model string) error
}

// SetFleetMigrator wires the fleet migrate capability for
// POST /api/agents/migrate.
func (s *Server) SetFleetMigrator(m FleetMigrator) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fleetMigrator = m
}

// providerModelCatalog lists the model ids the menu offers per provider,
// best first (🎯T285.2). Curated, not exhaustive: ids the provider CLI
// accepts today. Models observed running in the fleet are merged in after
// the catalog, so a new id reaches the menu without a rebuild being the
// only road. A provider with no catalog and no observation still gets a
// row — its only entry is the provider default (empty model id).
var providerModelCatalog = map[string][]string{
	"claude": {"claude-fable-5", "claude-opus-5", "claude-sonnet-5", "claude-haiku-4-5"},
	"grok":   {"grok-4.5", "grok-4"},
}

// migrateProviderOption is one provider row of the menu payload.
type migrateProviderOption struct {
	Provider string `json:"provider"`
	// Band / Reason / Eligible are the server-computed weekly classification
	// (planusage.WeeklyBandDetail). The tree's "!" mark maps band→colour;
	// the menu shows Reason on rows that are visible but not choosable.
	Band     planusage.WeeklyBand `json:"band"`
	Reason   string               `json:"reason"`
	Eligible bool                 `json:"eligible"`
	// Models are the offerable ids, best first. May be empty: the menu then
	// offers the provider default alone.
	Models      []string `json:"models"`
	FleetAgents int      `json:"fleet_agents,omitempty"`
}

// migrateOptions assembles the payload from the plan-usage snapshot, the
// live registry (a provider with running seats is listed even when it
// publishes nothing — acceptance clause 3), and the model catalog.
func (s *Server) migrateOptions(now time.Time) []migrateProviderOption {
	s.mu.RLock()
	src := s.planUsageSource
	reg := s.registry
	s.mu.RUnlock()

	th := planusage.DefaultThresholds()
	var snap planusage.Snapshot
	if src != nil {
		switch v := src().(type) {
		case planusage.Snapshot:
			snap = v
		case *planusage.Snapshot:
			if v != nil {
				snap = *v
			}
		}
	}
	view := planusage.CockpitSnapshot(snap)

	// Running models per provider (registry truth), for catalog merge and
	// for listing seat-bearing providers the snapshot does not know.
	running := map[string][]string{}
	if reg != nil {
		for _, def := range reg.List() {
			p := strings.ToLower(strings.TrimSpace(string(def.Provider)))
			if p == "" {
				continue
			}
			if m := strings.TrimSpace(def.Model); m != "" {
				running[p] = append(running[p], m)
			} else if _, seen := running[p]; !seen {
				running[p] = nil
			}
		}
	}

	seen := map[string]bool{}
	var out []migrateProviderOption
	add := func(provider string, be planusage.Backend, published bool) {
		p := strings.ToLower(strings.TrimSpace(provider))
		if p == "" || seen[p] {
			return
		}
		seen[p] = true
		info := planusage.BandInfo{
			Band:   planusage.BandUnpublished,
			Reason: planusage.WeeklyBandDetail(planusage.Backend{Provider: p}, now, th).Reason,
		}
		if published {
			info = planusage.WeeklyBandDetail(be, now, th)
		}
		out = append(out, migrateProviderOption{
			Provider:    p,
			Band:        info.Band,
			Reason:      info.Reason,
			Eligible:    info.Eligible,
			Models:      mergeModels(providerModelCatalog[p], running[p]),
			FleetAgents: be.FleetAgents,
		})
	}
	for _, be := range view.Backends {
		add(be.Provider, be, true)
	}
	// Providers with running seats but no snapshot row (acceptance 3).
	var extras []string
	for p := range running {
		if !seen[p] {
			extras = append(extras, p)
		}
	}
	sort.Strings(extras)
	for _, p := range extras {
		add(p, planusage.Backend{Provider: p}, false)
	}
	return out
}

// mergeModels is catalog order first (best first), then observed running
// models not already listed, in first-seen order.
func mergeModels(catalog, observed []string) []string {
	out := append([]string(nil), catalog...)
	have := map[string]bool{}
	for _, m := range out {
		have[strings.ToLower(m)] = true
	}
	for _, m := range observed {
		k := strings.ToLower(strings.TrimSpace(m))
		if k == "" || have[k] {
			continue
		}
		have[k] = true
		out = append(out, strings.TrimSpace(m))
	}
	return out
}

// handleMigrateOptions is GET /api/migrate/options.
func (s *Server) handleMigrateOptions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"providers": s.migrateOptions(time.Now()),
	})
}

// handleAgentMigrateHTTP is POST /api/agents/migrate — the thin HTTP
// wrapper over the fleet migrate path (🎯T285.2), for every seat except
// the overseer (which POSTs /api/overseer/migrate; chat re-attach lives
// there). Body: {"name":…, "provider":…, "model":…, "force":…}. A
// same-provider call with a model is a model pin (stop + relaunch with
// the pin, same session); cross-provider carries the model into the
// successor's launch as its pin.
func (s *Server) handleAgentMigrateHTTP(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name     string `json:"name"`
		Provider string `json:"provider"`
		Model    string `json:"model"`
		Force    bool   `json:"force"`
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid JSON body"}`, http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(body.Name)
	provider := strings.ToLower(strings.TrimSpace(body.Provider))
	model := strings.TrimSpace(body.Model)
	if name == "" || provider == "" {
		http.Error(w, `{"error":"name and provider are required"}`, http.StatusBadRequest)
		return
	}

	s.mu.RLock()
	mig := s.fleetMigrator
	reg := s.registry
	overseer := s.overseerName
	s.mu.RUnlock()
	if mig == nil {
		http.Error(w, `{"error":"fleet migration is not wired"}`, http.StatusServiceUnavailable)
		return
	}
	// The overseer's rotation must re-attach owner chat — only the overseer
	// endpoint does that. Refuse here rather than strand the conversation.
	isOverseer := name == overseer
	if reg != nil {
		if def := reg.Def(name); def != nil && def.Purpose == claudia.PurposeOverseer {
			isOverseer = true
		}
	}
	if isOverseer {
		writeJSONError(w, http.StatusConflict,
			"\""+name+"\" is the overseer — POST /api/overseer/migrate instead (chat re-attach lives there)")
		return
	}

	// Same-provider + model → pin, not a migrate (the fleet refuses
	// same-provider rotation; a pin is a relaunch of the same session).
	if reg != nil {
		if def := reg.Def(name); def != nil &&
			strings.EqualFold(strings.TrimSpace(string(def.Provider)), provider) {
			if model == "" {
				writeJSONError(w, http.StatusConflict, name+" is already on "+provider)
				return
			}
			if err := mig.PinModel(name, model); err != nil {
				writeJSONError(w, http.StatusConflict, err.Error())
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true, "name": name, "provider": provider, "model": model,
				"kind": "pin",
			})
			return
		}
	}

	pending, err := mig.PrepareMigration(name, claudia.Provider(provider), body.Force)
	if err != nil {
		writeJSONError(w, http.StatusConflict, err.Error())
		return
	}
	pending, err = mig.CompleteThinBrief(pending)
	if err != nil {
		writeJSONError(w, http.StatusConflict, err.Error())
		return
	}
	// Row already rotated: a launch failure must stay legible, not read as
	// "nothing happened" (same shape as jevons_agent_migrate).
	if err := mig.Launch(&thread.Thread{ID: name, Model: model}); err != nil {
		writeJSONError(w, http.StatusConflict,
			name+" is now registered on "+pending.To+" but did not start: "+err.Error()+
				" — its handover is saved; the next launch delivers it")
		return
	}
	seeded, ok, serr := mig.SeedSuccessor(name)
	resp := map[string]any{
		"ok": true, "name": name, "provider": provider, "kind": "migrate",
		"from": pending.From, "to": pending.To,
	}
	if model != "" {
		resp["model"] = model
	}
	switch {
	case serr != nil:
		resp["handover"] = "not delivered: " + serr.Error() + " (stays pending)"
	case !ok:
		resp["handover"] = "cold — no predecessor transcript handed over"
	default:
		resp["handover"] = seeded.BriefSource
	}
	_ = json.NewEncoder(w).Encode(resp)
	s.NotifyAgentsChanged()
}

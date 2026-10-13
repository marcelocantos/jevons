// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/planusage"
	"github.com/marcelocantos/jevons/internal/thread"
)

// recoverySwitcher switches the seat's row to the destination plan as the
// fleet would, and counts what the sweep asked of it.
type recoverySwitcher struct {
	registry  *claudia.Registry
	prepared  int
	launched  []string
	launchErr error
}

func (m *recoverySwitcher) SwitchModel(name string, to claudia.Provider, model string, force bool) (claudia.SeatSwitch, error) {
	m.prepared++
	if force {
		return claudia.SeatSwitch{}, errors.New("unexpected interruption")
	}
	def := *m.registry.Def(name)
	from := def.Provider
	def.Provider, def.Model = to, model
	if err := m.registry.Register(def); err != nil {
		return claudia.SeatSwitch{}, err
	}
	// A policy-parked seat is not running: its next start resumes there.
	return claudia.SeatSwitch{Changed: true, From: from, To: to, Model: model}, nil
}

func (m *recoverySwitcher) Launch(t *thread.Thread) error {
	if m.launchErr != nil {
		return m.launchErr
	}
	m.launched = append(m.launched, t.ID)
	return nil
}

func recoveryFixture(t *testing.T) (*Server, *recoverySwitcher, *planusage.Snapshot) {
	t.Helper()
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{Name: "worker", SessionID: "original", Provider: claudia.ProviderClaude, Purpose: claudia.PurposeWork}); err != nil {
		t.Fatal(err)
	}
	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	store, err := fleetintent.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.SetFleetIntentStore(store)
	m := &recoverySwitcher{registry: reg}
	s.SetModelSwitcher(m)
	snap := &planusage.Snapshot{At: time.Now()}
	s.SetPlanUsageSource(func() planusage.Snapshot { return *snap })
	return s, m, snap
}

// Journey exception: this regression is deterministic placement and durable
// intent across two sweeps. Injected readings cover empty/tied usage and exact
// transitions without changing real subscription usage or parking real seats.
// The switch and launch are faked; these tests exercise the real
// Claudia Resolve, registry and intent store through SweepPlanPolicy.
func TestT1001PolicyParkRecoversOnNextDecisiveSweep(t *testing.T) {
	for _, initial := range []string{"empty", "tied"} {
		for _, destination := range []string{"other", "cooled-source", "missing-source", "same-provider"} {
			{
				t.Run(initial+"/"+destination, func(t *testing.T) {
					s, m, snap := recoveryFixture(t)
					if initial == "empty" {
						// A persisted park from an earlier unresolved placement.
						s.MarkAgentParked("worker", planPolicyActor, "resolve: token-tied models; set PreferProvider")
					} else {
						snap.Backends = []planusage.Backend{t39015Weekly("claude", 6, 94, snap.At), t39015Weekly("codex", 50, 50, snap.At), t39015Weekly("grok", 50, 50, snap.At)}
					}
					first := s.SweepPlanPolicy()
					if len(first) != 1 || first[0].Execution != "parked" || m.prepared != 0 || len(m.launched) != 0 {
						t.Fatalf("unresolved placement must stay parked: %+v", first)
					}
					if initial == "tied" && !strings.Contains(first[0].Reason, "token-tied") {
						t.Fatalf("not a real tie: %+v", first)
					}
					if rec := s.fleetIntent().Agents["worker"]; rec.State != fleetintent.Parked || rec.By != planPolicyActor {
						t.Fatalf("missing policy park: %+v", rec)
					}
					// Reopen the intent store: recovery must also survive restart.
					reopened, err := fleetintent.Open(filepath.Dir(filepath.Dir(s.intentStore().Path())))
					if err != nil {
						t.Fatal(err)
					}
					s.SetFleetIntentStore(reopened)
					// Fleet-wide working alone does not lift the per-seat park.
					if err := s.SetFleetIntent(fleetintent.Working, "owner", "fleet working"); err != nil {
						t.Fatal(err)
					}
					claude, codex := 6.0, 54.0 // Codex is OK, Claude is hot.
					if destination == "cooled-source" {
						claude, codex = 50, 80
					}
					if destination == "same-provider" {
						claude, codex = 80, 50
					}
					snap.Backends = []planusage.Backend{t39015Weekly("codex", codex, 100-codex, snap.At)}
					if destination != "missing-source" {
						snap.Backends = append(snap.Backends, t39015Weekly("claude", claude, 100-claude, snap.At))
					}
					acts := s.SweepPlanPolicy()
					want := "codex"
					if destination == "same-provider" {
						want = "claude"
					}
					if len(acts) != 1 || acts[0].To != want || acts[0].Failure != "" {
						t.Fatalf("next sweep did not resolve %s: %+v", want, acts)
					}
					if acts[0].Execution != "switched" && acts[0].Execution != "resumed" {
						t.Fatalf("recovery not executed: %+v", acts)
					}
					if rec := s.fleetIntent().Agents["worker"]; rec.State != fleetintent.Working {
						t.Fatalf("recovered seat still held: %+v", rec)
					}
					if got := string(m.registry.Def("worker").Provider); got != want {
						t.Fatalf("provider %s, want %s", got, want)
					}
					if destination == "same-provider" && (m.prepared != 0 || len(m.launched) != 1) {
						t.Fatalf("same-provider resume switched: %+v", m)
					}
					if got := s.SweepPlanPolicy(); len(got) != 0 {
						t.Fatalf("recovery repeated: %+v", got)
					}
				})
			}
		}
	}
}

func TestT1001HotClaudeSwitchesWithoutPin(t *testing.T) {
	s, m, snap := recoveryFixture(t)
	snap.Backends = []planusage.Backend{t39015Weekly("claude", 6, 94, snap.At), t39015Weekly("codex", 54, 46, snap.At)}
	acts := s.SweepPlanPolicy()
	if len(acts) != 1 || acts[0].To != "codex" || acts[0].Execution != "switched" || m.prepared != 1 {
		t.Fatalf("decisive destination required a pin: %+v", acts)
	}
}

func TestT1001PreservesDeliberateHolds(t *testing.T) {
	for _, held := range []string{"owner", "overseer", "fleet", "owner-cold-reason"} {
		t.Run(held, func(t *testing.T) {
			s, m, snap := recoveryFixture(t)
			snap.Backends = []planusage.Backend{t39015Weekly("claude", 6, 94, snap.At), t39015Weekly("codex", 80, 20, snap.At)}
			s.MarkAgentParked("worker", planPolicyActor, "no eligible destination")
			if strings.Contains(held, "fleet") {
				if err := s.SetFleetIntent(fleetintent.Parked, "owner", "hold fleet"); err != nil {
					t.Fatal(err)
				}
			} else {
				by := "owner"
				if held == "overseer" {
					by = "jevons"
				}
				s.MarkAgentParked("worker", by, "prepare returned COLD (owner copied diagnostic)")
			}
			before := s.fleetIntent().Agents["worker"]
			acts := s.SweepPlanPolicy()
			if m.prepared != 0 || len(m.launched) != 0 {
				t.Fatalf("deliberate hold acted on: %+v", acts)
			}
			if after := s.fleetIntent().Agents["worker"]; after != before {
				t.Fatalf("hold overwritten: before=%+v after=%+v", before, after)
			}
		})
	}
}

func TestT1001FailedResumeRemainsRecoverable(t *testing.T) {
	s, m, snap := recoveryFixture(t)
	s.MarkAgentParked("worker", planPolicyActor, "no eligible destination")
	snap.Backends = []planusage.Backend{t39015Weekly("claude", 80, 20, snap.At)}
	m.launchErr = errors.New("launch unavailable")
	if acts := s.SweepPlanPolicy(); len(acts) != 1 || acts[0].Execution != "pending" {
		t.Fatalf("failed resume hidden: %+v", acts)
	}
	if rec := s.fleetIntent().Agents["worker"]; rec.State != fleetintent.Parked {
		t.Fatalf("failure lifted intent: %+v", rec)
	}
	m.launchErr = nil
	if acts := s.SweepPlanPolicy(); len(acts) != 1 || acts[0].Execution != "resumed" {
		t.Fatalf("resume did not retry: %+v", acts)
	}
}

func TestT1001RecoveryHonorsProviderConstraints(t *testing.T) {
	for _, constraint := range []string{"exclude", "allow-none", "allow-only", "prefer", "cap"} {
		t.Run(constraint, func(t *testing.T) {
			s, _, snap := recoveryFixture(t)
			s.MarkAgentParked("worker", planPolicyActor, "resolve: token-tied")
			snap.Backends = []planusage.Backend{t39015Weekly("claude", 6, 94, snap.At), t39015Weekly("codex", 80, 20, snap.At), t39015Weekly("grok", 50, 50, snap.At)}
			plans, err := claudia.OpenSeatPolicyStore("")
			if err != nil {
				t.Fatal(err)
			}
			st := claudia.SeatPolicy{}
			switch constraint {
			case "exclude":
				st.ExcludeProviders = []claudia.Provider{claudia.ProviderCodex}
			case "allow-none":
				st.AllowNone = true
			case "allow-only":
				st.AllowedProviders = []claudia.Provider{claudia.ProviderGrok}
			case "prefer":
				st.PreferProvider = claudia.ProviderGrok
			case "cap":
				s.SetProviderSoftCaps(map[string]int{"codex": 1})
				// Registered stopped seats count against the same cap as running
				// seats; do not create real processes to fill the fixture.
				if err := s.registry.Register(claudia.AgentDef{Name: "occupied", SessionID: "occupied", Provider: claudia.ProviderCodex, Purpose: claudia.PurposeAside}); err != nil {
					t.Fatal(err)
				}
			}
			if err := plans.Put("worker", st); err != nil {
				t.Fatal(err)
			}
			s.SetSeatPlan(plans)
			acts := s.SweepPlanPolicy()
			if len(acts) != 1 {
				t.Fatalf("actions: %+v", acts)
			}
			if constraint == "allow-none" {
				if acts[0].Execution != "parked" || s.fleetIntent().AgentState("worker") != fleetintent.Parked {
					t.Fatalf("allow-none lifted: %+v", acts)
				}
			} else if acts[0].To != "grok" || acts[0].Execution != "switched" {
				t.Fatalf("constraint ignored: %+v", acts)
			}
		})
	}
}

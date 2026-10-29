// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/marcelocantos/claudia"
)

// 🎯T617: model pins must use the live in-place switch when the provider
// supports it, so a seat whose session id would not load on relaunch can
// still change model without being stopped first.

func TestT617PinLiveSeatUsesInPlaceSwitchNotStop(t *testing.T) {
	var stops int
	var switched []string
	f, reg := switchFixture(t, &claudia.StubAgentOps{
		Stop: func() { stops++ },
		SwitchModel: func(p claudia.Provider, m string) error {
			switched = append(switched, string(p)+"/"+m)
			return nil
		},
	})
	proc, err := reg.Launch("jevons-po")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reg.Stop("jevons-po") })
	if err := proc.WaitReady(context.Background()); err != nil {
		t.Fatal(err)
	}
	stops = 0
	if err := f.PinModel("jevons-po", "gpt-5.6"); err != nil {
		t.Fatal(err)
	}
	if stops != 0 {
		t.Fatalf("Stop was called %d times during a capable live pin", stops)
	}
	if len(switched) != 1 || switched[0] != "openai-codex/gpt-5.6" {
		t.Fatalf("in-place switch = %v, want one call to openai-codex/gpt-5.6", switched)
	}
	if !reg.Get("jevons-po").Alive() || reg.Def("jevons-po").Model != "gpt-5.6" {
		t.Fatalf("seat not running on the new model: alive=%v model=%q", reg.Get("jevons-po").Alive(), reg.Def("jevons-po").Model)
	}
}

func TestT617PinUnloadableSessionStillRunsAfterInPlaceSwitch(t *testing.T) {
	const badModel = "gpt-5.6"
	f, reg := switchFixture(t, &claudia.StubAgentOps{
		SwitchModel: func(claudia.Provider, string) error { return nil },
	})
	reg.SetLaunchers(&claudia.RegistryLaunchers{
		Adopt: func(claudia.Config) (*claudia.Agent, error) { return nil, claudia.ErrNoSessionWindow },
		Start: func(ctx context.Context, cfg claudia.Config) (*claudia.Agent, error) {
			if cfg.Model == badModel {
				return nil, errors.New("acp session/load 01a05c6d: Path not found")
			}
			return claudia.StartStub(ctx, cfg, &claudia.StubAgentOps{
				SwitchModel: func(claudia.Provider, string) error { return nil },
			})
		},
	})
	proc, err := reg.Launch("jevons-po")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reg.Stop("jevons-po") })
	if err := proc.WaitReady(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := f.PinModel("jevons-po", badModel); err != nil {
		t.Fatalf("pin should succeed without relaunch: %v", err)
	}
	if !reg.Get("jevons-po").Alive() {
		t.Fatal("seat was stopped by a path that should have switched in place")
	}
	if reg.Def("jevons-po").Model != badModel {
		t.Fatalf("model = %q, want %q", reg.Def("jevons-po").Model, badModel)
	}
}

func TestT617UnsupportedProviderFallsBackToRelaunch(t *testing.T) {
	f, reg := switchFixture(t, nil)
	proc, err := reg.Launch("jevons-po")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reg.Stop("jevons-po") })
	if err := proc.WaitReady(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := f.PinModel("jevons-po", "gpt-5.6"); err != nil {
		t.Fatal(err)
	}
	if reg.Def("jevons-po").Model != "gpt-5.6" || !reg.Get("jevons-po").Alive() {
		t.Fatalf("fallback relaunch did not land: model=%q alive=%v", reg.Def("jevons-po").Model, reg.Get("jevons-po").Alive())
	}
}

func TestT617FailedRelaunchRestoresSeatAndRegistryModel(t *testing.T) {
	const badModel = "gpt-5.6"
	t.Setenv("CLAUDIA_NO_BROKER", "1")
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg.SetDirect(true)
	reg.SetLaunchers(&claudia.RegistryLaunchers{
		Adopt: func(claudia.Config) (*claudia.Agent, error) { return nil, claudia.ErrNoSessionWindow },
		Start: func(ctx context.Context, cfg claudia.Config) (*claudia.Agent, error) {
			if cfg.Model == badModel {
				return nil, errors.New("acp session/load: Path not found")
			}
			return claudia.StartStub(ctx, cfg, nil)
		},
	})
	if err := reg.Register(claudia.AgentDef{
		Name: "jevons-po", WorkDir: t.TempDir(), SessionID: "s-po", Materialized: true,
		Provider: "openai-codex", Model: "gpt-5.5", Purpose: claudia.PurposeWork,
	}); err != nil {
		t.Fatal(err)
	}
	f := NewClaudia(reg)
	proc, err := reg.Launch("jevons-po")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reg.Stop("jevons-po") })
	if err := proc.WaitReady(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := f.PinModel("jevons-po", badModel); err == nil {
		t.Fatal("want relaunch failure on the fallback path")
	}
	if reg.Def("jevons-po").Model != "gpt-5.5" {
		t.Fatalf("registry model = %q after failed pin, want the original gpt-5.5", reg.Def("jevons-po").Model)
	}
	if !reg.Get("jevons-po").Alive() {
		t.Fatal("seat should be running again after restore")
	}
}

func TestT617SupportedSwitchFailureDoesNotStopSeat(t *testing.T) {
	f, reg := switchFixture(t, &claudia.StubAgentOps{
		SwitchModel: func(claudia.Provider, string) error {
			return errors.New("acp session/load: Path not found")
		},
	})
	proc, err := reg.Launch("jevons-po")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reg.Stop("jevons-po") })
	if err := proc.WaitReady(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := f.PinModel("jevons-po", "gpt-5.6"); err == nil {
		t.Fatal("want the in-place failure to surface")
	}
	if reg.Def("jevons-po").Model != "gpt-5.5" {
		t.Fatalf("registry drifted to %q after a failed in-place switch", reg.Def("jevons-po").Model)
	}
	if !reg.Get("jevons-po").Alive() {
		t.Fatal("a supported switch that failed must not stop the seat")
	}
}

func TestT617OnlyCapabilityErrorFallsBack(t *testing.T) {
	capErr := &claudia.CapabilityError{}
	if !isUnsupportedCapability(capErr) {
		t.Fatal("CapabilityError must read as unsupported")
	}
	if !isUnsupportedCapability(fmt.Errorf("wrapped: %w", capErr)) {
		t.Fatal("wrapped CapabilityError must still read as unsupported")
	}
	if isUnsupportedCapability(errors.New("acp session/load: Path not found")) {
		t.Fatal("a genuine failure must not fall through to stop-then-relaunch")
	}
}

func TestT617PinStoppedSeatNeverStopsOrRelaunches(t *testing.T) {
	var mu sync.Mutex
	var launches int
	f, reg := switchFixture(t, nil)
	reg.SetLaunchers(&claudia.RegistryLaunchers{
		Adopt: func(claudia.Config) (*claudia.Agent, error) { return nil, claudia.ErrNoSessionWindow },
		Start: func(ctx context.Context, cfg claudia.Config) (*claudia.Agent, error) {
			mu.Lock()
			launches++
			mu.Unlock()
			return claudia.StartStub(ctx, cfg, nil)
		},
	})
	if err := f.PinModel("jevons-po", "gpt-5.6"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	got := launches
	mu.Unlock()
	if got != 0 {
		t.Fatalf("stopped pin launched %d times, want a registry rewrite only", got)
	}
	if reg.Def("jevons-po").Model != "gpt-5.6" {
		t.Fatalf("def = %+v", reg.Def("jevons-po"))
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package upgrade

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
)

func TestCursorRestartStartsFreshWithoutLoad(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "agents.json")
	reg, err := claudia.NewRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	reg.SetDirect(true)
	if err := reg.Register(claudia.AgentDef{
		Name: "jv-t540.7.1.1-main-view-order", WorkDir: t.TempDir(),
		SessionID: "9c366ad6-d042-4e17-ae88-f741acc26452",
		Provider:  claudia.ProviderCursor, Materialized: true,
		AutoStart: true, TermLogPath: "-",
	}); err != nil {
		t.Fatal(err)
	}
	const oldSession = "9c366ad6-d042-4e17-ae88-f741acc26452"
	starts := 0
	reg.SetLaunchers(&claudia.RegistryLaunchers{
		Start: func(ctx context.Context, cfg claudia.Config) (*claudia.Agent, error) {
			starts++
			if cfg.SessionID == oldSession || cfg.RequireResume {
				t.Errorf("restart called session/load on %s requireResume=%v", cfg.SessionID, cfg.RequireResume)
			}
			return claudia.StartStub(ctx, cfg, nil)
		},
	})
	agent, err := adoptOrLaunchRetryingHeld(context.Background(), reg, "jv-t540.7.1.1-main-view-order")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reg.Stop("jv-t540.7.1.1-main-view-order") })
	if agent == nil || !agent.Alive() {
		t.Fatal("fresh restart did not leave a live process")
	}
	if got := reg.Def("jv-t540.7.1.1-main-view-order").SessionID; got == oldSession || got == "" {
		t.Fatal("restart kept the stored cursor session")
	}
	if starts != 1 {
		t.Fatalf("starts=%d, want one fresh start and no resume attempt", starts)
	}
}

func TestBrokerHeldCursorMigrationKeepsDestinationSessionOnRestart(t *testing.T) {
	t.Setenv("CLAUDIA_NO_BROKER", "1")
	prev := brokerMayOwnSeats
	brokerMayOwnSeats = func() bool { return true }
	t.Cleanup(func() { brokerMayOwnSeats = prev })
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg.SetDirect(true)
	const destinationSession = "cursor-migrated-destination"
	if err := reg.Register(claudia.AgentDef{
		Name: "worker", Provider: claudia.ProviderCursor, SessionID: destinationSession,
		Model: "composer-2.5", WorkDir: t.TempDir(), AutoStart: true,
	}); err != nil {
		t.Fatal(err)
	}
	reg.SetLaunchers(&claudia.RegistryLaunchers{Start: func(ctx context.Context, cfg claudia.Config) (*claudia.Agent, error) {
		if cfg.SessionID != destinationSession || !cfg.AdoptOnly {
			t.Fatalf("broker destination was reminted before adoption: %+v", cfg)
		}
		return claudia.StartStub(ctx, cfg, nil)
	}})
	if _, err := adoptOrLaunchRetryingHeld(context.Background(), reg, "worker"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reg.Stop("worker") })
	if got := reg.Def("worker"); got == nil || got.SessionID != destinationSession {
		t.Fatalf("restart changed the broker destination: %+v", got)
	}
}

func TestCursorBootStartRemembersCLIModel(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".cursor"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".cursor", "cli-config.json"), []byte(`{"model":{"modelId":"claude-opus-5"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg.SetDirect(true)
	const oldSession = "11111111-2222-3333-4444-555555555555"
	if err := reg.Register(claudia.AgentDef{
		Name: "jevons-po", WorkDir: t.TempDir(), SessionID: oldSession,
		Provider: claudia.ProviderCursor, Materialized: true, AutoStart: true, TermLogPath: "-",
	}); err != nil {
		t.Fatal(err)
	}
	reg.SetLaunchers(&claudia.RegistryLaunchers{
		Start: func(ctx context.Context, cfg claudia.Config) (*claudia.Agent, error) {
			return claudia.StartStub(ctx, cfg, nil)
		},
	})
	if _, err := adoptOrLaunchRetryingHeld(context.Background(), reg, "jevons-po"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reg.Stop("jevons-po") })
	def := reg.Def("jevons-po")
	if def == nil || def.Model != "" {
		t.Fatal("boot start pinned AgentDef.Model")
	}
	if def.SessionID == "" || def.SessionID == oldSession {
		t.Fatalf("session=%q, want a fresh id", def.SessionID)
	}
	// StartStub has no OS pid, so this process records nothing. The
	// write when the def's connect pid is still 0 is TestRecordAgentUsesProcessPID.
	src, err := os.ReadFile("reattach.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "look.RecordAgent(reg, name, pid)") {
		t.Fatal("boot adopt/launch does not record the CLI model")
	}
}

func TestResumeDeniedRemintSkipsHeldStore(t *testing.T) {
	def := &claudia.AgentDef{Name: "jevons-po", Provider: claudia.ProviderCursor, SessionID: "sid-po"}
	held := fmt.Errorf("leftover cursor-agent [424242] still holds store for session sid-po: %w", claudia.ErrCursorResumeDenied)
	if resumeDeniedRemint(def, held) {
		t.Fatal("a held store authorized a remint")
	}
	denied := fmt.Errorf("acp session/load sid-po: Invalid params (%w)", claudia.ErrCursorResumeDenied)
	if !resumeDeniedRemint(def, denied) {
		t.Fatal("a cursor resume refusal did not remint")
	}
	grok := &claudia.AgentDef{Name: "jevons", Provider: claudia.ProviderGrok, SessionID: "g"}
	if resumeDeniedRemint(grok, denied) {
		t.Fatal("Grok load failure authorized a remint")
	}
	if !resumeDeniedRemint(grok, fmt.Errorf("exclusive GROK_HOME unavailable")) {
		t.Fatal("unpublished Grok home did not remint")
	}
}

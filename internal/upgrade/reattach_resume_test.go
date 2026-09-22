// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package upgrade

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"
)

func TestAutoStartRemintsCursorResumeRefusal(t *testing.T) {
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
	starts := 0
	reg.SetLaunchers(&claudia.RegistryLaunchers{
		Start: func(ctx context.Context, cfg claudia.Config) (*claudia.Agent, error) {
			starts++
			if cfg.RequireResume {
				return nil, fmt.Errorf("acp session/load %s: Invalid params (%w)", cfg.SessionID, claudia.ErrCursorResumeDenied)
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
		t.Fatal("remint did not leave a live process")
	}
	if got := reg.Def("jv-t540.7.1.1-main-view-order").SessionID; got == "9c366ad6-d042-4e17-ae88-f741acc26452" {
		t.Fatal("boot auto-start repeated the refused session id")
	}
	if starts != 2 {
		t.Fatalf("starts=%d, want the refused resume and the fresh mint", starts)
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

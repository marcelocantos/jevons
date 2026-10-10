// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"
)

// The persisted def is the transport that the real registry Launch uses;
// startConfigFromDef alone is not an oracle for the running path.
func TestT1047FreshSubscriptionTransportParity(t *testing.T) {
	for _, tc := range []struct{ plan, transport claudia.Provider }{
		{claudia.ProviderClaude, "anthropic"},
		{claudia.ProviderCodex, "openai-codex"},
	} {
		for _, explicit := range []bool{false, true} {
			t.Run(string(tc.plan)+map[bool]string{true: "-explicit", false: "-omitted"}[explicit], func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "agents.json")
				reg, err := claudia.NewRegistry(path)
				if err != nil {
					t.Fatal(err)
				}
				s := New(t.TempDir(), nil, nil)
				s.SetRegistry(reg)
				s.SetDefaultProvider(string(tc.plan))
				arg := ""
				if explicit {
					arg = string(tc.plan)
				}
				def, existed, _, err := s.stitchAgentStart("transport-test", t.TempDir(), "", arg, "", "jevons-po", claudia.PurposeWork, "", "")
				if err != nil {
					t.Fatal(err)
				}
				if existed {
					t.Fatal("fresh mint reported existing")
				}
				if def.Provider != tc.transport {
					t.Fatalf("stitch provider=%q want %q", def.Provider, tc.transport)
				}
				// Reload the persisted registry, not the returned pointer.
				disk, err := claudia.NewRegistry(path)
				if err != nil {
					t.Fatal(err)
				}
				stored := disk.Def("transport-test")
				if stored == nil || stored.Provider != tc.transport {
					t.Fatalf("persisted def=%+v want %q", stored, tc.transport)
				}
				if cfg := startConfigFromDef(stored); cfg.Provider != stored.Provider {
					t.Fatalf("config=%q persisted=%q", cfg.Provider, stored.Provider)
				}
			})
		}
	}
}

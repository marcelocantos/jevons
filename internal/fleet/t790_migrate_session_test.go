// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/cli"
)

// 🎯T790 is the identity-after-remap story; 🎯T1013.5 moved the mechanics
// it guards into claudia.Registry.Migrate. The scenarios that used to
// exercise jevons' own re-derivation of the live successor's identity
// (an unreadable session id, a live read that still echoes the
// predecessor's id, a destination claudia had already committed) cannot
// recur in the current wrapper: PrepareMigrationPinned no longer
// reconstructs AgentDef from a live read at all — it forwards
// moved.Destination exactly as claudia's registry already recorded it
// after the move. What jevons still owns is the model parameter reaching
// Claudia and the ModelSwitch note that fires once the move lands, which
// these cover through the stopped-seat path (identical logic to the live
// path: both read moved.Destination.Model).

// The model parameter reaches claudia and the registry row.
func TestT790ModelIsHonoured(t *testing.T) {
	f, reg := migrateStoppedFixture(t, "019fd13d-e500-7913-b96c-981e50aa7902")
	f.SetRetainedHistory(func(string) (string, error) { return "user: continue\nassistant: ok\n", nil })
	if _, err := f.PrepareMigrationPinned("jevons-po", claudia.ProviderClaude, "claude-opus-5", false); err != nil {
		t.Fatal(err)
	}
	if m := reg.Def("jevons-po").Model; m != "claude-opus-5" {
		t.Fatalf("row model = %q, want claude-opus-5", m)
	}
}

// A migration that changes the model (or the provider the model is bound
// to) is a model switch. The note is what a later diagnosis reads.
func TestMigrationNotesTheModelSwitch(t *testing.T) {
	const oldSession = "019fd13d-e500-7913-b96c-981e50aa7903"
	f, reg := migrateStoppedFixture(t, oldSession)
	f.SetRetainedHistory(func(string) (string, error) { return "user: continue\nassistant: ok\n", nil })
	def := reg.Def("jevons-po")
	row := *def
	row.Model = "grok-4.6"
	if err := reg.Register(row); err != nil {
		t.Fatal(err)
	}
	var got *ModelSwitch
	f.SetModelSwitchHook(func(sw *ModelSwitch) { got = sw })

	if _, err := f.PrepareMigrationPinned("jevons-po", claudia.ProviderClaude, "claude-opus-5", false); err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("migration landed with no model switch note")
	}
	if got.Name != "jevons-po" || got.From != "grok-4.6" || got.To != "claude-opus-5" ||
		got.FromProvider != string(claudia.ProviderGrok) ||
		cli.PlanProvider(claudia.Provider(got.Provider)) != claudia.ProviderClaude ||
		got.How != ModelSwitchHowMigrate {
		t.Fatalf("switch = %+v", got)
	}
}

// A model change clears the previous version: an unpinned destination
// binds its own default rather than carrying the source provider's id
// forward.
func TestModelChangeDropsThePreviousVersion(t *testing.T) {
	for _, target := range []claudia.Provider{claudia.ProviderCursor, claudia.ProviderClaude} {
		t.Run(string(target), func(t *testing.T) {
			f, reg := migrateStoppedFixture(t, "019fd13d-e500-7913-b96c-981e50aa7910")
			f.SetRetainedHistory(func(string) (string, error) { return "user: continue\nassistant: ok\n", nil })
			def := reg.Def("jevons-po")
			row := *def
			row.Model = "grok-4.6"
			if err := reg.Register(row); err != nil {
				t.Fatal(err)
			}
			if _, err := f.PrepareMigrationPinned("jevons-po", target, "", false); err != nil {
				t.Fatal(err)
			}
			if m := reg.Def("jevons-po").Model; m == "grok-4.6" {
				t.Fatalf("model=%q; previous version kept across the change", m)
			}
		})
	}
}

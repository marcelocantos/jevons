// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"testing"

	"github.com/marcelocantos/claudia"
)

// 🎯T790: a live remap whose successor session id cannot be read never
// records a fallback uuid as Materialized — that row would demand a resume
// of a conversation that was never written.
func TestT790UnreadableSessionIsNotMaterialized(t *testing.T) {
	const oldSession = "019fd13d-e500-7913-b96c-981e50aa7900"
	f, _, _ := migrateFixture(t, oldSession, true)
	f.liveMigrate = func(*claudia.MigrateArgs) error { return nil }
	f.liveSession = func(string) (string, string) { return "", "" }

	pending, err := f.PrepareMigration("jevons-po", claudia.ProviderClaude, false)
	if err != nil {
		t.Fatal(err)
	}
	def := f.reg.Def("jevons-po")
	if claudia.PlanProvider(def.Provider) != claudia.ProviderClaude {
		t.Fatalf("provider=%s", def.Provider)
	}
	if def.Materialized {
		t.Fatal("unreadable live session id was recorded as Materialized")
	}
	if def.SessionID == "" || def.SessionID == oldSession {
		t.Fatalf("row needs a fresh mint id, got %q", def.SessionID)
	}
	if !pending.SessionUnread {
		t.Fatal("pending must say the session id was unread")
	}
}

// A live session id equal to the predecessor's is not a successor id.
func TestT790PredecessorSessionIsNotMaterialized(t *testing.T) {
	const oldSession = "019fd13d-e500-7913-b96c-981e50aa7901"
	f, _, _ := migrateFixture(t, oldSession, true)
	f.liveMigrate = func(*claudia.MigrateArgs) error { return nil }
	f.liveSession = func(string) (string, string) { return oldSession, "" }
	if _, err := f.PrepareMigration("jevons-po", claudia.ProviderClaude, false); err != nil {
		t.Fatal(err)
	}
	if f.reg.Def("jevons-po").Materialized {
		t.Fatal("predecessor session id recorded as Materialized")
	}
}

// The model parameter reaches claudia and the registry row.
func TestT790ModelIsHonoured(t *testing.T) {
	f, _, _ := migrateFixture(t, "019fd13d-e500-7913-b96c-981e50aa7902", true)
	var got string
	f.liveMigrate = func(a *claudia.MigrateArgs) error { got = a.Model; return nil }
	f.liveSession = func(string) (string, string) { return "live-sid", "" }
	if _, err := f.PrepareMigrationPinned("jevons-po", claudia.ProviderClaude, "claude-opus-5", false); err != nil {
		t.Fatal(err)
	}
	if got != "claude-opus-5" {
		t.Fatalf("MigrateArgs.Model=%q", got)
	}
	if m := f.reg.Def("jevons-po").Model; m == "" {
		t.Fatal("row model not recorded")
	}
}

// A migration that changes the model (or the provider the model is bound
// to) is a model switch. The note is what a later diagnosis reads; the
// slog line on this path does not carry the model.
func TestMigrationNotesTheModelSwitch(t *testing.T) {
	const oldSession = "019fd13d-e500-7913-b96c-981e50aa7903"
	f, _, _ := migrateFixture(t, oldSession, true)
	def := f.reg.Def("jevons-po")
	row := *def
	row.Model = "grok-4.6"
	if err := f.reg.Register(row); err != nil {
		t.Fatal(err)
	}
	f.liveMigrate = func(*claudia.MigrateArgs) error { return nil }
	f.liveSession = func(string) (string, string) { return "live-sid", "claude-opus-5" }
	var got *ModelSwitch
	f.SetModelSwitchHook(func(sw *ModelSwitch) { got = sw })

	if _, err := f.PrepareMigrationPinned("jevons-po", claudia.ProviderClaude, "", false); err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("migration landed with no model switch note")
	}
	if got.Name != "jevons-po" || got.From != "grok-4.6" || got.To != "claude-opus-5" ||
		got.FromProvider != string(claudia.ProviderGrok) || claudia.PlanProvider(claudia.Provider(got.Provider)) != claudia.ProviderClaude ||
		got.How != ModelSwitchHowMigrate {
		t.Fatalf("switch = %+v", got)
	}
}

// A model change clears the previous version. The live agent still
// reporting that same id is the cache, not the model the seat moved to.
// A different reported id is the successor and is kept (see
// TestMigrationNotesTheModelSwitch).
func TestModelChangeDropsThePreviousVersion(t *testing.T) {
	for _, target := range []claudia.Provider{claudia.ProviderCursor, claudia.ProviderClaude} {
		t.Run(string(target), func(t *testing.T) {
			f, _, _ := migrateFixture(t, "019fd13d-e500-7913-b96c-981e50aa7910", true)
			def := f.reg.Def("jevons-po")
			row := *def
			row.Model = "grok-4.6"
			if err := f.reg.Register(row); err != nil {
				t.Fatal(err)
			}
			f.liveMigrate = func(*claudia.MigrateArgs) error { return nil }
			f.liveSession = func(string) (string, string) { return "live-sid", "grok-4.6" }
			if _, err := f.PrepareMigrationPinned("jevons-po", target, "", false); err != nil {
				t.Fatal(err)
			}
			if m := f.reg.Def("jevons-po").Model; m != "" {
				t.Fatalf("model=%q want empty; previous version kept across the change", m)
			}
		})
	}
}

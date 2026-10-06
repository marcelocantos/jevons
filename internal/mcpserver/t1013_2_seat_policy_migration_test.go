// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"
)

// 🎯T1013.2: jevons' retired internal/seatplan.Store wrote exactly this
// JSON shape (map[string]State keyed by agent name) to <state dir>/
// seatplan.json. The migration path is: on daemon startup, open Claudia's
// own seat-policy store, then import that legacy file once. This test
// exercises the import against the literal legacy bytes, not a round-trip
// through the new store, so it catches a field-name drift between the
// retired jevons struct and claudia.SeatPolicy even if nobody runs the
// migration by hand again.
func TestT1013_2SeatPolicyStoreImportsLegacyJevonsFile(t *testing.T) {
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, "seatplan.json")
	// Byte-for-byte what the retired jevons internal/seatplan.Store wrote
	// (same field names, same JSON tags) for one seat with every kind of
	// field populated: preference, an explicit allow-list, an exclusion,
	// both host consent flags, and an in-flight migration bookmark.
	legacy := `{
  "jv-legacy-full": {
    "prefer_provider": "claude",
    "allowed_providers": ["claude", "grok"],
    "exclude_providers": ["codex"],
    "host_may_interrupt": true,
    "host_never_park": true,
    "migration_seed": "bounded handover awaiting delivery",
    "migration_from": "cursor",
    "migration_from_session": "sess-123",
    "migration_pending_start": true
  },
  "jv-legacy-allow-none": {
    "allow_none": true
  }
}
`
	if err := os.WriteFile(legacyPath, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}

	store, err := claudia.OpenSeatPolicyStore(filepath.Join(dir, "seat-policy"))
	if err != nil {
		t.Fatal(err)
	}
	n, err := store.ImportLegacyFile(legacyPath)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected 2 imported seats, got %d", n)
	}

	full := store.Get("jv-legacy-full")
	if full.PreferProvider != claudia.ProviderClaude {
		t.Fatalf("prefer_provider lost: %+v", full)
	}
	if len(full.AllowedProviders) != 2 {
		t.Fatalf("allowed_providers lost: %+v", full)
	}
	if len(full.ExcludeProviders) != 1 || full.ExcludeProviders[0] != claudia.ProviderCodex {
		t.Fatalf("exclude_providers lost: %+v", full)
	}
	if !full.HostMayInterrupt || !full.HostNeverPark {
		t.Fatalf("host consent flags lost: %+v", full)
	}
	if full.MigrationSeed == "" || full.MigrationFromSession == "" || !full.MigrationPendingStart {
		t.Fatalf("migration bookmark lost: %+v", full)
	}

	allowNone := store.Get("jv-legacy-allow-none")
	providers, restricted := allowNone.Allowed()
	if !restricted || len(providers) != 0 {
		t.Fatalf("allow_none lost: %+v restricted=%v providers=%v", allowNone, restricted, providers)
	}

	// A seat Claudia already has a live opinion on is never overwritten by
	// a legacy import, even on a repeated daemon restart.
	if err := store.Put("jv-legacy-full", claudia.SeatPolicy{PreferProvider: claudia.ProviderGrok}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ImportLegacyFile(legacyPath); err != nil {
		t.Fatalf("second import: %v", err)
	}
	if got := store.Get("jv-legacy-full"); got.PreferProvider != claudia.ProviderGrok {
		t.Fatalf("legacy re-import clobbered Claudia's own policy: %+v", got)
	}
}

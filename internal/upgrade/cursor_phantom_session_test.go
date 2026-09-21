// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package upgrade

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"
)

// The overseer's row named 227ea2e6 on 2026-09-22: a fallback id minted when
// a live migrate could not read the agent's session, recorded as Materialized.
// No store was ever written for it, so the next Launch would have demanded a
// resume that cannot succeed and latched the owner's chat down.
func TestPhantomCursorSessionIsReleasedAtBoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	const written = "a51de173-8767-4a2f-ad14-c6409af5c6b7"
	const phantom = "227ea2e6-b1be-4d73-bf73-64f509807f51"
	store := claudia.CursorACPStorePath(written)
	if err := os.MkdirAll(filepath.Dir(store), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	rows := []claudia.AgentDef{
		{Name: "jevons", SessionID: phantom, Provider: claudia.ProviderCursor},
		// The controls: a session that is on disk, and a provider this does
		// not speak for.
		{Name: "mm2-worker", SessionID: written, Provider: claudia.ProviderCursor},
		{Name: "jevons-po", SessionID: phantom, Provider: claudia.ProviderCodex},
	}
	for _, d := range rows {
		d.WorkDir, d.Materialized, d.TermLogPath = t.TempDir(), true, "-"
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}

	releasePhantomCursorSessions(reg)

	if reg.Def("jevons").Materialized {
		t.Fatal("a cursor row with no store on disk still demands a resume")
	}
	if !reg.Def("mm2-worker").Materialized {
		t.Fatal("a cursor row whose store exists was released; its conversation would be replaced")
	}
	if !reg.Def("jevons-po").Materialized {
		t.Fatal("a non-cursor row was touched")
	}
}

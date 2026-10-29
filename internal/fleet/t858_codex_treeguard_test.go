// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/treeguard"
)

// 🎯T858: a codex work seat runs git commit, whose pre-commit hook invokes
// treeguard sweep. The sweep journals under ~/.jevons/treeguard/journal; when
// that path is outside the seat's writable roots the hook prints the failed-open
// line and exits 1. The grant rides CodexWorkSandboxTuning beside ~/.jevons/gates
// (🎯T598).
func TestCodexWorkSeatCanReachTheTreeguardStore(t *testing.T) {
	roots, network := CodexWorkSandboxTuning(claudia.ProviderCodex, claudia.PurposeWork, "")
	if !network {
		t.Fatal("a work seat that cannot bind loopback cannot run a journey")
	}
	want := treeguard.DefaultStoreRoot()
	if want == "" {
		t.Fatal("treeguard store root is empty; the fixture cannot mean anything")
	}
	found := false
	for _, r := range roots {
		if r == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("treeguard store %q not writable; roots=%v", want, roots)
	}
}

// Without the treeguard root, a seat that only had the gate grant reproduces
// the 2026-09-22 incident: sweep cannot create journal temps.
func TestT858ControlGateRootAloneIsInsufficient(t *testing.T) {
	roots, _ := CodexWorkSandboxTuning(claudia.ProviderCodex, claudia.PurposeWork, "")
	tg := treeguard.DefaultStoreRoot()
	for _, r := range roots {
		if r == tg {
			return
		}
	}
	t.Fatalf("expected treeguard root %q among codex writable roots %v", tg, roots)
}

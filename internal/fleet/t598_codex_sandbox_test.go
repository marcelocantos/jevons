// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/gate"
)

// 🎯T598: workspace-write alone gives a Codex seat an empty writable-root
// list and no network — enough to edit files, not enough to prove
// anything. bin/gate records under ~/.jevons/gates, outside the tree on
// purpose (🎯T386), and the journey oracles bind loopback.
func TestCodexWorkSeatCanReachTheGateStoreAndLoopback(t *testing.T) {
	roots, network := CodexWorkSandboxTuning(claudia.ProviderCodex, claudia.PurposeWork, "")
	if !network {
		t.Fatal("a work seat that cannot bind loopback cannot run a journey")
	}
	want := gate.DefaultStoreRoot()
	if want == "" {
		t.Fatal("gate store root is empty; the fixture cannot mean anything")
	}
	found := false
	for _, r := range roots {
		if r == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("gate store %q not writable; roots=%v", want, roots)
	}
}

// The tuning follows the mode: seats that get no Codex sandbox get no
// widening either. Otherwise this would quietly grant network access to
// every provider.
func TestOnlyCodexWorkSeatsAreWidened(t *testing.T) {
	cases := []struct {
		name    string
		prov    claudia.Provider
		purpose string
		role    string
	}{
		{"claude work", claudia.ProviderClaude, claudia.PurposeWork, ""},
		{"grok work", claudia.ProviderGrok, claudia.PurposeWork, ""},
		{"codex overseer", claudia.ProviderCodex, claudia.PurposeOverseer, ""},
		{"codex aside", claudia.ProviderCodex, claudia.PurposeAside, ""},
		{"codex auditor", claudia.ProviderCodex, claudia.PurposeWork, "auditor"},
	}
	for _, c := range cases {
		roots, network := CodexWorkSandboxTuning(c.prov, c.purpose, c.role)
		if len(roots) != 0 || network {
			t.Fatalf("%s was widened: roots=%v network=%v", c.name, roots, network)
		}
	}
}

// The widening rides the same decision as the mode, so a mint path cannot
// set one without the other.
func TestTuningTracksTheSandboxMode(t *testing.T) {
	for _, c := range []struct {
		prov    claudia.Provider
		purpose string
		role    string
	}{
		{claudia.ProviderCodex, claudia.PurposeWork, ""},
		{claudia.ProviderCodex, claudia.PurposeWork, "auditor"},
		{claudia.ProviderClaude, claudia.PurposeWork, ""},
		{claudia.ProviderCodex, claudia.PurposeAside, ""},
	} {
		mode := CodexWorkSandbox(c.prov, c.purpose, c.role)
		roots, network := CodexWorkSandboxTuning(c.prov, c.purpose, c.role)
		widened := len(roots) > 0 || network
		if (mode != "") != widened {
			t.Fatalf("mode=%q but widened=%v for %v/%s/%s", mode, widened, c.prov, c.purpose, c.role)
		}
	}
}

// A seat that cannot get what its mission needs is refused with a reason,
// rather than started into a first gate it can never pass — the 🎯T557.1
// shape, where the seat looked healthy and could produce no evidence.
func TestRefusalNamesTheMissingAccess(t *testing.T) {
	if why := CodexWorkSandboxRefusal(claudia.ProviderCodex, claudia.PurposeWork, ""); why != "" {
		t.Fatalf("a healthy work seat was refused: %s", why)
	}
	// Seats that were never widened are not refused either — they simply
	// are not Codex work seats.
	for _, c := range []struct {
		prov    claudia.Provider
		purpose string
	}{
		{claudia.ProviderClaude, claudia.PurposeWork},
		{claudia.ProviderCodex, claudia.PurposeOverseer},
	} {
		if why := CodexWorkSandboxRefusal(c.prov, c.purpose, ""); why != "" {
			t.Fatalf("%v/%s refused: %s", c.prov, c.purpose, why)
		}
	}
}

// The refusal must say what is missing and how it bites, or it is just
// another opaque stall.
func TestRefusalTextIsActionable(t *testing.T) {
	t.Setenv(gate.StoreDirEnv, " ")
	why := CodexWorkSandboxRefusal(claudia.ProviderCodex, claudia.PurposeWork, "")
	if why == "" {
		t.Skip("gate store root still resolves; nothing to refuse")
	}
	if !strings.Contains(why, "gate") {
		t.Fatalf("refusal does not name the missing access: %q", why)
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
)

// TestT285_2PinModelValidates: same-provider pin is a relaunch of the
// existing session, not a rotation. The error/no-op arms must not reach
// Launch (which would need a live provider in this hermetic).
func TestT285_2PinModelValidates(t *testing.T) {
	if err := (*Claudia)(nil).PinModel("x", "grok-4"); err == nil {
		t.Fatal("nil receiver: want error")
	}
	f := &Claudia{}
	if err := f.PinModel("x", "grok-4"); err == nil || !strings.Contains(err.Error(), "registry") {
		t.Fatalf("no registry: %v", err)
	}

	reg, err := claudia.NewRegistry(t.TempDir() + "/agents.json")
	if err != nil {
		t.Fatal(err)
	}
	f.reg = reg
	if err := f.PinModel("missing", "grok-4"); err == nil || !strings.Contains(err.Error(), "no agent") {
		t.Fatalf("missing agent: %v", err)
	}
	if err := f.PinModel("w", "  "); err == nil || !strings.Contains(err.Error(), "required") {
		t.Fatalf("empty model: %v", err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "w", Provider: "grok", Model: "grok-4",
		WorkDir: t.TempDir(), SessionID: "s-w", Purpose: claudia.PurposeWork,
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.PinModel("w", "grok-4"); err != nil {
		t.Fatalf("already-pinned no-op: %v", err)
	}
}

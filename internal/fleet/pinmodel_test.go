// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"errors"
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

// A landed SetModel is a model switch. The journal hook must see the seat,
// the provider, and both model ids — a same-model pin must not, or a no-op
// is later read as a move onto the model the seat was already on.
func TestPinModelNotesALandedSwitch(t *testing.T) {
	reg, err := claudia.NewRegistry(t.TempDir() + "/agents.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jevons-po", Provider: "cursor", Model: "claude-fable-5",
		WorkDir: t.TempDir(), SessionID: "s-po", Purpose: claudia.PurposeWork,
	}); err != nil {
		t.Fatal(err)
	}
	f := &Claudia{reg: reg}
	var got *ModelSwitch
	f.SetModelSwitchHook(func(sw *ModelSwitch) { got = sw })
	f.liveSetModel = func(name, model string) error {
		if name != "jevons-po" || model != "composer-2.5" {
			t.Fatalf("SetModel %s %s", name, model)
		}
		return nil
	}
	if err := f.PinModel("jevons-po", "composer-2.5"); err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("landed SetModel wrote no model switch")
	}
	if got.Name != "jevons-po" || got.Provider != "cursor" || got.FromProvider != "cursor" ||
		got.From != "claude-fable-5" || got.To != "composer-2.5" || got.How != ModelSwitchHowSetModel {
		t.Fatalf("switch = %+v", got)
	}
	if m := reg.Def("jevons-po").Model; m != "composer-2.5" {
		t.Fatalf("registry model = %q", m)
	}

	got = nil
	f.liveSetModel = func(string, string) error {
		t.Fatal("same-model pin must not call SetModel")
		return nil
	}
	if err := f.PinModel("jevons-po", "composer-2.5"); err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("same-model pin was recorded as a switch: %+v", got)
	}
}

// A SetModel that fails must not relabel the row and must not record a switch.
func TestPinModelFailureIsNotASwitch(t *testing.T) {
	reg, err := claudia.NewRegistry(t.TempDir() + "/agents.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jevons-po", Provider: "cursor", Model: "claude-fable-5",
		WorkDir: t.TempDir(), SessionID: "s-po", Purpose: claudia.PurposeWork,
	}); err != nil {
		t.Fatal(err)
	}
	f := &Claudia{reg: reg}
	f.SetModelSwitchHook(func(*ModelSwitch) {
		t.Fatal("failed SetModel was recorded as a switch")
	})
	f.liveSetModel = func(string, string) error { return errors.New("turn in flight") }
	if err := f.PinModel("jevons-po", "composer-2.5"); err == nil || !strings.Contains(err.Error(), "turn in flight") {
		t.Fatalf("err = %v", err)
	}
	if m := reg.Def("jevons-po").Model; m != "claude-fable-5" {
		t.Fatalf("registry model changed on a failed switch: %q", m)
	}
}

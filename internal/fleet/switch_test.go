// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/thread"
)

// switchFixture is a registry whose seats start as claudia stubs, with a
// stopped work seat "jevons-po" on openai-codex.
func switchFixture(t *testing.T, ops *claudia.StubAgentOps) (*Claudia, *claudia.Registry) {
	t.Helper()
	t.Setenv("CLAUDIA_NO_BROKER", "1")
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg.SetDirect(true)
	reg.SetLaunchers(&claudia.RegistryLaunchers{
		Adopt: func(claudia.Config) (*claudia.Agent, error) { return nil, claudia.ErrNoSessionWindow },
		Start: func(ctx context.Context, cfg claudia.Config) (*claudia.Agent, error) {
			if cfg.AdoptOnly {
				return nil, claudia.ErrNoSessionWindow
			}
			return claudia.StartStub(ctx, cfg, ops)
		},
	})
	if err := reg.Register(claudia.AgentDef{
		Name: "jevons-po", WorkDir: t.TempDir(), SessionID: "s-po", Materialized: true,
		Provider: "openai-codex", Model: "gpt-5.5", Purpose: claudia.PurposeWork,
		Goal: "Achieve 🎯T195", TargetID: "T195", Parent: "jevons",
	}); err != nil {
		t.Fatal(err)
	}
	return NewClaudia(reg), reg
}

// claudia 🎯T195: a stopped seat moves by rewriting its row on the same
// session, keeping who it is; its next launch resumes the conversation on
// the new model. Nothing is summarised and no successor is minted.
func TestSwitchModelRewritesAStoppedSeatOnItsSession(t *testing.T) {
	f, reg := switchFixture(t, nil)
	var notes []*ModelSwitch
	f.SetModelSwitchHook(func(sw *ModelSwitch) { notes = append(notes, sw) })

	sw, err := f.SwitchModel("jevons-po", claudia.ProviderClaude, "claude-opus-5", false)
	if err != nil {
		t.Fatal(err)
	}
	if sw.Live || !sw.Changed || sw.To != "anthropic" {
		t.Fatalf("switch = %+v, want a stopped seat moved to anthropic", sw)
	}
	def := reg.Def("jevons-po")
	if def.Provider != "anthropic" || def.Model != "claude-opus-5" || def.SessionID != "s-po" || !def.Materialized {
		t.Fatalf("def = %+v, want anthropic/claude-opus-5 resuming session s-po", def)
	}
	if def.Purpose != claudia.PurposeWork || def.Goal != "Achieve 🎯T195" || def.TargetID != "T195" || def.Parent != "jevons" {
		t.Fatalf("switch lost the seat's identity: %+v", def)
	}
	if len(notes) != 1 || notes[0].FromProvider != "openai-codex" || notes[0].Provider != "anthropic" ||
		notes[0].From != "gpt-5.5" || notes[0].To != "claude-opus-5" || notes[0].How != ModelSwitchHow {
		t.Fatalf("notes = %+v, want one landed switch", notes)
	}

	// Already there: nothing changes and nothing is recorded.
	if sw, err := f.SwitchModel("jevons-po", "anthropic", "claude-opus-5", false); err != nil || sw.Changed {
		t.Fatalf("repeat switch = %+v, %v; want unchanged", sw, err)
	}
	if len(notes) != 1 {
		t.Fatalf("a no-op switch was recorded: %+v", notes)
	}
}

// A running seat switches in place: the same process takes the new model,
// so the chat, queue and reports attached to it stay attached.
func TestSwitchModelSwitchesALiveSeatInPlace(t *testing.T) {
	var mu sync.Mutex
	var asked []string
	f, reg := switchFixture(t, &claudia.StubAgentOps{SwitchModel: func(p claudia.Provider, m string) error {
		mu.Lock()
		defer mu.Unlock()
		asked = append(asked, string(p)+"/"+m)
		return nil
	}})
	proc, err := reg.Launch("jevons-po")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reg.Stop("jevons-po") })
	if err := proc.WaitReady(context.Background()); err != nil {
		t.Fatal(err)
	}

	sw, err := f.SwitchModel("jevons-po", "claude", "claude-opus-5", false)
	if err != nil {
		t.Fatal(err)
	}
	if !sw.Live || !sw.Changed {
		t.Fatalf("switch = %+v, want a live switch", sw)
	}
	mu.Lock()
	got := append([]string(nil), asked...)
	mu.Unlock()
	if len(got) != 1 || got[0] != "anthropic/claude-opus-5" {
		t.Fatalf("seat was asked %v, want one switch to anthropic/claude-opus-5", got)
	}
	if reg.Get("jevons-po") != proc || proc.Provider() != "anthropic" || proc.Model() != "claude-opus-5" {
		t.Fatalf("the seat was replaced or not switched: %s/%s", proc.Provider(), proc.Model())
	}
	if def := reg.Def("jevons-po"); def.Provider != "anthropic" || def.Model != "claude-opus-5" || def.SessionID != "s-po" {
		t.Fatalf("def = %+v, want the switch recorded on session s-po", def)
	}
}

// A refused live switch leaves the seat on its model and records nothing.
func TestSwitchModelRefusedLiveLeavesTheSeat(t *testing.T) {
	f, reg := switchFixture(t, &claudia.StubAgentOps{SwitchModel: func(claudia.Provider, string) error {
		return errors.New("switch refused: the conversation does not fit")
	}})
	proc, err := reg.Launch("jevons-po")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reg.Stop("jevons-po") })
	if err := proc.WaitReady(context.Background()); err != nil {
		t.Fatal(err)
	}
	var notes []*ModelSwitch
	f.SetModelSwitchHook(func(sw *ModelSwitch) { notes = append(notes, sw) })
	if _, err := f.SwitchModel("jevons-po", "claude", "claude-opus-5", false); err == nil || !strings.Contains(err.Error(), "does not fit") {
		t.Fatalf("err = %v, want the seat's refusal", err)
	}
	if def := reg.Def("jevons-po"); def.Provider != "openai-codex" || def.Model != "gpt-5.5" {
		t.Fatalf("refused switch rewrote the row: %+v", def)
	}
	if len(notes) != 0 {
		t.Fatalf("refused switch was recorded: %+v", notes)
	}
}

// 🎯T763 / 🎯T1032: a switch drops, recorded, the provider-native settings
// the destination refuses, and refuses before anything changes when that
// would drop a restriction.
func TestSwitchModelReconcilesProviderSettings(t *testing.T) {
	f, reg := switchFixture(t, nil)
	src := *reg.Def("jevons-po")
	src.SandboxMode, src.SandboxGitWrite = "workspace-write", true
	src.SandboxWritableRoots = []string{t.TempDir()}
	if err := reg.Register(src); err != nil {
		t.Fatal(err)
	}
	if _, err := f.SwitchModel("jevons-po", "claude", "claude-opus-5", false); err != nil {
		t.Fatal(err)
	}
	def := reg.Def("jevons-po")
	if len(StaleProviderCaps(*def)) != 0 || def.SandboxMode != "" || def.SandboxGitWrite {
		t.Fatalf("destination still carries settings it refuses: %+v", def)
	}
	if got := CapDrop("jevons-po"); !strings.Contains(got, "(switch)") || !strings.Contains(got, "sandbox_policy") {
		t.Fatalf("CapDrop = %q, want the switch's drop recorded", got)
	}

	for _, mode := range []string{"read-only", "unknown-policy"} {
		t.Run(mode, func(t *testing.T) {
			f, reg := switchFixture(t, nil)
			src := *reg.Def("jevons-po")
			src.SandboxMode = mode
			if err := reg.Register(src); err != nil {
				t.Fatal(err)
			}
			src = *reg.Def("jevons-po")
			_, err := f.SwitchModel("jevons-po", "claude", "claude-opus-5", true)
			if !errors.Is(err, ErrProviderSwitchWouldWeaken) {
				t.Fatalf("err = %v, want a refusal to drop the restriction", err)
			}
			if !reflect.DeepEqual(*reg.Def("jevons-po"), src) {
				t.Fatalf("refused switch changed the seat: %+v", reg.Def("jevons-po"))
			}
		})
	}
}

func TestSwitchModelRefusesAnUnknownSeat(t *testing.T) {
	f, _ := switchFixture(t, nil)
	if _, err := f.SwitchModel("ghost", "claude", "claude-opus-5", false); err == nil {
		t.Fatal("switching an unregistered seat did not fail")
	}
	if _, err := (*Claudia)(nil).SwitchModel("jevons-po", "claude", "m", false); err == nil {
		t.Fatal("nil adapter: want error")
	}
}

// 🎯T285.2 / 🎯T617: a pin is a switch on the seat's own provider.
func TestPinModelSwitchesOnTheSameProvider(t *testing.T) {
	f, reg := switchFixture(t, nil)
	if err := f.PinModel("jevons-po", " "); err == nil {
		t.Fatal("empty model: want error")
	}
	if err := f.PinModel("jevons-po", "gpt-5.6"); err != nil {
		t.Fatal(err)
	}
	if def := reg.Def("jevons-po"); def.Provider != "openai-codex" || def.Model != "gpt-5.6" || def.SessionID != "s-po" {
		t.Fatalf("def = %+v, want openai-codex/gpt-5.6 on session s-po", def)
	}
}

// A genuine thread spawn with no purpose is a side chat, and minting it as
// aside is correct.
func TestThreadSpawnMintStillDefaultsToAside(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	f := NewClaudia(reg)
	f.SetDefaultProvider(claudia.ProviderGrok)

	th := &thread.Thread{ID: "att-billing", WorkDir: t.TempDir(), Parent: "jevons"}
	if err := f.ensureRegistered(th); err != nil {
		t.Fatalf("ensureRegistered mint: %v", err)
	}
	if def := reg.Def("att-billing"); def == nil || def.Purpose != claudia.PurposeAside {
		t.Fatalf("thread-path mint should be aside, got %+v", def)
	}
}

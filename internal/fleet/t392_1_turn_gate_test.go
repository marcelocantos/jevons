// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/marcelocantos/claudia"
)

const t3921Session = "019fd13d-e500-7913-b96c-981e50aa2e99"

func TestT392_1SendDefersWithoutRotatingSession(t *testing.T) {
	const name = "jv-t392.1-turn-rate"
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: filepath.Join(dir, "work"), SessionID: t3921Session,
		Provider: claudia.ProviderGrok, Materialized: true, AutoStart: false,
	}); err != nil {
		t.Fatal(err)
	}
	reg.SetDirect(true)
	reg.SetLaunchers(&claudia.RegistryLaunchers{
		Start: func(_ context.Context, _ claudia.Config) (*claudia.Agent, error) {
			return claudia.NewStubAgent(nil), nil
		},
	})
	if _, err := reg.Launch(name); err != nil {
		t.Fatalf("stub launch: %v", err)
	}
	t.Cleanup(func() { reg.Stop(name) })

	f := NewClaudia(reg)
	var gatedSession string
	f.SetTurnGate(func(agent, sessionID string) error {
		gatedSession = sessionID
		return errors.New("turn delay for " + agent + ": high burn; session unchanged")
	})

	before := reg.Def(name).SessionID
	_, err = f.Send(name, "continue")
	if err == nil {
		t.Fatal("high-burn send admitted — turn-rate gate was not consulted")
	}
	if !strings.Contains(err.Error(), "high burn") {
		t.Fatalf("send err=%v want high-burn deferral", err)
	}
	if gatedSession != t3921Session {
		t.Fatalf("gate saw session %q want %s", gatedSession, t3921Session)
	}
	after := reg.Def(name).SessionID
	if after != before || after != t3921Session {
		t.Fatalf("send reminted session: before=%s after=%s", before, after)
	}

	// Mutation aliveness: compact-or-rotate of this seat must be visible.
	mutated := uuid.NewString()
	if mutated == before {
		t.Fatal("uuid collision — mutation control is a no-op")
	}
	def := *reg.Def(name)
	def.SessionID = mutated
	if err := reg.Register(def); err != nil {
		t.Fatal(err)
	}
	if got := reg.Def(name).SessionID; got == before {
		t.Fatal("SessionID mutation was not visible — the 🎯T392.1 oracle is dead")
	}
}

func TestT392_1DeliverDefersWithoutRotatingSession(t *testing.T) {
	const name = "jv-t392.1-deliver"
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: filepath.Join(dir, "work"), SessionID: t3921Session,
		Provider: claudia.ProviderGrok, Materialized: true, AutoStart: false,
	}); err != nil {
		t.Fatal(err)
	}
	f := NewClaudia(reg)
	f.SetTurnGate(func(agent, sessionID string) error {
		return errors.New("turn pause: high burn; session " + sessionID + " unchanged")
	})
	_, err = f.Deliver(name, "continue")
	if err == nil {
		t.Fatal("high-burn deliver admitted")
	}
	if got := reg.Def(name).SessionID; got != t3921Session {
		t.Fatalf("deliver reminted session: %s", got)
	}
}

func TestT392_1NilGateAdmits(t *testing.T) {
	f := NewClaudia(nil)
	if err := f.allowTurn("anyone"); err != nil {
		t.Fatalf("nil gate must admit: %v", err)
	}
}

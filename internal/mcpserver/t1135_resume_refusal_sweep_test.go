// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/fleet"
	"github.com/marcelocantos/jevons/internal/fleetintent"
)

func TestT1135SweepRetriesStoppedSeatWithResumeRefusalCount(t *testing.T) {
	_ = fleet.UseResumeRefusalFile("")
	fleet.SetResumeRefusalEscalateAfter(3)
	t.Cleanup(func() { fleet.SetResumeRefusalEscalateAfter(0) })

	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg.SetDirect(true)
	const name = "jv-t1135-stopped"
	oldSession := "session-old"
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: t.TempDir(), SessionID: oldSession,
		Materialized: true, AutoStart: true, Purpose: claudia.PurposeWork,
	}); err != nil {
		t.Fatal(err)
	}

	refusal := errors.New("existing conversation required for this seat and session; refusing to mint a replacement session")
	var launches int
	reg.SetLaunchers(&claudia.RegistryLaunchers{Start: func(_ context.Context, cfg claudia.Config) (*claudia.Agent, error) {
		launches++
		if cfg.RequireResume && cfg.SessionID == oldSession {
			return nil, refusal
		}
		if !cfg.RequireResume {
			return claudia.StartStub(context.Background(), cfg, nil)
		}
		return nil, refusal
	}})

	if _, err := fleet.LaunchAutoRecovering(reg, name); err == nil || !strings.Contains(err.Error(), "resume refused") {
		t.Fatalf("seed refusal: %v", err)
	}
	if fleet.ResumeRefusalAttempts(name) != 1 {
		t.Fatalf("attempts=%d want 1", fleet.ResumeRefusalAttempts(name))
	}

	intent := fleetintent.Snapshot{}
	reps := recoverResumeRefusedStopped(reg, nil, "jevons", intent, nil, nil, nil)
	if len(reps) != 1 || reps[0].Name != name {
		t.Fatalf("reps=%v", reps)
	}
	if reps[0].Recovered {
		t.Fatalf("first sweep should not recover yet: %+v", reps[0])
	}
	if launches < 2 {
		t.Fatalf("launches=%d want retry on sweep", launches)
	}

	reps = recoverResumeRefusedStopped(reg, nil, "jevons", intent, nil, nil, nil)
	if len(reps) != 1 {
		t.Fatalf("reps=%v", reps)
	}
	if !reps[0].Recovered {
		t.Fatalf("third attempt should escalate to fresh session: %+v", reps[0])
	}
	after := reg.Def(name)
	if after == nil || after.SessionID == "" || after.SessionID == oldSession {
		t.Fatalf("after escalate def=%+v", after)
	}
}

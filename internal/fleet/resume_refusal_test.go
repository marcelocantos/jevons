// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
)

func TestT1135ResumeRefusalEscalatesToFreshSession(t *testing.T) {
	_ = UseResumeRefusalFile("")
	SetResumeRefusalEscalateAfter(2)
	t.Cleanup(func() { SetResumeRefusalEscalateAfter(0) })

	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg.SetDirect(true)
	const name = "jv-t1135-auto"
	oldSession := "session-old"
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: t.TempDir(), SessionID: oldSession,
		Materialized: true, AutoStart: true, Purpose: claudia.PurposeWork, TargetID: "T1135",
	}); err != nil {
		t.Fatal(err)
	}

	refusal := errors.New("existing conversation required for this seat and session; refusing to mint a replacement session")
	reg.SetLaunchers(&claudia.RegistryLaunchers{Start: func(_ context.Context, cfg claudia.Config) (*claudia.Agent, error) {
		if cfg.RequireResume && cfg.SessionID == oldSession {
			return nil, refusal
		}
		if !cfg.RequireResume {
			return claudia.StartStub(context.Background(), cfg, nil)
		}
		return nil, refusal
	}})

	_, err = LaunchAutoRecovering(reg, name)
	if err == nil || !strings.Contains(err.Error(), "resume refused") {
		t.Fatalf("first refusal = %v, want bounded resume refusal", err)
	}
	_, err = LaunchAutoRecovering(reg, name)
	if err != nil {
		t.Fatalf("second refusal should escalate: %v", err)
	}
	after := reg.Def(name)
	if after == nil || after.SessionID == "" || after.SessionID == oldSession {
		t.Fatalf("after escalate def=%+v, want new session", after)
	}
}

func TestT1135ResumeRefusalReapsWhenFreshStillRefused(t *testing.T) {
	_ = UseResumeRefusalFile("")
	SetResumeRefusalEscalateAfter(1)
	t.Cleanup(func() { SetResumeRefusalEscalateAfter(0) })

	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg.SetDirect(true)
	const name = "jv-t1135-reap"
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: t.TempDir(), SessionID: "s1", Materialized: true, AutoStart: true,
	}); err != nil {
		t.Fatal(err)
	}
	always := errors.New("omp: seat is loaded in the sidecar under another session")
	reg.SetLaunchers(&claudia.RegistryLaunchers{Start: func(_ context.Context, cfg claudia.Config) (*claudia.Agent, error) {
		return nil, always
	}})

	_, err = LaunchAutoRecovering(reg, name)
	if !errors.Is(err, ErrSeatReapedForResumeRefusal) {
		t.Fatalf("err=%v, want %v", err, ErrSeatReapedForResumeRefusal)
	}
	if reg.Def(name) != nil {
		t.Fatal("registry row remains after reap escalation")
	}
}

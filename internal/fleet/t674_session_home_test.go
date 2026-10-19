// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
)

// 🎯T674 — a seat whose exclusive provider home is gone is reminted
// automatically, not left unstartable until someone kills it by hand.
//
// Specimen (2026-09-20): jevons-po failed twice with
// "exclusive GROK_HOME unavailable for session …: no such file or directory"
// and only recovered after a hand kill+remint. Codex's equivalent is
// "thread/resume: exclusive CODEX_HOME missing at …".

func t674BounceSeat(t *testing.T, name string, provider claudia.Provider, sid string) *claudia.Registry {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agents.json")
	work := t.TempDir()
	r1, err := claudia.NewRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := r1.Register(claudia.AgentDef{
		Name: name, WorkDir: work, SessionID: sid,
		Provider: provider, Materialized: true, AutoStart: true,
		Parent: "jevons-po", Purpose: claudia.PurposeWork, TargetID: "T674",
	}); err != nil {
		t.Fatal(err)
	}
	r2, err := claudia.NewRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	r2.SetDirect(true)
	return r2
}

func TestT674GrokHomeMissingRemintsAndReports(t *testing.T) {
	const oldSID = "009fd70d-t674-grok-home-gone"
	reg := t674BounceSeat(t, "jevons-po", claudia.ProviderGrok, oldSID)
	starts := 0
	reg.SetLaunchers(&claudia.RegistryLaunchers{
		Start: func(_ context.Context, cfg claudia.Config) (*claudia.Agent, error) {
			starts++
			if cfg.RequireResume {
				return nil, fmt.Errorf("exclusive GROK_HOME unavailable for session %s: stat grok-homes/%s: no such file or directory", cfg.SessionID, cfg.SessionID)
			}
			return claudia.StartStub(context.Background(), cfg, nil)
		},
	})
	agent, lost, err := LaunchRecoveringReported(context.Background(), reg, "jevons-po")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reg.Stop("jevons-po") })
	if agent == nil {
		t.Fatal("remint returned no process")
	}
	if lost.OldSession != oldSID || lost.NewSession == "" || lost.NewSession == oldSID {
		t.Fatalf("remint report = %+v, want old=%s and a fresh new session", lost, oldSID)
	}
	desc := lost.Describe()
	if !strings.Contains(desc, oldSID) || !strings.Contains(desc, "PRIOR CONTEXT IS LOST") {
		t.Fatalf("report does not name the lost session / lost history: %s", desc)
	}
	if got := reg.Def("jevons-po").SessionID; got != lost.NewSession {
		t.Fatalf("registry session %q != reported new %q", got, lost.NewSession)
	}
	if starts != 2 {
		t.Fatalf("starts=%d, want refusal then mint", starts)
	}
}

func TestT674CodexHomeMissingRemints(t *testing.T) {
	const oldSID = "codex-t674-home-gone"
	reg := t674BounceSeat(t, "jv-t674-codex", claudia.ProviderCodex, oldSID)
	starts := 0
	reg.SetLaunchers(&claudia.RegistryLaunchers{
		Start: func(_ context.Context, cfg claudia.Config) (*claudia.Agent, error) {
			starts++
			if cfg.RequireResume {
				return nil, fmt.Errorf("thread/resume: exclusive CODEX_HOME missing at /tmp/claudia/codex-homes/%s — refusing to mint", cfg.SessionID)
			}
			return claudia.StartStub(context.Background(), cfg, nil)
		},
	})
	agent, lost, err := LaunchRecoveringReported(context.Background(), reg, "jv-t674-codex")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reg.Stop("jv-t674-codex") })
	if agent == nil || lost.OldSession != oldSID || lost.NewSession == oldSID {
		t.Fatalf("codex home missing did not remint: agent=%v lost=%+v", agent != nil, lost)
	}
	if starts != 2 {
		t.Fatalf("starts=%d, want refusal then mint", starts)
	}
}

// Intact home (resume succeeds) must not rotate — T674 acceptance 3 control.
func TestT674IntactHomeResumesUntouched(t *testing.T) {
	const sid = "intact-home-session"
	reg := t674BounceSeat(t, "intact", claudia.ProviderGrok, sid)
	starts := 0
	reg.SetLaunchers(&claudia.RegistryLaunchers{
		Start: func(_ context.Context, cfg claudia.Config) (*claudia.Agent, error) {
			starts++
			if !cfg.RequireResume {
				t.Fatal("intact Materialized seat launched without RequireResume")
			}
			return claudia.StartStub(context.Background(), cfg, nil)
		},
	})
	agent, lost, err := LaunchRecoveringReported(context.Background(), reg, "intact")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reg.Stop("intact") })
	if agent == nil {
		t.Fatal("intact home returned no process")
	}
	if lost.NewSession != "" {
		t.Fatalf("intact home reminted onto %s — history would be discarded", lost.NewSession)
	}
	if got := reg.Def("intact").SessionID; got != sid {
		t.Fatalf("session drifted to %q", got)
	}
	if starts != 1 {
		t.Fatalf("starts=%d, want a single resume", starts)
	}
}

// Provider label on the row does not gate SessionHomeMissing: the T674
// specimen had provider=claude while the stored session pointed at a Grok
// exclusive home. Matching on the declared provider would leave the seat dead.
func TestT674SessionHomeMissingIsProviderAgnostic(t *testing.T) {
	err := fmt.Errorf("exclusive GROK_HOME unavailable for session x: stat …: no such file or directory")
	claude := &claudia.AgentDef{Name: "jevons-po", Provider: claudia.ProviderClaude, SessionID: "x"}
	if !SessionHomeMissing(err) || !remintAfterResumeError(claude, err) {
		t.Fatal("GROK_HOME missing on a claude-labelled row does not remint — the specimen stays dead")
	}
	codexErr := fmt.Errorf("thread/resume: exclusive CODEX_HOME missing at /x/codex-homes/y — refusing to mint")
	if !SessionHomeMissing(codexErr) || !remintAfterResumeError(claude, codexErr) {
		t.Fatal("CODEX_HOME missing does not remint")
	}
	// A store that exists still fails closed.
	load := grokLoadErr("x")
	if SessionHomeMissing(load) || remintAfterResumeError(&claudia.AgentDef{Provider: claudia.ProviderGrok}, load) {
		t.Fatal("Grok load failure on an existing store reminted")
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"errors"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/handover"
	"github.com/marcelocantos/jevons/internal/planusage"
	"github.com/marcelocantos/jevons/internal/thread"
)

type unattachedFake struct{ err error }

func (f *unattachedFake) PrepareMigration(string, claudia.Provider, bool) (handover.Pending, error) {
	return handover.Pending{}, f.err
}
func (f *unattachedFake) CompleteThinBrief(p handover.Pending) (handover.Pending, error) { return p, nil }
func (f *unattachedFake) SeedSuccessor(string) (handover.Pending, bool, error) {
	return handover.Pending{}, false, nil
}
func (f *unattachedFake) Launch(*thread.Thread) error { return nil }

// 🎯T691: a sweep right after a daemon restart can reach a seat Claudia holds
// live before this host re-attaches its handle. Claudia refuses the stopped
// path; that is a deferral to the next sweep, not a failed migration shown
// to the owner. Any other preparation error stays a failure.
func TestPlanSweepDefersSeatLiveInClaudiaButNotAttached(t *testing.T) {
	for _, tc := range []struct {
		err       error
		execution string
	}{
		{errors.New(`migrate stopped "jevons": live handle exists; use Agent.Migrate`), "deferred"},
		{errors.New("Migrate: context transfer: migration transfer: response: agent turn failed"), "failed"},
	} {
		reg, err := claudia.NewRegistry(t.TempDir() + "/agents.json")
		if err != nil {
			t.Fatal(err)
		}
		if err := reg.Register(claudia.AgentDef{Name: "jevons", SessionID: "old", Provider: "xai-oauth", Purpose: claudia.PurposeWork}); err != nil {
			t.Fatal(err)
		}
		now := time.Date(2026, 9, 28, 9, 35, 0, 0, time.UTC)
		s := New(t.TempDir(), nil, nil)
		s.SetRegistry(reg)
		s.SetPlanUsageSource(func() planusage.Snapshot {
			return planusage.Snapshot{At: now, Backends: []planusage.Backend{
				t39015Weekly("grok", 10, 90, now),
				t39015Weekly("claude", 90, 10, now),
			}}
		})
		s.SetMigrator(&unattachedFake{err: tc.err})
		acts := s.SweepPlanPolicy()
		if len(acts) != 1 || acts[0].Execution != tc.execution {
			t.Fatalf("error %q: actions=%+v, want execution %s", tc.err, acts, tc.execution)
		}
	}
}

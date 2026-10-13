// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/planusage"
)

// 🎯T691: a sweep right after a daemon restart can reach a seat Claudia runs
// before this host re-attaches its handle. Claudia refuses to rewrite such a
// seat (claudia.ErrSeatNotAttached); that is a deferral to the next sweep,
// not a failed switch shown to the owner. Any other refusal stays a failure,
// and leaves the seat working where it was.
func TestPlanSweepDefersSeatLiveInClaudiaButNotAttached(t *testing.T) {
	for _, tc := range []struct {
		err       error
		execution string
	}{
		{fmt.Errorf("switch jevons: %w", claudia.ErrSeatNotAttached), "deferred"},
		{errors.New("switch refused: the conversation does not fit openai-codex/gpt-5.5 even after compaction"), "failed"},
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
		s.SetModelSwitcher(&fakeSwitcher{err: tc.err})
		acts := s.SweepPlanPolicy()
		if len(acts) != 1 || acts[0].Execution != tc.execution {
			t.Fatalf("error %q: actions=%+v, want execution %s", tc.err, acts, tc.execution)
		}
		if def := reg.Def("jevons"); def.Provider != "xai-oauth" || def.SessionID != "old" {
			t.Fatalf("refused switch moved the seat: %+v", def)
		}
	}
}

// A seat that was not running (a policy park lifting) switches as stopped and
// then launches on the destination, on its own session.
func TestPlanSweepLaunchesAStoppedSeatAfterItsSwitch(t *testing.T) {
	reg, err := claudia.NewRegistry(t.TempDir() + "/agents.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{Name: "worker", SessionID: "s-worker", Provider: "xai-oauth", Purpose: claudia.PurposeWork}); err != nil {
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
	sw := &fakeSwitcher{reg: reg}
	s.SetModelSwitcher(sw)
	acts := s.SweepPlanPolicy()
	if len(acts) != 1 || acts[0].Execution != "switched" {
		t.Fatalf("actions = %+v, want one switched seat", acts)
	}
	if _, launched := sw.calls(); len(launched) != 1 || launched[0] != "worker" {
		t.Fatalf("launched = %v, want the stopped seat started on its destination", launched)
	}
	if def := reg.Def("worker"); def.Provider != "anthropic" || def.SessionID != "s-worker" {
		t.Fatalf("def = %+v, want anthropic on session s-worker", def)
	}
}

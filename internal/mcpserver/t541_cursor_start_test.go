// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/mark3labs/mcp-go/mcp"
)

func TestT541CursorSeatMaterializedOracle(t *testing.T) {
	t.Parallel()
	cases := []struct {
		store, bound, want bool
	}{
		{false, false, false}, // meta-only registry row
		{true, false, false},  // store without a bound process
		{false, true, false},  // process without a conversation
		{true, true, true},
	}
	for _, tc := range cases {
		if got := CursorSeatMaterialized(tc.store, tc.bound); got != tc.want {
			t.Errorf("CursorSeatMaterialized(store=%v, bound=%v) = %v, want %v",
				tc.store, tc.bound, got, tc.want)
		}
	}
}

func TestT541DeferStartPromptIsCursorOnly(t *testing.T) {
	t.Parallel()
	if !deferStartPrompt(claudia.ProviderCursor) {
		t.Fatal("Cursor must start-then-send")
	}
	for _, p := range []claudia.Provider{
		claudia.ProviderGrok, claudia.ProviderClaude, claudia.ProviderCodex, "",
	} {
		if deferStartPrompt(p) {
			t.Errorf("provider %q must keep T305 confirm-on-start", p)
		}
	}
}

func t541Server(t *testing.T) (*Server, *claudia.Registry) {
	t.Helper()
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	return s, reg
}

func TestT541FinishCursorStartReapsMetaOnly(t *testing.T) {
	s, reg := t541Server(t)
	const name = "jv-t541-meta"
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: t.TempDir(), SessionID: "sid-meta",
		Provider: claudia.ProviderCursor, Purpose: claudia.PurposeWork,
		Parent: "jevons-po",
	}); err != nil {
		t.Fatal(err)
	}
	var submitted atomic.Bool
	s.cursorSubmit = func(n, text string) error {
		if s.startMutexHeld() {
			t.Error("submit held start mutex — the MCP hang")
		}
		if n != name || !strings.Contains(text, "Execute 🎯T541") {
			t.Errorf("submit name=%q text=%q", n, text)
		}
		submitted.Store(true)
		return nil
	}
	s.cursorObserve = func(string) (bool, bool) { return false, false }

	_, err := s.finishCursorStart(name, false, "Execute 🎯T541.")
	if err == nil {
		t.Fatal("meta-only seat must fail loud")
	}
	if !strings.Contains(err.Error(), "unmaterialized") {
		t.Fatalf("error %q should name unmaterialized", err)
	}
	if !submitted.Load() {
		t.Fatal("remint must attempt to write a conversation")
	}
	released, kept := s.startBriefFailureTeardown(name, false, err)
	if !released || kept {
		t.Fatalf("teardown released=%v kept=%v, want reap", released, kept)
	}
	if reg.Def(name) != nil {
		t.Fatal("unmaterialized seat left registered as idle-zombie")
	}
}

func TestT541FinishCursorStartMaterializes(t *testing.T) {
	s, reg := t541Server(t)
	const name = "jv-t541-ok"
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: t.TempDir(), SessionID: "sid-ok",
		Provider: claudia.ProviderCursor, Purpose: claudia.PurposeWork,
	}); err != nil {
		t.Fatal(err)
	}
	s.cursorSubmit = func(string, string) error { return nil }
	s.cursorObserve = func(string) (bool, bool) { return true, true }

	note, err := s.finishCursorStart(name, false, "Execute 🎯T541.")
	if err != nil {
		t.Fatalf("materialized seat: %v", err)
	}
	if !strings.Contains(note, "🎯T541") {
		t.Fatalf("note %q should cite T541 start-then-send", note)
	}
	if d := reg.Def(name); d == nil || !d.Materialized {
		t.Fatal("MarkMaterialized not persisted after store+bound")
	}
}

func TestT541EmptyPromptWritesRemintSeed(t *testing.T) {
	s, _ := t541Server(t)
	var got string
	s.cursorSubmit = func(_, text string) error {
		got = text
		return nil
	}
	s.cursorObserve = func(string) (bool, bool) { return true, true }
	if _, err := s.finishCursorStart("jv-t541-seed", false, ""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, cursorRemintSeed) {
		t.Fatalf("empty prompt must write remint seed, got %q", got)
	}
}

func TestT541HandleAgentStartReleasesMutexBeforePrompt(t *testing.T) {
	s, _ := t541Server(t)
	var heldDuringLaunch, heldDuringSubmit atomic.Bool
	s.launchAgentFn = func(string) (*claudia.Agent, error) {
		heldDuringLaunch.Store(s.startMutexHeld())
		return nil, nil
	}
	s.cursorSubmit = func(string, string) error {
		heldDuringSubmit.Store(s.startMutexHeld())
		return nil
	}
	s.cursorObserve = func(string) (bool, bool) { return true, true }

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"name":      "jv-t541-start",
		"workdir":   t.TempDir(),
		"provider":  string(claudia.ProviderCursor),
		"parent":   "jevons-po",
		"purpose":  "work",
		"prompt":   "Execute 🎯T541.",
	}
	res, err := s.handleAgentStart(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || res.IsError {
		t.Fatalf("start error: %v", toolText(res))
	}
	text := toolText(res)
	if strings.Contains(text, "prompt_delivered=true") {
		t.Fatal("Cursor start must not claim prompt_delivered on the start RPC")
	}
	if !heldDuringLaunch.Load() {
		t.Fatal("Launch must run under start mutex")
	}
	if heldDuringSubmit.Load() {
		t.Fatal("prompt delivery held start mutex — agent_list would time out")
	}
}

func TestT541HandleAgentStartReapsUnmaterialized(t *testing.T) {
	s, reg := t541Server(t)
	s.launchAgentFn = func(string) (*claudia.Agent, error) { return nil, nil }
	s.cursorSubmit = func(string, string) error { return nil }
	s.cursorObserve = func(string) (bool, bool) { return false, true } // process, no store

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"name":     "jv-t541-zombie",
		"workdir":  t.TempDir(),
		"provider": string(claudia.ProviderCursor),
		"parent":   "jevons-po",
		"prompt":   "Execute 🎯T541.",
	}
	res, err := s.handleAgentStart(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || !res.IsError {
		t.Fatal("unmaterialized start must fail loud")
	}
	if !strings.Contains(toolText(res), "unmaterialized") {
		t.Fatalf("error %q", toolText(res))
	}
	if reg.Def("jv-t541-zombie") != nil {
		t.Fatal("unmaterialized mint left as idle-zombie")
	}
}

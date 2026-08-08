// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/discovery"
)

// 🎯T287: the RHS fleet prefix (company icon + condensed model) needs the
// agent's provider and the model it actually ran. Provider comes from the
// registry def; the model is learned from assistant frames and stays sticky
// so idle frames never blank the chrome.

func TestModelFromEvent(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"claude message.model", `{"type":"assistant","message":{"model":"claude-opus-4-8","content":[]}}`, "claude-opus-4-8"},
		{"top-level model", `{"type":"assistant","model":"grok-4.5"}`, "grok-4.5"},
		{"message wins over top level", `{"model":"stale","message":{"model":"claude-sonnet-4-5"}}`, "claude-sonnet-4-5"},
		{"no model named", `{"type":"assistant","message":{"content":[]}}`, ""},
		{"not json", `not json at all`, ""},
		{"empty raw", ``, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := modelFromEvent(claudia.Event{Raw: []byte(tc.raw)})
			if got != tc.want {
				t.Fatalf("modelFromEvent(%s) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestAgentProgressLearnsModelAndKeepsIt(t *testing.T) {
	hub := NewAgentProgressHub()

	if !hub.Observe("w", claudia.Event{
		Type: "assistant",
		Raw:  []byte(`{"type":"assistant","message":{"model":"claude-opus-4-8"}}`),
	}) {
		t.Fatal("first assistant frame should change the snapshot")
	}
	if got := hub.Get("w").Model; got != "claude-opus-4-8" {
		t.Fatalf("model=%q want claude-opus-4-8", got)
	}

	// A tool frame names no model — the learned one must survive.
	hub.Observe("w", claudia.Event{
		Type:         "progress",
		ProgressType: "tool_use",
		Raw:          []byte(`{"update":{"sessionUpdate":"tool_call","title":"Bash: go test"}}`),
	})
	if got := hub.Get("w").Model; got != "claude-opus-4-8" {
		t.Fatalf("after tool frame model=%q, want it sticky", got)
	}

	// Process liveness baseline must not blank it either.
	hub.SetStatus("w", "stopped")
	if got := hub.Get("w").Model; got != "claude-opus-4-8" {
		t.Fatalf("after SetStatus model=%q, want it sticky", got)
	}
}

func TestAgentProgressModelChangePushesRefresh(t *testing.T) {
	hub := NewAgentProgressHub()
	// Idle frame: same phase/summary twice is normally a no-change...
	hub.Observe("w", claudia.Event{Type: "assistant", Raw: []byte(`{"message":{"model":"grok-4.5"}}`)})
	if hub.Observe("w", claudia.Event{Type: "assistant", Raw: []byte(`{"message":{"model":"grok-4.5"}}`)}) {
		t.Fatal("identical frame should not report a change")
	}
	// ...but a new model must, so the RHS repaints the prefix after migrate.
	if !hub.Observe("w", claudia.Event{Type: "assistant", Raw: []byte(`{"message":{"model":"claude-opus-4-8"}}`)}) {
		t.Fatal("model change should report a change")
	}
	if got := hub.Get("w").Model; got != "claude-opus-4-8" {
		t.Fatalf("model=%q want claude-opus-4-8", got)
	}
}

// 🎯T311: the badge names what the agent is RUNNING. Precedence is
// observation-first — live frames, then the harness's own session log — with
// the registry Model pin as a placeholder only until something is observed.
// The owner's bug: jevons-po ran fable for hours while the badge said O5,
// because a sticky observation from the previous run outlived it.

const (
	testClaudeSessionA = "019fc1ba-3333-7000-8000-00000000000a"
	testClaudeSessionB = "019fc1ba-4444-7000-8000-00000000000b"
)

// writeClaudeSession lays out projects/<encoded workdir>/<session>.jsonl with
// one assistant frame per model, in order.
func writeClaudeSession(t *testing.T, projects, workDir, sessionID string, models ...string) {
	t.Helper()
	dir := filepath.Join(projects, discovery.EncodeClaudeProject(workDir))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, m := range models {
		b.WriteString(`{"type":"assistant","message":{"model":"` + m + `","content":[]}}` + "\n")
	}
	if err := os.WriteFile(filepath.Join(dir, sessionID+".jsonl"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

func claudeOnlyModels(projects string) *fleetModelResolver {
	return newFleetModelResolver(discovery.Roots{ClaudeProjects: projects})
}

func TestListFleetAgentsCarriesProviderAndModel(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, d := range []claudia.AgentDef{
		{Name: "grokker", WorkDir: dir, SessionID: testGrokSessionID, Provider: claudia.ProviderGrok},
		{Name: "observed", WorkDir: dir, SessionID: testClaudeSessionA, Provider: claudia.ProviderClaude},
	} {
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}
	hub := NewAgentProgressHub()
	hub.Observe("observed", claudia.Event{
		Type: "assistant",
		Raw:  []byte(`{"message":{"model":"claude-opus-4-8"}}`),
	})

	byName := map[string]agentInfo{}
	for _, a := range listFleetAgentsNotifying(reg, nil, hub, nil) {
		byName[a.Name] = a
	}

	if got := byName["grokker"].Provider; got != "grok" {
		t.Fatalf("grokker provider=%q want grok", got)
	}
	// Nothing pinned, nothing observed, no session root wired → empty, so the
	// UI paints the icon alone rather than inventing a version.
	if got := byName["grokker"].Model; got != "" {
		t.Fatalf("grokker model=%q want empty", got)
	}
	if got := byName["observed"].Model; got != "claude-opus-4-8" {
		t.Fatalf("observed model=%q want claude-opus-4-8", got)
	}
}

// Acceptance 2: a freshly (re)started agent shows its running model at once —
// the daemon's hub is empty, so the observation is seeded from the session log.
func TestAttachSeedsRunningModelFromSessionLog(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	projects, work := t.TempDir(), t.TempDir()
	writeClaudeSession(t, projects, work, testClaudeSessionA, "claude-opus-5", "claude-fable-5")
	// The owner's row: pinned "fable" at launch, actually running fable-5.
	if err := reg.Register(claudia.AgentDef{
		Name: "jevons-po", WorkDir: work, SessionID: testClaudeSessionA,
		Provider: claudia.ProviderClaude, Model: "fable",
	}); err != nil {
		t.Fatal(err)
	}

	// Hub empty (daemon just restarted) — the badge must not be blank, and
	// must not fall back to the launch pin when the log knows better.
	got := modelOf(t, listFleetAgentsNotifying(reg, nil, NewAgentProgressHub(), claudeOnlyModels(projects)), "jevons-po")
	if got != "claude-fable-5" {
		t.Fatalf("model=%q want claude-fable-5 seeded from the session log", got)
	}
}

// Acceptance 1: a live agent is never blank. Even with no session log yet, the
// launch pin stands in until the first observation.
func TestLiveAgentNeverReportsEmptyModel(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	if err := reg.Register(claudia.AgentDef{
		Name: "jv-fresh", WorkDir: work, SessionID: testClaudeSessionA,
		Provider: claudia.ProviderClaude, Model: "fable",
	}); err != nil {
		t.Fatal(err)
	}
	// Session root wired but the agent has written no turn yet.
	got := modelOf(t, listFleetAgentsNotifying(reg, nil, NewAgentProgressHub(), claudeOnlyModels(t.TempDir())), "jv-fresh")
	if got == "" {
		t.Fatal("live agent reported an empty model — the launch pin should stand in")
	}
	if got != "fable" {
		t.Fatalf("model=%q want the pin as the pre-observation placeholder", got)
	}
}

// Acceptance 3 (first half): live frames are fresher than the log they will
// eventually be written to, so a model change repaints immediately.
func TestLiveFramesUpdateTheBadgeAheadOfTheLog(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	projects, work := t.TempDir(), t.TempDir()
	writeClaudeSession(t, projects, work, testClaudeSessionA, "claude-opus-5")
	if err := reg.Register(claudia.AgentDef{
		Name: "jv-worker", WorkDir: work, SessionID: testClaudeSessionA,
		Provider: claudia.ProviderClaude, Model: "fable",
	}); err != nil {
		t.Fatal(err)
	}
	hub := NewAgentProgressHub()
	models := claudeOnlyModels(projects)
	if got := modelOf(t, listFleetAgentsNotifying(reg, nil, hub, models), "jv-worker"); got != "claude-opus-5" {
		t.Fatalf("model=%q want the log's claude-opus-5 before any frame", got)
	}

	// It answers a turn on fable-5; the badge must follow the wire, not the
	// pin and not the older log entry.
	hub.Observe("jv-worker", claudia.Event{
		Type: "assistant",
		Raw:  []byte(`{"message":{"model":"claude-fable-5"}}`),
	})
	if got := modelOf(t, listFleetAgentsNotifying(reg, nil, hub, models), "jv-worker"); got != "claude-fable-5" {
		t.Fatalf("model=%q want claude-fable-5 from the live frame", got)
	}
}

// Acceptance 3 (second half): kill clears the sticky observation, and a
// restart under a different model reports the new one rather than the corpse's.
func TestKillClearsObservationAndRestartShowsTheNewModel(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	projects, work := t.TempDir(), t.TempDir()
	writeClaudeSession(t, projects, work, testClaudeSessionA, "claude-opus-5")
	if err := reg.Register(claudia.AgentDef{
		Name: "jv-worker", WorkDir: work, SessionID: testClaudeSessionA,
		Provider: claudia.ProviderClaude,
	}); err != nil {
		t.Fatal(err)
	}
	hub := NewAgentProgressHub()
	models := claudeOnlyModels(projects)
	hub.Observe("jv-worker", claudia.Event{
		Type: "assistant",
		Raw:  []byte(`{"message":{"model":"claude-opus-5"}}`),
	})
	if got := modelOf(t, listFleetAgentsNotifying(reg, nil, hub, models), "jv-worker"); got != "claude-opus-5" {
		t.Fatalf("model=%q want the observation while the agent runs", got)
	}

	// Killed: nothing left in the registry, so the hub must let go of it.
	if err := reg.Remove("jv-worker"); err != nil {
		t.Fatal(err)
	}
	listFleetAgentsNotifying(reg, nil, hub, models)
	if got := hub.Get("jv-worker").Model; got != "" {
		t.Fatalf("after kill the hub still holds model=%q", got)
	}

	// Restarted on a fresh session running fable — the dead run's opus must
	// not come back, even if a stale observation outlived the kill.
	writeClaudeSession(t, projects, work, testClaudeSessionB, "claude-fable-5")
	if err := reg.Register(claudia.AgentDef{
		Name: "jv-worker", WorkDir: work, SessionID: testClaudeSessionB,
		Provider: claudia.ProviderClaude,
	}); err != nil {
		t.Fatal(err)
	}
	hub.Observe("jv-worker", claudia.Event{
		Type: "assistant",
		Raw:  []byte(`{"message":{"model":"claude-opus-5"}}`),
	})
	hub.SyncEpoch("jv-worker", testClaudeSessionA) // stamp it as the dead run's
	if got := modelOf(t, listFleetAgentsNotifying(reg, nil, hub, models), "jv-worker"); got != "claude-fable-5" {
		t.Fatalf("restarted model=%q want claude-fable-5 — stale observation inherited", got)
	}
}

func modelOf(t *testing.T, agents []agentInfo, name string) string {
	t.Helper()
	for _, a := range agents {
		if a.Name == name {
			return a.Model
		}
	}
	t.Fatalf("agent %q not listed", name)
	return ""
}

// 🎯T323: after claude→grok migrate residue, /api/agents must never still
// report Anthropic model ids. Sticky hub observation, registry pin, or both.
func TestListFleetAgentsDropsForeignModelAfterMigrateResidue(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	// Owner evidence shape: provider=grok, sticky/pin Model=fable from Claude.
	if err := reg.Register(claudia.AgentDef{
		Name: "jevons-po", WorkDir: work, SessionID: testGrokSessionID,
		Provider: claudia.ProviderGrok, Model: "fable",
	}); err != nil {
		t.Fatal(err)
	}
	hub := NewAgentProgressHub()
	// Hub still holds the Claude-era observation (session never stamped, or
	// stamped then left as residue).
	hub.Observe("jevons-po", claudia.Event{
		Type: "assistant",
		Raw:  []byte(`{"message":{"model":"claude-fable-5"}}`),
	})

	got := modelOf(t, listFleetAgentsNotifying(reg, nil, hub, nil), "jevons-po")
	if got != "" {
		t.Fatalf("model=%q want empty — Anthropic residue under provider=grok", got)
	}
	// Hub itself must be scrubbed so the next poll does not re-serve it.
	if hub.Get("jevons-po").Model != "" {
		t.Fatalf("hub still holds model=%q after list scrubbed it", hub.Get("jevons-po").Model)
	}
}

// 🎯T323: empty model under grok is fine (mark alone); a same-company pin or
// session-log seed still wins.
func TestListFleetAgentsKeepsSameCompanyModel(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	if err := reg.Register(claudia.AgentDef{
		Name: "empty", WorkDir: work, SessionID: "s-empty",
		Provider: claudia.ProviderGrok,
	}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "pinned", WorkDir: work, SessionID: "s-pin",
		Provider: claudia.ProviderGrok, Model: "grok-4.5-build",
	}); err != nil {
		t.Fatal(err)
	}
	agents := listFleetAgentsNotifying(reg, nil, NewAgentProgressHub(), nil)
	if got := modelOf(t, agents, "empty"); got != "" {
		t.Fatalf("empty model=%q want empty", got)
	}
	if got := modelOf(t, agents, "pinned"); got != "grok-4.5-build" {
		t.Fatalf("pinned model=%q want grok-4.5-build", got)
	}
}

func TestModelFitsProvider(t *testing.T) {
	cases := []struct {
		provider, model string
		want            bool
	}{
		{"grok", "", true},
		{"grok", "grok-4.5-build", true},
		{"grok", "fable", false},
		{"grok", "claude-fable-5", false},
		{"grok", "claude-opus-4-8", false},
		{"claude", "fable", true},
		{"claude", "claude-opus-5", true},
		{"claude", "grok-4.5", false},
		{"bedrock", "claude-sonnet-4-5", true},
		{"", "fable", true},          // no provider → keep; UI sniffs model
		{"mystery", "fable", true},   // unknown provider → keep
		{"grok", "custom-thing", true}, // unrecognised pin → keep
	}
	for _, tc := range cases {
		if got := modelFitsProvider(tc.provider, tc.model); got != tc.want {
			t.Errorf("modelFitsProvider(%q, %q) = %v want %v", tc.provider, tc.model, got, tc.want)
		}
	}
}

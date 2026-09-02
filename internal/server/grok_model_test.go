// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/discovery"
)

// A real Grok updates.jsonl turn_completed frame, trimmed to the fields the
// resolver reads.
func grokTurnLine(model string) string {
	return `{"timestamp":1785673000,"method":"_x.ai/session/update","params":{"sessionId":"s1",` +
		`"update":{"sessionUpdate":"turn_completed","prompt_id":"p1","usage":{"inputTokens":10,` +
		`"outputTokens":5,"costUsdTicks":1000,"modelUsage":{"` + model + `":{"inputTokens":10}}}}}}`
}

func grokMetaLine(model string) string {
	return `{"timestamp":1785673000,"method":"_x.ai/session/update","params":{"sessionId":"s1",` +
		`"update":{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"hi"},` +
		`"_meta":{"modelId":"` + model + `","promptIndex":0}}}}`
}

func grokSummaryJSON(model string) string {
	return `{"current_model_id":"` + model + `","agent_name":"grok-build-plan"}`
}

func TestGrokModelFromTail(t *testing.T) {
	chatter := `{"method":"_x.ai/session/update","params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"hi"}}}}`

	tests := []struct {
		name string
		data string
		want string
	}{
		{"single turn", grokTurnLine("grok-4.5-build"), "grok-4.5-build"},
		{"last turn wins", grokTurnLine("grok-4-build") + "\n" + grokTurnLine("grok-4.5-build"), "grok-4.5-build"},
		{"chat frames carry no model", chatter, ""},
		{"turn among chatter", chatter + "\n" + grokTurnLine("grok-4.5-build") + "\n" + chatter, "grok-4.5-build"},
		{"empty", "", ""},
		{"corrupt line is skipped", `{"turn_completed" "modelUsage" broken`, ""},
		{"_meta.modelId when no billed turn", grokMetaLine("grok-4.6"), "grok-4.6"},
		{"modelUsage beats later _meta.modelId", grokTurnLine("grok-4.6-build") + "\n" + grokMetaLine("grok-4"), "grok-4.6-build"},
	}
	for _, tt := range tests {
		if got := grokModelFromTail([]byte(tt.data)); got != tt.want {
			t.Errorf("%s: grokModelFromTail = %q want %q", tt.name, got, tt.want)
		}
	}
}

// The badge label is what the owner actually sees — 🎯T293 exists because it
// was blank. grok-4.5-build must reduce to the "4.5" the web helper paints.
func TestGrokModelIsVersionBearing(t *testing.T) {
	model := grokModelFromTail([]byte(grokTurnLine("grok-4.5-build")))
	if !strings.HasPrefix(model, "grok-4.5") {
		t.Fatalf("model=%q — web condenseModel needs a version to paint", model)
	}
}

// writeGrokSession lays out sessionsDir/<encoded workdir>/<session>/updates.jsonl.
func writeGrokSession(t *testing.T, sessionsDir, workDir, sessionID, body string) string {
	t.Helper()
	dir := filepath.Join(sessionsDir, discovery.EncodeCWDBucket(workDir), sessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "updates.jsonl")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// Grok session ids are UUIDv7-shaped; discovery.SessionPath rejects anything else.
const testGrokSessionID = "019fc1ba-1111-7000-8000-000000000001"

func TestGrokModelResolverReadsSessionLog(t *testing.T) {
	sessions := t.TempDir()
	work := t.TempDir()
	writeGrokSession(t, sessions, work, testGrokSessionID, grokTurnLine("grok-4.5-build")+"\n")

	r := newGrokModelResolver(sessions)
	if got := r.Model(work, testGrokSessionID); got != "grok-4.5-build" {
		t.Fatalf("Model = %q want grok-4.5-build", got)
	}
	// Second call is cached: deleting the log must not change the answer.
	if err := os.Remove(filepath.Join(sessions, discovery.EncodeCWDBucket(work), testGrokSessionID, "updates.jsonl")); err != nil {
		t.Fatal(err)
	}
	if got := r.Model(work, testGrokSessionID); got != "grok-4.5-build" {
		t.Fatalf("cached Model = %q want grok-4.5-build (sticky)", got)
	}
}

func TestGrokModelResolverEmptyWhenNoTurnYet(t *testing.T) {
	sessions := t.TempDir()
	work := t.TempDir()
	// Session exists but has only chat frames — no billed turn, no model.
	writeGrokSession(t, sessions, work, testGrokSessionID,
		`{"method":"_x.ai/session/update","params":{"update":{"sessionUpdate":"agent_message_chunk"}}}`+"\n")

	r := newGrokModelResolver(sessions)
	if got := r.Model(work, testGrokSessionID); got != "" {
		t.Fatalf("Model = %q want empty — never invent a version", got)
	}
	if got := newGrokModelResolver("").Model(work, testGrokSessionID); got != "" {
		t.Fatalf("no sessions dir: Model = %q want empty", got)
	}
	if got := (*sessionModelResolver)(nil).Model(work, testGrokSessionID); got != "" {
		t.Fatalf("nil resolver: Model = %q want empty", got)
	}
}

// An agent whose workdir moved after the session was created still resolves:
// the bucket no longer matches, so the lookup falls back to a scan by id.
func TestGrokModelResolverFallsBackToSessionScan(t *testing.T) {
	sessions := t.TempDir()
	oldWork := t.TempDir()
	writeGrokSession(t, sessions, oldWork, testGrokSessionID, grokTurnLine("grok-4.5-build")+"\n")

	r := newGrokModelResolver(sessions)
	if got := r.Model(t.TempDir(), testGrokSessionID); got != "grok-4.5-build" {
		t.Fatalf("Model after workdir move = %q want grok-4.5-build", got)
	}
}

// A negative lookup is not repeated on every fleet poll (the sessions root
// holds hundreds of buckets), but it does expire.
func TestGrokModelResolverNegativeLookupIsCachedThenRetried(t *testing.T) {
	sessions := t.TempDir()
	work := t.TempDir()
	now := time.Unix(1785673000, 0)
	r := newGrokModelResolver(sessions)
	r.now = func() time.Time { return now }

	if got := r.Model(work, testGrokSessionID); got != "" {
		t.Fatalf("Model = %q want empty before any session log", got)
	}
	writeGrokSession(t, sessions, work, testGrokSessionID, grokTurnLine("grok-4.5-build")+"\n")
	if got := r.Model(work, testGrokSessionID); got != "" {
		t.Fatalf("Model = %q want empty — negative lookup still within TTL", got)
	}
	now = now.Add(sessionModelLookupTTL + time.Second)
	if got := r.Model(work, testGrokSessionID); got != "grok-4.5-build" {
		t.Fatalf("Model after TTL = %q want grok-4.5-build", got)
	}
}

// A long-lived session log is read from the tail; the last turn still wins.
func TestGrokModelResolverReadsPastTheTailWindow(t *testing.T) {
	sessions := t.TempDir()
	work := t.TempDir()
	var b strings.Builder
	b.WriteString(grokTurnLine("grok-4-build") + "\n")
	filler := `{"method":"_x.ai/session/update","params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"` +
		strings.Repeat("x", 4000) + `"}}}}` + "\n"
	for b.Len() < sessionModelTailBytes+len(filler) {
		b.WriteString(filler)
	}
	b.WriteString(grokTurnLine("grok-4.5-build") + "\n")
	writeGrokSession(t, sessions, work, testGrokSessionID, b.String())

	r := newGrokModelResolver(sessions)
	if got := r.Model(work, testGrokSessionID); got != "grok-4.5-build" {
		t.Fatalf("Model = %q want grok-4.5-build from the tail", got)
	}
}

// The product path: a Grok fleet row reaches the RHS carrying a model.
func TestListFleetAgentsResolvesGrokModel(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	sessions := t.TempDir()
	work := t.TempDir()
	writeGrokSession(t, sessions, work, testGrokSessionID, grokTurnLine("grok-4.5-build")+"\n")
	if err := reg.Register(claudia.AgentDef{
		Name: "grokker", WorkDir: work, SessionID: testGrokSessionID, Provider: claudia.ProviderGrok,
	}); err != nil {
		t.Fatal(err)
	}
	// A Claude agent must not be given a Grok model.
	if err := reg.Register(claudia.AgentDef{
		Name: "clauder", WorkDir: work, SessionID: "019fc1ba-2222-7000-8000-000000000002", Provider: claudia.ProviderClaude,
	}); err != nil {
		t.Fatal(err)
	}

	byName := map[string]agentInfo{}
	for _, a := range listFleetAgentsNotifying(reg, nil, nil, nil, grokOnlyModels(sessions)) {
		byName[a.Name] = a
	}
	if got := byName["grokker"].Model; got != "grok-4.5-build" {
		t.Fatalf("grokker model=%q want grok-4.5-build", got)
	}
	if got := byName["clauder"].Model; got != "" {
		t.Fatalf("clauder model=%q want empty", got)
	}
}

// grokOnlyModels resolves Grok rows from sessions and answers "" for every
// other provider.
func grokOnlyModels(sessions string) *fleetModelResolver {
	return newFleetModelResolver(discovery.Roots{GrokSessions: sessions})
}

// 🎯T311 reverses the old precedence: the session log is what the process
// actually billed its last turn against, so it beats the launch pin, which is
// only ever intent. (Before T311 the pin won and a re-pinned agent kept
// painting the model it no longer ran.)
func TestListFleetAgentsPrefersSessionLogOverPinnedGrokModel(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	sessions := t.TempDir()
	work := t.TempDir()
	writeGrokSession(t, sessions, work, testGrokSessionID, grokTurnLine("grok-4.5-build")+"\n")
	if err := reg.Register(claudia.AgentDef{
		Name: "pinned", WorkDir: work, SessionID: testGrokSessionID,
		Provider: claudia.ProviderGrok, Model: "grok-4",
	}); err != nil {
		t.Fatal(err)
	}
	agents := listFleetAgentsNotifying(reg, nil, nil, nil, grokOnlyModels(sessions))
	if len(agents) != 1 || agents[0].Model != "grok-4.5-build" {
		t.Fatalf("agents=%+v want the running grok-4.5-build, not the grok-4 pin", agents)
	}

	// With no session log yet, the pin is all there is — better than a blank
	// badge on a live agent, and it yields the moment a turn is written.
	agents = listFleetAgentsNotifying(reg, nil, nil, nil, grokOnlyModels(t.TempDir()))
	if len(agents) != 1 || agents[0].Model != "grok-4" {
		t.Fatalf("agents=%+v want the pin as the pre-observation placeholder", agents)
	}
}

// writeGrokHomeSession plants GROK_HOME/sessions/<bucket>/<id>/{summary,updates}.
func writeGrokHomeSession(t *testing.T, grokHome, workDir, sessionID, summary, updates string) {
	t.Helper()
	dir := filepath.Join(grokHome, "sessions", discovery.EncodeCWDBucket(workDir), sessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if summary != "" {
		if err := os.WriteFile(filepath.Join(dir, "summary.json"), []byte(summary), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if updates != "" {
		if err := os.WriteFile(filepath.Join(dir, "updates.jsonl"), []byte(updates), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func grokHomeModels(grokHome string) *fleetModelResolver {
	return newFleetModelResolver(discovery.Roots{
		GrokHomeSessions: []string{filepath.Join(grokHome, "sessions")},
	})
}

// 🎯T619: exclusive-MCP seats write under GROK_HOME, not cfg.SessionsDir.
// A planted current_model_id beats a disagreeing launch pin.
func TestListFleetAgentsPrefersGROKHomeSummaryOverPinnedGrokModel(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	work := t.TempDir()
	writeGrokHomeSession(t, home, work, testGrokSessionID, grokSummaryJSON("grok-4.6"), "")
	if err := reg.Register(claudia.AgentDef{
		Name: "exclusive", WorkDir: work, SessionID: testGrokSessionID,
		Provider: claudia.ProviderGrok, Model: "grok-4.5",
	}); err != nil {
		t.Fatal(err)
	}

	// cfg.SessionsDir is a different empty tree — the session is only in GROK_HOME.
	models := newFleetModelResolver(discovery.Roots{
		GrokSessions:     t.TempDir(),
		GrokHomeSessions: []string{filepath.Join(home, "sessions")},
	})
	agents := listFleetAgentsNotifying(reg, nil, nil, nil, models)
	if len(agents) != 1 || agents[0].Model != "grok-4.6" {
		t.Fatalf("agents=%+v want GROK_HOME current_model_id grok-4.6, not the grok-4.5 pin", agents)
	}

	// Pin stands in when the exclusive home has no evidence yet.
	agents = listFleetAgentsNotifying(reg, nil, nil, nil, grokHomeModels(t.TempDir()))
	if len(agents) != 1 || agents[0].Model != "grok-4.5" {
		t.Fatalf("agents=%+v want the pin as the pre-observation placeholder", agents)
	}
}

func TestGrokModelFromEvidencePrefersSummaryCurrentModelID(t *testing.T) {
	got := grokModelFromEvidence([]byte(grokSummaryJSON("grok-4.6")))
	if got != "grok-4.6" {
		t.Fatalf("summary = %q want grok-4.6", got)
	}
	got = grokModelFromEvidence([]byte(grokTurnLine("grok-4.6-build") + "\n"))
	if got != "grok-4.6-build" {
		t.Fatalf("usage tail = %q want grok-4.6-build", got)
	}
}

func TestGrokEvidencePathNewestMtimeWins(t *testing.T) {
	work := t.TempDir()
	oldHome := t.TempDir()
	newHome := t.TempDir()
	writeGrokHomeSession(t, oldHome, work, testGrokSessionID, grokSummaryJSON("grok-4.5"), "")
	writeGrokHomeSession(t, newHome, work, testGrokSessionID, grokSummaryJSON("grok-4.6"), "")

	oldSum := filepath.Join(oldHome, "sessions", discovery.EncodeCWDBucket(work), testGrokSessionID, "summary.json")
	newSum := filepath.Join(newHome, "sessions", discovery.EncodeCWDBucket(work), testGrokSessionID, "summary.json")
	oldTime := time.Now().Add(-time.Hour)
	newTime := time.Now()
	if err := os.Chtimes(oldSum, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(newSum, newTime, newTime); err != nil {
		t.Fatal(err)
	}

	roots := []string{
		filepath.Join(oldHome, "sessions"),
		filepath.Join(newHome, "sessions"),
	}
	got := grokEvidencePath(roots, work, testGrokSessionID)
	if got != newSum {
		t.Fatalf("evidence path = %q want newest %q", got, newSum)
	}
	r := newGrokModelResolver("", roots...)
	if model := r.Model(work, testGrokSessionID); model != "grok-4.6" {
		t.Fatalf("Model = %q want grok-4.6 from newest exclusive home", model)
	}
}

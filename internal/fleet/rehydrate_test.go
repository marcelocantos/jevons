// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
)

// 🎯T313 hermetic surface. Everything here is decided from disk state
// plus the registry; no Claude, no Grok, no process is spawned.
//
// HOME is redirected so claudia.SessionExists resolves
// $HOME/.claude/projects/<escaped-workdir>/<session>.jsonl inside the
// test's own tree — the real one must never be read or written.

// lostSessionFixture registers a Materialized agent whose Claude JSONL
// does not exist: the exact registry shape a daemon restart leaves
// behind when a worker's first turn never submitted.
func lostSessionFixture(t *testing.T) (*Claudia, *claudia.Registry, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)

	workDir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	def := claudia.AgentDef{
		Name:         "jv-t313-rehydrate",
		WorkDir:      workDir,
		SessionID:    "cd641cad-1a42-47de-a037-1643add32e94",
		Materialized: true,
		Provider:     claudia.ProviderClaude,
		Model:        "opus",
		Parent:       "jevons-po",
		Purpose:      claudia.PurposeWork,
		TargetID:     "T313",
		AutoStart:    true,
	}
	if err := reg.Register(def); err != nil {
		t.Fatal(err)
	}
	return NewClaudia(reg), reg, workDir
}

// writeSessionJSONL materialises the transcript claudia would resume.
func writeSessionJSONL(t *testing.T, sessionID, workDir string) {
	t.Helper()
	path := claudia.SessionJSONLPath(sessionID, workDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// Acceptance 3: a registry entry with a missing JSONL rehydrates, and
// lineage / target / provider / model survive the rotation.
func TestRehydrateLostSessionPreservesLineage(t *testing.T) {
	f, reg, _ := lostSessionFixture(t)
	before := *reg.Def("jv-t313-rehydrate")

	lost, ok, err := f.RehydrateLostSession("jv-t313-rehydrate")
	if err != nil {
		t.Fatalf("rehydrate: %v", err)
	}
	if !ok {
		t.Fatal("missing JSONL did not trigger rehydrate — the dead-end survives")
	}

	after := reg.Def("jv-t313-rehydrate")
	if after == nil {
		t.Fatal("agent deregistered by rehydrate; the row must survive")
	}
	if after.SessionID == before.SessionID {
		t.Fatalf("session not rotated: still %s", after.SessionID)
	}
	if after.SessionID != lost.NewSession {
		t.Fatalf("registry session %s != reported new session %s", after.SessionID, lost.NewSession)
	}
	if after.Materialized {
		t.Fatal("rotated row still Materialized — next Launch would demand a resume again")
	}
	// The whole point: identity rides through the rotation.
	if after.Name != before.Name || after.WorkDir != before.WorkDir {
		t.Fatalf("identity lost: name=%q workdir=%q", after.Name, after.WorkDir)
	}
	if after.Parent != "jevons-po" {
		t.Fatalf("parent lineage lost: %q", after.Parent)
	}
	if after.Purpose != claudia.PurposeWork {
		t.Fatalf("purpose lost: %q", after.Purpose)
	}
	if after.Provider != claudia.ProviderClaude {
		t.Fatalf("provider lost: %q", after.Provider)
	}
	if after.Model != "opus" {
		t.Fatalf("model lost: %q", after.Model)
	}
	if after.TargetID != "T313" {
		t.Fatalf("target binding lost: %q", after.TargetID)
	}
	if !after.AutoStart {
		t.Fatal("auto-start lost")
	}

	// Acceptance 2: the surface names the lost session and says the
	// brief must be re-sent.
	desc := lost.Describe()
	if !strings.Contains(desc, before.SessionID) {
		t.Fatalf("report does not name the lost session id: %s", desc)
	}
	if !strings.Contains(desc, "PRIOR CONTEXT IS LOST") || !strings.Contains(desc, "re-send") {
		t.Fatalf("report does not state context is gone / brief must be re-sent: %s", desc)
	}
	if !strings.Contains(desc, "jevons-po") || !strings.Contains(desc, "T313") {
		t.Fatalf("report does not evidence preserved lineage/target: %s", desc)
	}
}

// A healthy agent must not be rotated: rehydrate is a recovery path,
// not something that runs on every start. Rotating a live conversation
// would destroy exactly the history claudia's guard protects.
func TestRehydrateSkipsHealthySession(t *testing.T) {
	f, reg, workDir := lostSessionFixture(t)
	def := reg.Def("jv-t313-rehydrate")
	writeSessionJSONL(t, def.SessionID, workDir)

	lost, ok, err := f.RehydrateLostSession("jv-t313-rehydrate")
	if err != nil {
		t.Fatalf("rehydrate: %v", err)
	}
	if ok {
		t.Fatalf("healthy session rotated onto %s — history would be discarded", lost.NewSession)
	}
	if got := reg.Def("jv-t313-rehydrate").SessionID; got != def.SessionID {
		t.Fatalf("session changed on a healthy agent: %s", got)
	}
}

// SessionLost is the gate. It must fire only for a Claude row that is
// Materialized with no transcript — never on a first launch, and never
// on a provider whose sessions jevons does not store.
func TestSessionLostGate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workDir := t.TempDir()

	claudeLost := &claudia.AgentDef{
		Name: "a", WorkDir: workDir, SessionID: "missing-id",
		Materialized: true, Provider: claudia.ProviderClaude,
	}
	if !SessionLost(claudeLost) {
		t.Fatal("materialized claude row with no JSONL not reported lost")
	}

	// Not yet materialized: a first launch legitimately has no JSONL.
	fresh := *claudeLost
	fresh.Materialized = false
	if SessionLost(&fresh) {
		t.Fatal("un-materialized row reported lost — first launch would rotate needlessly")
	}

	// Grok/Codex keep sessions in provider-owned stores; a missing
	// Claude JSONL says nothing about them.
	grok := *claudeLost
	grok.Provider = claudia.ProviderGrok
	if SessionLost(&grok) {
		t.Fatal("grok row judged lost from a Claude transcript path")
	}

	// Empty provider means Claude, and the transcript exists.
	present := *claudeLost
	present.Provider = ""
	present.SessionID = "present-id"
	writeSessionJSONL(t, present.SessionID, workDir)
	if SessionLost(&present) {
		t.Fatal("row with an existing transcript reported lost")
	}

	if SessionLost(nil) {
		t.Fatal("nil def reported lost")
	}
}

// RehydratedDef is the pure rotation. Kept separately testable because
// it is the piece that must not quietly drop a field when AgentDef
// grows one.
func TestRehydratedDefClearsLivenessOnly(t *testing.T) {
	def := claudia.AgentDef{
		Name: "w", WorkDir: "/tmp/w", SessionID: "old", Materialized: true,
		Provider: claudia.ProviderClaude, Model: "opus", Parent: "jevons-po",
		Purpose: claudia.PurposeWork, TargetID: "T313", AutoStart: true,
		Description: "widget", ConnectURL: "http://dead", ConnectPID: 4321,
	}
	next := RehydratedDef(def, "new")

	if next.SessionID != "new" || next.Materialized {
		t.Fatalf("session/materialized wrong: %s %v", next.SessionID, next.Materialized)
	}
	// 🎯T204 trap: a persisted dead endpoint sends the next Launch into
	// a reattach that resets.
	if next.ConnectURL != "" || next.ConnectPID != 0 {
		t.Fatalf("stale connect endpoint survived: %q %d", next.ConnectURL, next.ConnectPID)
	}
	if next.Name != def.Name || next.WorkDir != def.WorkDir || next.Provider != def.Provider ||
		next.Model != def.Model || next.Parent != def.Parent || next.Purpose != def.Purpose ||
		next.TargetID != def.TargetID || next.AutoStart != def.AutoStart ||
		next.Description != def.Description {
		t.Fatalf("rotation dropped identity fields: %+v", next)
	}
}

// Unregistered names are an error, not a silent mint of a new agent.
func TestRehydrateUnknownAgent(t *testing.T) {
	f, _, _ := lostSessionFixture(t)
	if _, ok, err := f.RehydrateLostSession("nobody"); err == nil || ok {
		t.Fatalf("unknown agent rehydrated: ok=%v err=%v", ok, err)
	}
}

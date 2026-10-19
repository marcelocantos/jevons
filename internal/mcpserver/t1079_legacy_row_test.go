// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/discovery"
	"github.com/marcelocantos/jevons/internal/seatstate"
)

// t1079Pairs is each plan in the two spellings a registry row can carry: the
// plan name an older boot or a legacy mint stored, and the launch id worker
// mints have stored since f0c3f7bd. claudia v0.58 launches both on the same
// sidecar seat (🎯T1079).
var t1079Pairs = [][2]claudia.Provider{
	{"grok", "xai-oauth"},
	{"claude", "anthropic"},
	{"codex", "openai-codex"},
	{"Grok", "xai-oauth"},
}

// 🎯T1079: a legacy row that stores a plan name behaves as the launch-id row
// does on the production transcript and evidence readers, the observation
// pass that feeds /api/agents, the capacity census, and the plan-dependent
// controls. The fixture is the worst case for the old code: the session id
// has both a Grok Build store and a Claude Code JSONL on disk, which a
// plan-name branch read as the seat's conversation while the launch-id row
// (correctly) did not.
func TestT1079LegacyPlanRowMatchesLaunchRow(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("JEVONS_SPOOL_DIR", t.TempDir())
	work := t.TempDir()
	const sid = "019f4f4b-945a-7a23-ba4c-51a0c26e1079"
	grokSessions := filepath.Join(home, ".grok", "sessions")
	vendor := filepath.Join(grokSessions, discovery.EncodeCWDBucket(work), sid)
	if err := os.MkdirAll(vendor, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vendor, "updates.jsonl"), []byte(`{"method":"session/update"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	jsonl := claudia.SessionJSONLPath(sid, work)
	if err := os.MkdirAll(filepath.Dir(jsonl), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(jsonl, []byte(`{"type":"user"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	roots := discovery.Roots{GrokSessions: grokSessions, ClaudeProjects: filepath.Join(home, ".claude", "projects")}

	type reading struct {
		Existence    TranscriptExistence
		Evidence     SessionEvidence
		KeepsJSONL   bool
		Status       string
		Load         map[string]int
		Reconnect    bool
		Registration bool
		DeferPrompt  bool
	}
	read := func(provider claudia.Provider) reading {
		def := claudia.AgentDef{Name: "jv-t1079-seat", WorkDir: work, SessionID: sid, Provider: provider}
		s := New(t.TempDir(), nil, nil)
		s.SetBirthRoots(roots)
		_, reconnect := mcpReconnectUnsupported(provider)
		_, registration := mcpRegistrationsFor(provider)
		load := activeProcessLoad([]claudia.AgentDef{def},
			func(string) seatstate.State { return seatstate.State{Alive: seatstate.Yes} },
			func(string) bool { return false })
		return reading{
			Existence: LookupTranscriptExistence(TranscriptExistenceQuery{
				Name: def.Name, Provider: provider, SessionID: sid, WorkDir: work, Roots: roots,
			}),
			Evidence:     ReadSessionEvidence(provider, sid, work),
			KeepsJSONL:   providerKeepsClaudeTranscript(provider),
			Status:       observedAgentPhase(s, def, true),
			Load:         map[string]int(load),
			Reconnect:    reconnect,
			Registration: registration,
			DeferPrompt:  deferStartPrompt(provider),
		}
	}
	for _, pair := range t1079Pairs {
		legacy, launch := read(pair[0]), read(pair[1])
		if !reflect.DeepEqual(legacy, launch) {
			t.Errorf("row %q reads differently from row %q:\n legacy %+v\n launch %+v", pair[0], pair[1], legacy, launch)
		}
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0
package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/cost"
	"github.com/marcelocantos/jevons/internal/spool"
)

func TestT1049UnsupportedPinPreservesHistoryAndSession(t *testing.T) {
	cases := []struct{ name, rollout, want string }{
		{"empty evidence", "", "absence is not complete evidence"},
		{"submitted prompts without answer", `{"type":"event_msg","payload":{"type":"user_message"}}` + "\n" + `{"type":"response_item","payload":{"role":"user"}}` + "\n", "2 submitted user messages, 0 assistant messages"},
		{"assistant history", `{"type":"response_item","payload":{"role":"assistant"}}` + "\n", "1 assistant messages"},
		{"partial rollout", `{"type":"event_msg","payload":`, "partial/unreadable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("XDG_STATE_HOME", home)
			t.Setenv(spool.DirEnv, filepath.Join(home, "empty-spool"))
			s := t1046Server(t)
			d := claudia.AgentDef{Name: "fixture", Provider: "openai-codex", Model: cost.ModelCodexSpark, SessionID: "thread-fixture", WorkDir: t.TempDir()}
			if err := s.registry.Register(d); err != nil {
				t.Fatal(err)
			}
			if tc.rollout != "" {
				path := filepath.Join(codexHomeDir(d.SessionID), "sessions", "2026", "rollout-thread-fixture.jsonl")
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(tc.rollout), 0600); err != nil {
					t.Fatal(err)
				}
			}
			err := s.diagnoseUnsupportedStoredPin(s.registry.Def(d.Name))
			if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "Recovery:") {
				t.Fatalf("diagnosis=%v want %q", err, tc.want)
			}
			before := s.registry.Def(d.Name)
			// Evidence can change between the diagnosis and the requested retry.
			// Neither observation can authorize a destructive rotation.
			if tc.rollout == "" {
				path := filepath.Join(codexHomeDir(d.SessionID), "sessions", "rollout-thread-fixture.jsonl")
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(`{"type":"event_msg","payload":{"type":"user_message"}}`+"\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			got, existed, _, retryErr := s.stitchAgentStart(d.Name, d.WorkDir, "", "", "mechanical", "jevons-po", "work", "", "")
			if retryErr == nil || !existed || got != nil {
				t.Fatalf("retry should refuse: got=%+v existed=%v err=%v", got, existed, retryErr)
			}
			after := s.registry.Def(d.Name)
			if before.SessionID != after.SessionID || before.Model != after.Model || before.Materialized != after.Materialized {
				t.Fatalf("row changed: before=%+v after=%+v", before, after)
			}
		})
	}
}

func TestT1049SidecarHistoryCannotBeTreatedAsEmpty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_STATE_HOME", home)
	t.Setenv(spool.DirEnv, filepath.Join(home, "spool"))
	if err := os.MkdirAll(spool.Dir(), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(spool.Dir(), "events-2026-10-10.log"), []byte(`{"ts":"2026-10-10T00:00:00Z","seat":"fixture","type":"turn_end","text":"assistant"}`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s := t1046Server(t)
	d := claudia.AgentDef{Name: "fixture", Provider: "openai-codex", Model: cost.ModelCodexSpark, SessionID: "thread-fixture", WorkDir: t.TempDir()}
	if err := s.registry.Register(d); err != nil {
		t.Fatal(err)
	}
	err := s.diagnoseUnsupportedStoredPin(s.registry.Def(d.Name))
	if err == nil || !strings.Contains(err.Error(), "sidecar spool has 1 seat records") {
		t.Fatalf("diagnosis=%v", err)
	}
}

func TestT1049NonSparkUnavailableCodexPinIsFailClosed(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_STATE_HOME", home)
	t.Setenv(spool.DirEnv, filepath.Join(home, "spool"))
	s := t1046Server(t)
	d := claudia.AgentDef{Name: "retired-model", Provider: "openai-codex", Model: "gpt-retired-codex", SessionID: "retired-thread", WorkDir: t.TempDir()}
	if err := s.registry.Register(d); err != nil {
		t.Fatal(err)
	}
	got, existed, _, err := s.stitchAgentStart(d.Name, d.WorkDir, "", "", "mechanical", "jevons-po", "work", "", "")
	if err == nil || !existed || got != nil || !strings.Contains(err.Error(), "gpt-retired-codex") || !strings.Contains(err.Error(), "Recovery:") {
		t.Fatalf("non-Spark retry: got=%+v existed=%v err=%v", got, existed, err)
	}
	after := s.registry.Def(d.Name)
	if after.Model != d.Model || after.SessionID != d.SessionID {
		t.Fatalf("row rotated: %+v", after)
	}
	s.markAgentTurnBegan(d.Name)
	if s.registry.Def(d.Name).Materialized {
		t.Fatal("unsupported non-Spark turn must not earn resume")
	}
}

func TestT1049AlternateHistorySourceIsNotProofOfEmptiness(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_STATE_HOME", home)
	t.Setenv(spool.DirEnv, filepath.Join(home, "missing-default-spool"))
	// An independently configured sidecar store (or a native non-exclusive
	// CODEX_HOME) can hold history the default evidence reader cannot locate.
	alternate := filepath.Join(home, "alternate-sidecar-store", "session.jsonl")
	if err := os.MkdirAll(filepath.Dir(alternate), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(alternate, []byte(`{"role":"assistant","content":"history"}`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s := t1046Server(t)
	d := claudia.AgentDef{Name: "alternate", Provider: "openai-codex", Model: "gpt-retired-codex", SessionID: "alternate-thread", WorkDir: t.TempDir()}
	if err := s.registry.Register(d); err != nil {
		t.Fatal(err)
	}
	diagnostic := s.diagnoseUnsupportedStoredPin(s.registry.Def(d.Name))
	if diagnostic == nil || !strings.Contains(diagnostic.Error(), "absence is not complete evidence") {
		t.Fatalf("alternate history falsely certified empty: %v", diagnostic)
	}
	if got, _, _, err := s.stitchAgentStart(d.Name, d.WorkDir, "", "", "mechanical", "jevons-po", "work", "", ""); err == nil || got != nil {
		t.Fatalf("alternate history permitted fallback: %+v, %v", got, err)
	}
}

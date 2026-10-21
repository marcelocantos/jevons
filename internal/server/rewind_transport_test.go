// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/coder/websocket"
	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/chatlog"
)

// The withdrawn product operation must refuse before touching either history
// store or the registry, and must never attempt a provider launch.
func TestT1055RewindRefusesWithoutMutatingHistory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, provider := range []claudia.Provider{"", "claude", "anthropic", "codex", "openai-codex", "grok", "xai-oauth", "cursor"} {
		t.Run(string(provider), func(t *testing.T) {
			dir := t.TempDir()
			workdir, err := filepath.EvalSymlinks(dir)
			if err != nil {
				t.Fatal(err)
			}
			reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
			if err != nil {
				t.Fatal(err)
			}
			reg.SetDirect(true)
			const oldSession = "native-history"
			if err := reg.Register(claudia.AgentDef{Name: "jevons", WorkDir: workdir, Provider: provider, SessionID: oldSession, Materialized: true}); err != nil {
				t.Fatal(err)
			}
			original := "{\"type\":\"user\",\"message\":{\"content\":\"keep\"}}\n{\"type\":\"assistant\",\"message\":{\"content\":\"answer\"}}\n{\"type\":\"user\",\"message\":{\"content\":\"remove\"}}\n"
			nativePath := claudia.SessionJSONLPath(oldSession, workdir)
			if err := os.MkdirAll(filepath.Dir(nativePath), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(nativePath, []byte(original), 0o600); err != nil {
				t.Fatal(err)
			}
			journalPath := filepath.Join(dir, "chat.jsonl")
			journal, err := chatlog.Open(journalPath)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = journal.Close() })
			for _, line := range strings.Split(strings.TrimSpace(original), "\n") {
				if err := journal.Append(line); err != nil {
					t.Fatal(err)
				}
			}
			before := *reg.Def("jevons")
			reg.SetLaunchers(&claudia.RegistryLaunchers{Start: func(_ context.Context, cfg claudia.Config) (*claudia.Agent, error) {
				t.Fatal("withdrawn rewind attempted a provider launch")
				return nil, nil
			}})
			s := New("test", dir)
			s.SetRegistry(reg)
			s.SetChatLog(journal)
			if err := s.RewindOverseer(1); err == nil || !strings.Contains(err.Error(), "rewind is unavailable") {
				t.Fatalf("rewind error = %v", err)
			}
			stored, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
			if err != nil {
				t.Fatal(err)
			}
			if def := stored.Def("jevons"); !reflect.DeepEqual(*def, before) {
				t.Fatalf("withdrawn rewind mutated registry: before=%+v after=%+v", before, def)
			}
			data, err := os.ReadFile(nativePath)
			if err != nil || string(data) != original {
				t.Fatalf("native transcript changed: %q, %v", data, err)
			}
			data, err = os.ReadFile(journalPath)
			if err != nil || string(data) != original {
				t.Fatalf("durable owner journal changed: %q, %v", data, err)
			}
		})
	}
}

func TestT1055RewindControlReportsWithdrawal(t *testing.T) {
	s, path := serverWithJournal(t)
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		_, msg, err := conn.Read(r.Context())
		if err != nil {
			return
		}
		s.handleChatControlFrame(r.Context(), conn, nil, string(msg))
	}))
	defer host.Close()
	conn, _, err := websocket.Dial(t.Context(), host.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	if err := conn.Write(t.Context(), websocket.MessageText, []byte(`{"type":"rewind","turns":1}`)); err != nil {
		t.Fatal(err)
	}
	_, payload, err := conn.Read(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var frame struct {
		Type  string
		Error string
	}
	if err := json.Unmarshal(payload, &frame); err != nil {
		t.Fatal(err)
	}
	if frame.Type != "error" || !strings.Contains(frame.Error, "conversation has not been changed") {
		t.Fatalf("withdrawal response = %s", payload)
	}
	if got := journalLines(t, path); len(got) != 0 {
		t.Fatalf("control frame journaled: %v", got)
	}
}

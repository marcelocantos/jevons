// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
)

// These are adversarial checks of the journey, not product journeys. Exercise
// the entire J9 body against a controlled MCP peer, including cleanup and the
// independently observed launch provider. A canned nonempty answer used to pass.
func TestT625DirectedJourneyRejectsFalseGreens(t *testing.T) {
	for _, tc := range []struct {
		name, reply, provider string
		wantOK                bool
	}{
		{"exact requested reply", "exact", "grok", true},
		{"empty reply", "", "grok", false},
		{"unrelated completed reply", "unrelated", "grok", false},
		{"old fixed token", "orch-direct-ok", "grok", false},
		{"truncated requested token", "truncated", "grok", false},
		{"token only mentioned", "mentioned", "grok", false},
		{"reply from previous invocation", "replayed", "grok", false},
		{"wrong worker provider", "exact", "claude", false},
		{"no runtime launch", "exact", "", false},
		{"registry survives removal", "exact", "registry-leak", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := t.TempDir()
			logPath := filepath.Join(state, "jevonsd.log")
			var id string
			var removed, directed bool
			ids, tokens := map[string]bool{}, map[string]bool{}
			previousToken := "no previous request"
			peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/agents" {
					agents := []AgentInfo{}
					if tc.provider == "registry-leak" {
						agents = append(agents, AgentInfo{Name: id})
					}
					_ = json.NewEncoder(w).Encode(agents)
					return
				}
				var req struct {
					Method string `json:"method"`
					Params struct {
						Name string            `json:"name"`
						Args map[string]string `json:"arguments"`
					} `json:"params"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				if r.URL.Path != "/mcp" || req.Method != "tools/call" {
					t.Errorf("unexpected request %s %s", r.URL.Path, req.Method)
				}
				var reply string
				switch req.Params.Name {
				case "jevons_thread_spawn":
					id = req.Params.Args["id"]
					removed = false
					if id == "" || ids[id] {
						t.Errorf("worker identity reused or empty: %q", id)
					}
					ids[id] = true
					if req.Params.Args["provider"] != "grok" {
						t.Error("journey failed to explicitly select Grok")
					}
					if _, err := os.Stat(req.Params.Args["workdir"]); err != nil {
						t.Errorf("worker directory: %v", err)
					}
					launch := ""
					if tc.provider != "" {
						provider := tc.provider
						if provider == "registry-leak" {
							provider = "grok"
						}
						launch = fmt.Sprintf("msg=\"agent started\" name=%s provider=%s\n", id, provider)
					}
					if err := os.WriteFile(logPath, []byte(launch), 0o600); err != nil {
						t.Error(err)
					}
					reply = fmt.Sprintf("Spawned thread %q (session fixture).", id)
				case "jevons_thread_list":
					if !removed {
						reply = id
					}
				case "jevons_thread_direct":
					directed = true
					if req.Params.Args["id"] != id {
						t.Error("direct was sent to a different worker")
					}
					token := strings.TrimPrefix(req.Params.Args["text"], "Reply with exactly: ")
					if token == "" || tokens[token] {
						t.Errorf("request token reused or empty: %q", token)
					}
					tokens[token] = true
					reply = tc.reply
					switch tc.reply {
					case "exact":
						reply = token
					case "truncated":
						reply = token[:len(token)/2]
					case "mentioned":
						reply = "I could not produce " + token
					case "replayed":
						reply = previousToken
					}
					previousToken = token
				case "jevons_thread_remove":
					if req.Params.Args["id"] != id {
						t.Error("cleanup targeted a different worker")
					}
					removed = true
				default:
					t.Errorf("unexpected tool %s", req.Params.Name)
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1,
					"result": map[string]any{"content": []map[string]string{{"type": "text", "text": reply}}}})
			}))
			s := &suite{stateDir: state, logPath: logPath, provider: claudia.ProviderGrok,
				host: strings.TrimPrefix(peer.URL, "http://")}
			for invocation := range 2 {
				err := s.jThreadSpawnDirectRemove()
				if (err == nil) != tc.wantOK {
					t.Errorf("invocation %d: J9 error=%v, want success=%v", invocation, err, tc.wantOK)
				}
			}
			peer.Close()
			if !directed || !removed {
				t.Errorf("directed=%v removed=%v", directed, removed)
			}
		})
	}
}

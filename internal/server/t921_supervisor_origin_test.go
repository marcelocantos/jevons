// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/delivery"
)

// 🎯T921: the owner's overnight Codex supervisor posts through the owner's
// HTTP send with no origin. Its passes declare themselves ("Automated
// supervisor pass, not the owner", "no authority claimed"); they are
// delivered as the supervisor's, never as the owner's. The owner's own
// messages are untouched.
func TestT921SupervisorPassesAreNotTheOwners(t *testing.T) {
	s := New("test", t.TempDir())
	var got []string
	s.SetAgentSendOriginHook(func(name, text, origin string, mode delivery.Mode) (AgentSendOutcome, error) {
		got = append(got, origin)
		return AgentSendOutcome{Status: "sent"}, nil
	})
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	send := func(body string) {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/agents/jevons/send", strings.NewReader(body)))
		if w.Code != http.StatusOK {
			t.Fatalf("send %s: %d %s", body, w.Code, w.Body.String())
		}
	}
	for _, body := range []string{
		// The three live shapes from 2026-09-21..29.
		`{"text":"Automated supervisor pass, not the owner. jevons-po has no jevons_* tools (T886) and sits idle."}`,
		`{"text":"Automated supervisor pass 12:20 (not the owner). jevonsd.log 11:36:43 ..."}`,
		`{"text":"Supervisor keep-busy reminder (no authority claimed; your own T155/T325.1 doctrine): resume your workers."}`,
		// Explicit, without the wording.
		`{"text":"The fleet is idle.","origin":"supervisor"}`,
	} {
		got = nil
		send(body)
		if len(got) != 1 || got[0] != sendOriginAgent {
			t.Fatalf("%s delivered as %v, want the supervisor's (agent), not the owner's", body, got)
		}
	}
	for _, body := range []string{
		`{"text":"Please check why jevons-po is stuck."}`,
		`{"text":"The supervisord config needs a look — I am the owner.","origin":"owner"}`,
	} {
		got = nil
		send(body)
		if len(got) != 1 || got[0] != sendOriginOwner {
			t.Fatalf("owner message %s delivered as %v, want the owner's", body, got)
		}
	}
}

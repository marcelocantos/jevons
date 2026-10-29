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

// A program that posts to /api/agents/{name}/send without naming an origin
// is not the owner. jevons-po's curl report (2026-10-23) defaulted to owner
// origin and painted in the owner's chat as the owner's own message. An
// explicit origin still decides; the browser-shaped cockpit path is unchanged.
func TestUnnamedNonBrowserSendIsAgentOrigin(t *testing.T) {
	cases := []struct {
		name, body, want string
	}{
		{"unnamed program", `{"text":"report"}`, sendOriginAgent},
		{"explicit owner", `{"text":"report","origin":"owner"}`, sendOriginOwner},
		{"explicit agent", `{"text":"report","origin":"agent"}`, sendOriginAgent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := New("test", t.TempDir())
			var got string
			s.SetAgentSendOriginHook(func(_, _, origin string, _ delivery.Mode) (AgentSendOutcome, error) {
				got = origin
				return AgentSendOutcome{Status: "sent"}, nil
			})
			req := httptest.NewRequest(http.MethodPost, "/api/agents/jevons-po/send", strings.NewReader(tc.body))
			req.SetPathValue("name", "jevons-po")
			rr := httptest.NewRecorder()
			s.handleAgentSend(rr, req)
			if rr.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
			}
			if got != tc.want {
				t.Fatalf("delivered origin=%q, want %q", got, tc.want)
			}
		})
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
)

// 🎯T832: J30-J33 certify the shared overseer against its CURRENT provider,
// because an earlier journey may have moved it (J13 once migrated it in
// place, and the full grok run failed 'agent jevons launched on "claude",
// requested "grok"').
func TestT832OverseerFollowsItsLatestLaunch(t *testing.T) {
	migrated := `msg="agent started" name=jevons provider=grok session=a
msg="agent started" name=jevons provider=claude session=b`
	for _, tc := range []struct {
		name, logs string
		current    claudia.Provider
		wantOK     bool
	}{
		{"migrated overseer on its current backend", migrated, "claude", true},
		{"registry names the sidecar id of the launched plan", migrated, "anthropic", true},
		{"never migrated", `msg="agent started" name=jevons provider=grok`, "xai-oauth", true},
		// Controls: a launch on the wrong backend still fails.
		{"latest launch is not the registry's provider", migrated, "grok", false},
		{"only launch is on another backend", `msg="agent started" name=jevons provider=codex`, "claude", false},
		{"no launch evidence", ``, "grok", false},
		{"another seat's launch", `msg="agent started" name=jevons-po provider=grok`, "grok", false},
		{"adoption is not a launch", `msg="agent adopted" name=jevons provider=grok`, "grok", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := latestLaunchProvider([]byte(tc.logs), overseerName, tc.current)
			if (err == nil) != tc.wantOK {
				t.Fatalf("latestLaunchProvider: error=%v, wantOK=%v", err, tc.wantOK)
			}
		})
	}
}

// The acceptance's control: with the overseer genuinely on another backend
// than the journey requested, the journey fails on the aside it minted on
// the wrong backend, not on the overseer.
func TestT832WrongBackendFailsOnTheAsideNotTheOverseer(t *testing.T) {
	logs := []byte(`msg="agent started" name=jevons provider=grok
msg="agent started" name=jevons provider=claude
msg="agent started" name=react-aside-x provider=claude`)
	if err := latestLaunchProvider(logs, overseerName, "anthropic"); err != nil {
		t.Fatalf("overseer on its current provider must pass: %v", err)
	}
	err := queueJourneyProvider(logs, "react-aside-x", "grok")
	if err == nil || !strings.Contains(err.Error(), "react-aside-x") {
		t.Fatalf("aside launched on claude for a grok journey must fail naming the aside, got %v", err)
	}
}

func TestT832RegistryProviderReadsAPIAgents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/agents" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`[{"name":"jevons-po","provider":"xai-oauth"},{"name":"jevons","provider":"anthropic"},{"name":"blank","provider":""}]`))
	}))
	defer srv.Close()
	s := &suite{host: strings.TrimPrefix(srv.URL, "http://")}
	got, err := s.registryProvider(overseerName)
	if err != nil || got != "anthropic" {
		t.Fatalf("registryProvider(jevons) = %q, %v; want anthropic", got, err)
	}
	for _, name := range []string{"missing", "blank"} {
		if got, err := s.registryProvider(name); err == nil {
			t.Fatalf("registryProvider(%s) = %q, want an error", name, got)
		}
	}
}

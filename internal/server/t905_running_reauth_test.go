// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
)

// 🎯T905: a seat that is still running but whose turns the provider refuses
// on its login is offered Reauth, and Reauth repairs the plan without
// relaunching it (Claudia reloads the live seats). On 2026-09-29 every such
// seat answered 409 "agent is already running".
func TestT905RunningSeatRefusedOnItsLoginIsOfferedAndRecovered(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg.SetDirect(true)
	const refused, healthy, other = "t905-refused", "t905-healthy", "t905-grok"
	for name, provider := range map[string]claudia.Provider{refused: "anthropic", healthy: "anthropic", other: "grok"} {
		if err := reg.Register(claudia.AgentDef{Name: name, WorkDir: t.TempDir(), Provider: provider, SessionID: name + "-sid"}); err != nil {
			t.Fatal(err)
		}
	}
	var launched []string
	reg.SetLaunchers(&claudia.RegistryLaunchers{Start: func(_ context.Context, cfg claudia.Config) (*claudia.Agent, error) {
		launched = append(launched, cfg.Name)
		return claudia.NewStubAgent(nil), nil
	}})
	for _, name := range []string{refused, healthy, other} {
		if _, err := reg.Launch(name); err != nil {
			t.Fatal(err)
		}
	}
	launched = nil

	s := New("test", t.TempDir())
	s.SetRegistry(reg)
	var cleared []string
	s.SetPlanAuthFailure(
		func(name string) bool { return name == refused },
		func(names []string) { cleared = append(cleared, names...) },
	)
	recovered := 0
	s.authRecover = func(_ context.Context, provider claudia.Provider) error {
		recovered++
		if provider != "anthropic" {
			t.Fatalf("provider = %q", provider)
		}
		return nil
	}
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)

	// The cockpit offers Reauth on the refused running seat only.
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/agents", nil))
	var rows []struct {
		Name            string `json:"name"`
		ReauthAvailable bool   `json:"reauth_available"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &rows); err != nil {
		t.Fatalf("agents: %v: %s", err, w.Body.String())
	}
	for _, r := range rows {
		if want := r.Name == refused; r.ReauthAvailable != want {
			t.Fatalf("%s reauth_available = %v, want %v", r.Name, r.ReauthAvailable, want)
		}
	}

	post := func(name string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/agents/"+name+"/auth/recover", nil))
		return w
	}
	// A healthy running seat is still refused, and nothing is recovered.
	if w := post(healthy); w.Code != http.StatusConflict || recovered != 0 {
		t.Fatalf("healthy running seat: status=%d recovered=%d body=%s", w.Code, recovered, w.Body.String())
	}
	w = post(refused)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"status":"recovered"`) {
		t.Fatalf("refused running seat: status=%d body=%s", w.Code, w.Body.String())
	}
	if recovered != 1 {
		t.Fatalf("recovered = %d", recovered)
	}
	if len(launched) != 0 {
		t.Fatalf("a running seat was relaunched: %v", launched)
	}
	slices.Sort(cleared)
	if !slices.Equal(cleared, []string{healthy, refused}) {
		t.Fatalf("cleared = %v, want both seats on the anthropic plan and not the grok one", cleared)
	}
}

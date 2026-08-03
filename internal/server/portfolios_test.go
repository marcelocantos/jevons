// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/config"
)

// 🎯T200 hermetic: fixture portfolio ≥2 members as one group; empty calm.

func TestBuildPortfolioViewsEmptyCalm(t *testing.T) {
	got := BuildPortfolioViews(nil, nil)
	if got == nil {
		t.Fatal("want non-nil empty slice")
	}
	if len(got) != 0 {
		t.Fatalf("len=%d want 0", len(got))
	}
	got = BuildPortfolioViews([]config.Portfolio{}, []agentInfo{{Name: "x", WorkDir: "/tmp"}})
	if len(got) != 0 {
		t.Fatalf("empty defs: len=%d", len(got))
	}
}

func TestBuildPortfolioViewsGroupsTwoMembers(t *testing.T) {
	defs := []config.Portfolio{{
		ID:   "personal",
		Name: "Personal",
		Members: []string{
			"github.com/marcelocantos/jevons",
			"github.com/marcelocantos/pigeon",
		},
	}}
	agents := []agentInfo{
		{Name: "jevons", WorkDir: "/Users/x/work/github.com/marcelocantos/jevons"},
		{Name: "jevons-po", WorkDir: "/Users/x/work/github.com/marcelocantos/jevons"},
		{Name: "pigeon-worker", WorkDir: "/Users/x/work/github.com/marcelocantos/pigeon"},
		{Name: "other", WorkDir: "/Users/x/work/github.com/other/repo"},
	}
	views := BuildPortfolioViews(defs, agents)
	if len(views) != 1 {
		t.Fatalf("portfolios=%d want 1: %+v", len(views), views)
	}
	p := views[0]
	if p.ID != "personal" || p.Name != "Personal" {
		t.Fatalf("view: %+v", p)
	}
	if len(p.Members) != 2 {
		t.Fatalf("members=%d want 2 (one group with ≥2 members): %+v", len(p.Members), p.Members)
	}
	// jevons member matches two agents by workdir path, not name parse.
	if p.Members[0].Label != "jevons" {
		t.Fatalf("label0=%q", p.Members[0].Label)
	}
	if len(p.Members[0].Agents) != 2 {
		t.Fatalf("jevons agents=%v want 2", p.Members[0].Agents)
	}
	if len(p.Members[1].Agents) != 1 || p.Members[1].Agents[0] != "pigeon-worker" {
		t.Fatalf("pigeon agents=%v", p.Members[1].Agents)
	}
	// Unmatched agent must not appear.
	for _, m := range p.Members {
		for _, n := range m.Agents {
			if n == "other" {
				t.Fatal("name-unrelated agent must not join via name heuristics")
			}
		}
	}
}

func TestWorkdirMatchesMemberDeclarativeOnly(t *testing.T) {
	// Path match.
	if !workdirMatchesMember("/Users/a/work/github.com/org/repo", "github.com/org/repo") {
		t.Fatal("expected path containment match")
	}
	// Agent named like the product but wrong workdir must not match.
	if workdirMatchesMember("/tmp/unrelated", "github.com/org/repo") {
		t.Fatal("must not match on name; workdir only")
	}
	if workdirMatchesMember("", "github.com/org/repo") {
		t.Fatal("empty workdir")
	}
	if workdirMatchesMember("/tmp/x", "") {
		t.Fatal("empty member")
	}
}

func TestHandleListPortfoliosHTTP(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	dirJ := filepath.Join(t.TempDir(), "work", "github.com", "marcelocantos", "jevons")
	dirP := filepath.Join(t.TempDir(), "work", "github.com", "marcelocantos", "pigeon")
	for _, d := range []claudia.AgentDef{
		{Name: "jevons", WorkDir: dirJ, SessionID: "1", Provider: "grok"},
		{Name: "jv-po", WorkDir: dirJ, SessionID: "2", Provider: "grok", Parent: "jevons"},
		{Name: "pigeon-w", WorkDir: dirP, SessionID: "3", Provider: "grok"},
	} {
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}

	s := New("test", t.TempDir())
	s.SetRegistry(reg)
	s.SetPortfolios([]config.Portfolio{{
		ID:   "personal",
		Name: "Personal",
		Members: []string{
			"github.com/marcelocantos/jevons",
			"github.com/marcelocantos/pigeon",
		},
	}})
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/api/portfolios")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var body PortfoliosResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Portfolios) != 1 {
		t.Fatalf("portfolios=%+v", body.Portfolios)
	}
	if len(body.Portfolios[0].Members) < 2 {
		t.Fatalf("want ≥2 members in one group: %+v", body.Portfolios[0].Members)
	}
	// At least one member has matched agents.
	var matched int
	for _, m := range body.Portfolios[0].Members {
		matched += len(m.Agents)
	}
	if matched < 2 {
		t.Fatalf("matched agents=%d want ≥2: %+v", matched, body.Portfolios[0])
	}
}

func TestHandleListPortfoliosMissingCalm(t *testing.T) {
	s := New("test", t.TempDir())
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/api/portfolios")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var body PortfoliosResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Portfolios == nil || len(body.Portfolios) != 0 {
		t.Fatalf("calm empty: %+v", body)
	}
}

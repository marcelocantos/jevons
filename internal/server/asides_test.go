// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
)

// 🎯T136: POST /api/asides registers purpose=aside in the fleet registry
// (register-only when no text, no process launch) so the RHS tree can show 💡 nodes.
// 🎯T263: when text is provided, start/rehydrate and deliver the opening prompt.

func TestEnsureAsideAgentRegistersPurposeAside(t *testing.T) {
	state := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(state, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	// Overseer root so parent defaults correctly.
	if err := reg.Register(claudia.AgentDef{
		Name: "jevons", WorkDir: state, SessionID: "s-o", Provider: "grok", AutoStart: true,
		Purpose: claudia.PurposeOverseer,
	}); err != nil {
		t.Fatal(err)
	}
	s := New("test", state)
	s.SetOverseerName("jevons")
	s.SetRegistry(reg)

	out, err := s.ensureAsideAgent("att-billing", "billing nit", "", "")
	if err != nil {
		t.Fatalf("ensureAsideAgent: %v", err)
	}
	if !out.Created {
		t.Fatal("want created=true on first register")
	}
	if out.Name != "att-billing" || out.Purpose != claudia.PurposeAside {
		t.Fatalf("out=%+v", out)
	}
	if out.Parent != "jevons" {
		t.Fatalf("parent=%q want jevons", out.Parent)
	}
	if out.Description != "billing nit" {
		t.Fatalf("description=%q", out.Description)
	}
	if out.Status != "stopped" {
		t.Fatalf("status=%q want stopped (no launch)", out.Status)
	}

	// List feed surfaces purpose + description for RHS tree.
	agents := listFleetAgents(reg)
	var found *agentInfo
	for i := range agents {
		if agents[i].Name == "att-billing" {
			found = &agents[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("aside missing from list: %+v", agents)
	}
	if found.Purpose != claudia.PurposeAside {
		t.Fatalf("list purpose=%q", found.Purpose)
	}
	if found.Description != "billing nit" {
		t.Fatalf("list description=%q", found.Description)
	}
	if found.Parent != "jevons" {
		t.Fatalf("list parent=%q", found.Parent)
	}

	// Idempotent: same id updates description, not a second agent.
	out2, err := s.ensureAsideAgent("att-billing", "billing nit v2", "", "side")
	if err != nil {
		t.Fatal(err)
	}
	if out2.Created {
		t.Fatal("second call must not report created")
	}
	if out2.Description != "billing nit v2" {
		t.Fatalf("updated description=%q", out2.Description)
	}
	if len(listFleetAgents(reg)) != 2 { // jevons + att-billing
		t.Fatalf("len agents=%d want 2", len(listFleetAgents(reg)))
	}
}

func TestHandleCreateAsideHTTP(t *testing.T) {
	state := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(state, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jevons", WorkDir: state, SessionID: "1", Provider: "grok",
	}); err != nil {
		t.Fatal(err)
	}
	s := New("test", state)
	s.SetOverseerName("jevons")
	s.SetRegistry(reg)
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	body, _ := json.Marshal(map[string]string{
		"id":    "att-smoke",
		"title": "smoke test",
	})
	resp, err := http.Post(srv.URL+"/api/asides", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var out createAsideResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Name != "att-smoke" || out.Purpose != "aside" || out.Description != "smoke test" {
		t.Fatalf("out=%+v", out)
	}

	// Second POST → 200, not 201.
	body2, _ := json.Marshal(map[string]string{"id": "att-smoke", "title": "smoke test"})
	resp2, err := http.Post(srv.URL+"/api/asides", "application/json", bytes.NewReader(body2))
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("second status %d want 200", resp2.StatusCode)
	}
}

// 🎯T263: freeform aside create with opening text delivers in the same turn
// (register + start/rehydrate + send) — not registry-only empty transcript.
func TestHandleCreateAsideWithTextDelivers(t *testing.T) {
	state := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(state, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jevons", WorkDir: state, SessionID: "1", Provider: "grok",
	}); err != nil {
		t.Fatal(err)
	}
	s := New("test", state)
	s.SetOverseerName("jevons")
	s.SetRegistry(reg)

	var gotName, gotText string
	s.SetAgentSendHook(func(name, text string) (string, error) {
		gotName, gotText = name, text
		return "rehydrated_sent", nil
	})

	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	body, _ := json.Marshal(map[string]string{
		"id":    "att-msftck4l-9sguxj",
		"title": "how does bullseye compare to beads?",
		"text":  "how does bullseye compare to beads?",
	})
	resp, err := http.Post(srv.URL+"/api/asides", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var out createAsideResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Name != "att-msftck4l-9sguxj" || out.Purpose != "aside" {
		t.Fatalf("out=%+v", out)
	}
	if out.Status != "running" {
		t.Fatalf("status=%q want running after deliver", out.Status)
	}
	if out.Deliver != "rehydrated_sent" {
		t.Fatalf("deliver=%q", out.Deliver)
	}
	if gotName != "att-msftck4l-9sguxj" {
		t.Fatalf("send name=%q", gotName)
	}
	if gotText != "how does bullseye compare to beads?" {
		t.Fatalf("send text=%q", gotText)
	}
	// Opening deliver seeds progress working (RHS activity chrome).
	prog := s.agentProgress.Get("att-msftck4l-9sguxj")
	if prog.Phase != "working" {
		t.Fatalf("progress phase=%q want working", prog.Phase)
	}
	// Registry must contain the aside (not empty-session only).
	if reg.Def("att-msftck4l-9sguxj") == nil {
		t.Fatal("aside missing from registry after create+deliver")
	}
}

// 🎯T263: start/deliver failure is loud (HTTP error), not silent stopped+empty.
func TestHandleCreateAsideDeliverFailIsLoud(t *testing.T) {
	state := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(state, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jevons", WorkDir: state, SessionID: "1", Provider: "grok",
	}); err != nil {
		t.Fatal(err)
	}
	s := New("test", state)
	s.SetOverseerName("jevons")
	s.SetRegistry(reg)
	s.SetAgentSendHook(func(name, text string) (string, error) {
		return "", fmt.Errorf("agent %q rehydrate failed: transcript not found for session ea056cd8", name)
	})

	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	body, _ := json.Marshal(map[string]string{
		"id":    "att-fail-open",
		"title": "broken open",
		"text":  "please answer this",
	})
	resp, err := http.Post(srv.URL+"/api/asides", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 400 {
		t.Fatalf("want loud fail status ≥400, got %d", resp.StatusCode)
	}
	var errBody map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&errBody); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errBody["error"], "start/deliver failed") {
		t.Fatalf("error=%q want start/deliver failed prefix", errBody["error"])
	}
	// Agent is registered (partial success) but deliver failed loudly.
	if reg.Def("att-fail-open") == nil {
		t.Fatal("aside should remain registered after partial create")
	}
}

func TestEnsureAsideAgentRefusesOverseerName(t *testing.T) {
	state := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(state, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := New("test", state)
	s.SetOverseerName("jevons")
	s.SetRegistry(reg)
	if _, err := s.ensureAsideAgent("jevons", "nope", "", ""); err == nil {
		t.Fatal("want error when id is overseer")
	}
}

// 🎯T152: DELETE /api/asides/{id} deregisters purpose=aside so RHS 💡 leaves
// after __TARGET_FILED__ close; non-aside agents are refused.

func TestDismissAsideAgentRemovesFromList(t *testing.T) {
	state := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(state, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jevons", WorkDir: state, SessionID: "s-o", Provider: "grok",
		Purpose: claudia.PurposeOverseer,
	}); err != nil {
		t.Fatal(err)
	}
	s := New("test", state)
	s.SetOverseerName("jevons")
	s.SetRegistry(reg)

	if _, err := s.ensureAsideAgent("att-mscr94z3-nyd7q6", "file T151 links", "", "target"); err != nil {
		t.Fatal(err)
	}
	if len(listFleetAgents(reg)) != 2 {
		t.Fatalf("pre-dismiss agents=%d want 2", len(listFleetAgents(reg)))
	}
	if err := s.dismissAsideAgent("att-mscr94z3-nyd7q6", ""); err != nil {
		t.Fatalf("dismissAsideAgent: %v", err)
	}
	agents := listFleetAgents(reg)
	for _, a := range agents {
		if a.Name == "att-mscr94z3-nyd7q6" {
			t.Fatalf("aside still in list after dismiss: %+v", agents)
		}
	}
	if len(agents) != 1 || agents[0].Name != "jevons" {
		t.Fatalf("agents after dismiss=%+v want only jevons", agents)
	}
	// Idempotent: second dismiss is ok.
	if err := s.dismissAsideAgent("att-mscr94z3-nyd7q6", ""); err != nil {
		t.Fatalf("second dismiss: %v", err)
	}
}

func TestDismissAsideAgentRefusesNonAside(t *testing.T) {
	state := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(state, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jevons", WorkDir: state, SessionID: "s-o", Provider: "grok",
		Purpose: claudia.PurposeOverseer,
	}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jv-worker", WorkDir: state, SessionID: "s-w", Provider: "grok",
		Purpose: claudia.PurposeWork, Parent: "jevons",
	}); err != nil {
		t.Fatal(err)
	}
	s := New("test", state)
	s.SetOverseerName("jevons")
	s.SetRegistry(reg)

	if err := s.dismissAsideAgent("jv-worker", ""); err == nil {
		t.Fatal("want error dismissing non-aside worker")
	}
	if reg.Def("jv-worker") == nil {
		t.Fatal("worker must remain registered")
	}
	if err := s.dismissAsideAgent("jevons", ""); err == nil {
		t.Fatal("want error dismissing overseer")
	}
}

func TestHandleDeleteAsideHTTP(t *testing.T) {
	state := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(state, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jevons", WorkDir: state, SessionID: "1", Provider: "grok",
	}); err != nil {
		t.Fatal(err)
	}
	s := New("test", state)
	s.SetOverseerName("jevons")
	s.SetRegistry(reg)
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	// Register via POST then DELETE.
	body, _ := json.Marshal(map[string]string{"id": "att-t152", "title": "file me"})
	resp, err := http.Post(srv.URL+"/api/asides", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST status %d", resp.StatusCode)
	}

	req, err := http.NewRequest(http.MethodDelete, srv.URL+"/api/asides/att-t152", nil)
	if err != nil {
		t.Fatal(err)
	}
	delResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer delResp.Body.Close()
	if delResp.StatusCode != http.StatusOK {
		t.Fatalf("DELETE status %d", delResp.StatusCode)
	}
	var out map[string]any
	if err := json.NewDecoder(delResp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out["dismissed"] != true || out["name"] != "att-t152" {
		t.Fatalf("out=%+v", out)
	}
	if reg.Def("att-t152") != nil {
		t.Fatal("registry still has att-t152 after DELETE")
	}

	// Already gone → still 200 (idempotent).
	req2, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/asides/att-t152", nil)
	del2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	del2.Body.Close()
	if del2.StatusCode != http.StatusOK {
		t.Fatalf("idempotent DELETE status %d", del2.StatusCode)
	}
}

// 🎯T270: dismissed asides remain in durable history with type delineation.

func TestClosedAsideHistoryArchivesOnDismiss(t *testing.T) {
	state := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(state, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jevons", WorkDir: state, SessionID: "s-o", Provider: "grok",
		Purpose: claudia.PurposeOverseer,
	}); err != nil {
		t.Fatal(err)
	}
	s := New("test", state)
	s.SetOverseerName("jevons")
	s.SetRegistry(reg)

	if _, err := s.ensureAsideAgent("att-side-1", "billing nit", "", "side"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ensureAsideAgent("att-tgt-1", "file T270", "", "target"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ensureAsideAgent("att-cap-1", "capture note", "", "capture"); err != nil {
		t.Fatal(err)
	}
	if err := s.dismissAsideAgent("att-side-1", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.dismissAsideAgent("att-tgt-1", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.dismissAsideAgent("att-cap-1", ""); err != nil {
		t.Fatal(err)
	}

	// Live fleet has no asides.
	for _, a := range listFleetAgents(reg) {
		if a.Name != "jevons" {
			t.Fatalf("live list still has %q", a.Name)
		}
	}

	hist, err := s.listClosedAsides()
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 3 {
		t.Fatalf("history len=%d want 3: %+v", len(hist), hist)
	}
	byID := map[string]closedAsideRecord{}
	for _, r := range hist {
		byID[r.ID] = r
	}
	if byID["att-side-1"].Kind != asideKindSide || byID["att-side-1"].KindLabel != "Side chat" {
		t.Fatalf("side record=%+v", byID["att-side-1"])
	}
	if byID["att-tgt-1"].Kind != asideKindTarget || byID["att-tgt-1"].KindLabel != "Target filing" {
		t.Fatalf("target record=%+v", byID["att-tgt-1"])
	}
	if byID["att-cap-1"].Kind != asideKindCapture || byID["att-cap-1"].KindLabel != "Capture" {
		t.Fatalf("capture record=%+v", byID["att-cap-1"])
	}
	// Durable file survives new Server on same stateDir (not session memory).
	s2 := New("test2", state)
	hist2, err := s2.listClosedAsides()
	if err != nil {
		t.Fatal(err)
	}
	if len(hist2) != 3 {
		t.Fatalf("reload history len=%d", len(hist2))
	}
}

func TestHandleListClosedAsidesHTTP(t *testing.T) {
	state := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(state, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jevons", WorkDir: state, SessionID: "1", Provider: "grok",
	}); err != nil {
		t.Fatal(err)
	}
	s := New("test", state)
	s.SetOverseerName("jevons")
	s.SetRegistry(reg)
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	// Create with kind=target, dismiss, list history.
	body, _ := json.Marshal(map[string]string{
		"id": "att-hist", "title": "file me", "kind": "target",
	})
	resp, err := http.Post(srv.URL+"/api/asides", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST status %d", resp.StatusCode)
	}
	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/asides/att-hist", nil)
	delResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	delResp.Body.Close()
	if delResp.StatusCode != http.StatusOK {
		t.Fatalf("DELETE status %d", delResp.StatusCode)
	}

	getResp, err := http.Get(srv.URL + "/api/asides/history")
	if err != nil {
		t.Fatal(err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("GET history status %d", getResp.StatusCode)
	}
	var out struct {
		Asides []closedAsideRecord `json:"asides"`
		Count  int                 `json:"count"`
	}
	if err := json.NewDecoder(getResp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Count != 1 || len(out.Asides) != 1 {
		t.Fatalf("out=%+v", out)
	}
	if out.Asides[0].ID != "att-hist" || out.Asides[0].Kind != "target" {
		t.Fatalf("record=%+v", out.Asides[0])
	}
	if out.Asides[0].KindLabel != "Target filing" {
		t.Fatalf("kind_label=%q", out.Asides[0].KindLabel)
	}
}

// 🎯T365: the RHS needs to tell a target filing from an idea/capture aside
// after a hard reload, when the browser remembers nothing about the create
// command. The fleet feed carries the create-time kind for that.
func TestFleetFeedCarriesAsideKind(t *testing.T) {
	state := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(state, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jevons", WorkDir: state, SessionID: "s-o", Provider: "grok", AutoStart: true,
		Purpose: claudia.PurposeOverseer,
	}); err != nil {
		t.Fatal(err)
	}
	s := New("test", state)
	s.SetOverseerName("jevons")
	s.SetRegistry(reg)

	// One aside per kind, plus a work agent that must stay unmarked.
	if _, err := s.ensureAsideAgent("att-filing", "safe mode", "", "target"); err != nil {
		t.Fatalf("create target aside: %v", err)
	}
	if _, err := s.ensureAsideAgent("att-idea", "an idea", "", "capture"); err != nil {
		t.Fatalf("create capture aside: %v", err)
	}
	if _, err := s.ensureAsideAgent("att-chat", "side chat", "", ""); err != nil {
		t.Fatalf("create side aside: %v", err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jv-t365-chrome", WorkDir: state, SessionID: "s-w", Provider: "grok",
		Parent: "jevons", Purpose: claudia.PurposeWork,
	}); err != nil {
		t.Fatal(err)
	}

	byName := map[string]agentInfo{}
	for _, a := range listFleetAgents(reg) {
		byName[a.Name] = a
	}
	want := map[string]string{
		"att-filing":     asideKindTarget,
		"att-idea":       asideKindCapture,
		"att-chat":       asideKindSide,
		"jv-t365-chrome": "", // work rows carry no aside kind
	}
	for name, kind := range want {
		got, ok := byName[name]
		if !ok {
			t.Fatalf("%s missing from fleet feed", name)
		}
		if got.AsideKind != kind {
			t.Fatalf("%s aside_kind=%q want %q", name, got.AsideKind, kind)
		}
	}

	// The kind must also survive the JSON the browser actually reads.
	body, err := json.Marshal(byName["att-filing"])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"aside_kind":"target"`) {
		t.Fatalf("wire body missing aside_kind: %s", body)
	}
}

// An aside registered before 🎯T270 has no meta.json; the feed must leave the
// kind empty (client defaults to 💡) rather than inventing a filing.
func TestFleetFeedAsideKindEmptyWithoutMeta(t *testing.T) {
	state := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(state, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "att-legacy", WorkDir: filepath.Join(state, "asides", "att-legacy"),
		SessionID: "s-l", Provider: "grok", Purpose: claudia.PurposeAside,
		Description: "old aside",
	}); err != nil {
		t.Fatal(err)
	}
	for _, a := range listFleetAgents(reg) {
		if a.Name == "att-legacy" && a.AsideKind != "" {
			t.Fatalf("aside_kind=%q want empty (no meta on disk)", a.AsideKind)
		}
	}
}

func TestNormalizeAsideKind(t *testing.T) {
	cases := map[string]string{
		"":            asideKindSide,
		"aside":       asideKindSide,
		"side":        asideKindSide,
		"capture":     asideKindCapture,
		"target":      asideKindTarget,
		"file-target": asideKindTarget,
		"target-aside": asideKindTarget,
	}
	for in, want := range cases {
		if got := normalizeAsideKind(in); got != want {
			t.Fatalf("normalizeAsideKind(%q)=%q want %q", in, got, want)
		}
	}
}

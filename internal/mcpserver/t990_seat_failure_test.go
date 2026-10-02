// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
)

// 🎯T990: on 2026-10-02 the owner force-started 🎯T989 from the cockpit
// twice. Each time a seat was minted, retired as unbriefed_seat, and the
// frontier row bounced from "acknowledged" back to the green play arrow
// with nothing to show for it — the cause (Claude Code's workspace-trust
// dialog on an untrusted workdir) lived only in the event journal. A seat
// that dies before its opening brief lands must leave its target waiting
// with the real reason, served where the play button reads it.
func TestT990UnbriefedSeatRetirementSurfacesTheCause(t *testing.T) {
	dir := t.TempDir()
	workdir := filepath.Join(dir, "jevons-mobile")
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg.SetDirect(true)
	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	if err := reg.Register(claudia.AgentDef{
		Name: "jv-t989-mobile-webview", WorkDir: workdir, SessionID: "s1",
		Purpose: claudia.PurposeWork, Parent: "yourworld2-po", TargetID: "T989",
		Provider: claudia.ProviderClaude,
	}); err != nil {
		t.Fatal(err)
	}

	// A Claude Code config that has never been asked about this workdir.
	trust := filepath.Join(dir, "claude.json")
	if err := os.WriteFile(trust, []byte(`{"projects":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	prev := seatTrustConfigPath
	seatTrustConfigPath = func() string { return trust }
	defer func() { seatTrustConfigPath = prev }()

	cause := errors.New("turn not submitted: composer state=paste_chip after 8 Enter presses")
	if !s.releaseUnbriefedSeat("jv-t989-mobile-webview", false, cause) {
		t.Fatal("a seat this daemon minted was not retired")
	}
	if reg.Def("jv-t989-mobile-webview") != nil {
		t.Fatal("retired seat still registered")
	}

	wait, ok := s.SeatWaits()["T989"]
	if !ok {
		t.Fatalf("seat waits = %+v, want T989 waiting after its seat was retired", s.SeatWaits())
	}
	if wait.Kind != SeatWaitKindSeatFailed || wait.Seat != "jv-t989-mobile-webview" || wait.Parent != "yourworld2-po" {
		t.Fatalf("wait = %+v, want kind=%s naming the seat and its parent", wait, SeatWaitKindSeatFailed)
	}
	for _, want := range []string{
		"unbriefed_seat",
		"yourworld2-po",
		"paste_chip",
		"has not trusted workdir " + workdir,
	} {
		if !strings.Contains(wait.Reason, want) {
			t.Errorf("wait reason missing %q:\n%s", want, wait.Reason)
		}
	}

	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/seat-waits", nil))
	var served map[string]SeatWait
	if err := json.Unmarshal(rec.Body.Bytes(), &served); err != nil {
		t.Fatalf("GET /api/seat-waits = %s, %v", rec.Body.String(), err)
	}
	if served["T989"].Kind != SeatWaitKindSeatFailed || !strings.Contains(served["T989"].Reason, "unbriefed_seat") {
		t.Fatalf("GET /api/seat-waits = %s, want T989 seat_failed with its reason", rec.Body.String())
	}

	// A seat that lands clears it (the start path calls this on launch).
	s.ClearSeatWait("T989")
	if _, ok := s.SeatWaits()["T989"]; ok {
		t.Fatal("a re-seated target is still waiting")
	}
}

// A trusted workdir does not get the trust diagnosis: the daemon says only
// what it knows.
func TestT990TrustedWorkdirCarriesNoTrustDiagnosis(t *testing.T) {
	dir := t.TempDir()
	trust := filepath.Join(dir, "claude.json")
	doc := `{"projects":{"` + dir + `":{"hasTrustDialogAccepted":true}}}`
	if err := os.WriteFile(trust, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	prev := seatTrustConfigPath
	seatTrustConfigPath = func() string { return trust }
	defer func() { seatTrustConfigPath = prev }()

	def := &claudia.AgentDef{Name: "w", WorkDir: dir, Parent: "jevons-po", Provider: claudia.ProviderClaude}
	got := describeSeatFailure(def, "w", "startup_stall", "released a seat whose CLI stalled", nil)
	if strings.Contains(got, "has not trusted") {
		t.Fatalf("trust diagnosis on a trusted workdir:\n%s", got)
	}
	if !strings.Contains(got, "seat w (parent jevons-po) retired: startup_stall") {
		t.Fatalf("description = %q", got)
	}
}
